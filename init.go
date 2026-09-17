package easylog

import (
	"errors"
	"io"
	"log"
	"log/slog"
	"sync"

	fileoutput "github.com/LazyEasyDev/EasyLog/file"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

// ErrAlreadyInitialized indicates initialization was attempted while initialized or closing.
var ErrAlreadyInitialized = errors.New("easylog: already initialized")

// FileOptions configures the file output created by Init.
type FileOptions = fileoutput.Options

// TerminalFormatter configures terminal-only text display.
type TerminalFormatter = terminal.TextFormatter

// TerminalOptions configures terminal output; a nil Writer disables it, and writers remain caller-owned.
// A nil Formatter uses elapsed time, visible levels, and auto colors; non-nil formatters are copied.
type TerminalOptions struct {
	Writer    io.Writer
	Formatter *TerminalFormatter
}

// InitOptions configures the package runtime and its built-in outputs.
// A nil File or Terminal disables that output.
type InitOptions struct {
	Runtime  Options
	File     *FileOptions
	Terminal *TerminalOptions
}

type packageRuntime struct {
	runtime           *Runtime
	logger            *slog.Logger
	previousLogger    *slog.Logger
	previousLogWriter io.Writer
	previousLogFlags  int
}

var packageState struct {
	sync.RWMutex
	instance *packageRuntime
	closing  bool
}

// Init installs the package runtime as slog's default; zero options disable outputs and memory.
// It returns ErrAlreadyInitialized while initialized or closing.
func Init(options InitOptions) error {
	packageState.Lock()
	defer packageState.Unlock()
	if packageState.instance != nil || packageState.closing {
		return ErrAlreadyInitialized
	}

	var files *fileoutput.Output
	if options.File != nil {
		var err error
		files, err = fileoutput.New(*options.File)
		if err != nil {
			return err
		}
	}
	var outputs []Output
	if terminalOptions := options.Terminal; terminalOptions != nil && terminalOptions.Writer != nil {
		outputs = append(outputs, terminal.New(terminalOptions.Writer, terminalOptions.Formatter))
	}
	if files != nil {
		outputs = append(outputs, files)
	}
	installRuntimeLocked(options.Runtime, outputs)
	return nil
}

// InitWithOutputs installs a package runtime using New's output handling and ownership rules.
// It leaves outputs untouched on ErrAlreadyInitialized; on success, its logger becomes slog's default.
func InitWithOutputs(options Options, outputs []Output) error {
	packageState.Lock()
	defer packageState.Unlock()
	if packageState.instance != nil || packageState.closing {
		return ErrAlreadyInitialized
	}

	installRuntimeLocked(options, outputs)
	return nil
}

func installRuntimeLocked(options Options, outputs []Output) {
	runtime := New(options, outputs)
	logger := runtime.Logger()
	packageState.instance = &packageRuntime{
		runtime:           runtime,
		logger:            logger,
		previousLogger:    slog.Default(),
		previousLogWriter: log.Writer(),
		previousLogFlags:  log.Flags(),
	}
	slog.SetDefault(logger)
}

// Consumer returns the package memory consumer, or nil if retention is disabled or uninitialized.
func Consumer() MemoryConsumer {
	packageState.RLock()
	defer packageState.RUnlock()
	if packageState.instance == nil {
		return nil
	}
	return packageState.instance.runtime.Consumer()
}

// Sync synchronizes the package outputs, or does nothing when uninitialized.
// It may return ErrClosed if shutdown races with the call.
func Sync() error {
	packageState.RLock()
	instance := packageState.instance
	packageState.RUnlock()
	if instance == nil {
		return nil
	}
	return instance.runtime.Sync()
}

// Close restores prior slog/log globals only if EasyLog is still default, then calls Runtime.Close.
// Coordinate global logging changes with Init/Close; concurrent or repeated Close returns nil immediately.
func Close() error {
	packageState.Lock()
	if packageState.instance == nil {
		packageState.Unlock()
		return nil
	}

	instance := packageState.instance
	packageState.instance = nil
	packageState.closing = true
	packageState.Unlock()

	if slog.Default() == instance.logger {
		slog.SetDefault(instance.previousLogger)
		log.SetOutput(instance.previousLogWriter)
		log.SetFlags(instance.previousLogFlags)
	}

	closeErr := instance.runtime.Close()

	packageState.Lock()
	packageState.closing = false
	packageState.Unlock()
	return closeErr
}
