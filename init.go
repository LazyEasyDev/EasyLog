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

// ErrAlreadyInitialized indicates initialization was attempted while initializing, initialized, or closing.
var ErrAlreadyInitialized = errors.New("easylog: already initialized")

// ErrInitializing indicates Close was attempted while package initialization is in progress.
var ErrInitializing = errors.New("easylog: initialization in progress")

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
	instance     *packageRuntime
	initializing bool
	closing      bool
}

// Init installs the package runtime as slog's default; zero options disable outputs and memory.
// It returns ErrAlreadyInitialized while initializing, initialized, or closing.
func Init(options InitOptions) error {
	if err := beginInitialization(); err != nil {
		return err
	}
	var instance *packageRuntime
	defer func() { finishInitialization(instance) }()

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
	instance = installRuntime(options.Runtime, outputs)
	return nil
}

// InitWithOutputs installs a package runtime using New's output handling and ownership rules.
// It leaves outputs untouched on ErrAlreadyInitialized; on success, its logger becomes slog's default.
func InitWithOutputs(options Options, outputs []Output) error {
	if err := beginInitialization(); err != nil {
		return err
	}
	var instance *packageRuntime
	defer func() { finishInitialization(instance) }()

	instance = installRuntime(options, outputs)
	return nil
}

func beginInitialization() error {
	packageState.Lock()
	defer packageState.Unlock()
	if packageState.instance != nil || packageState.initializing || packageState.closing {
		return ErrAlreadyInitialized
	}
	packageState.initializing = true
	return nil
}

func finishInitialization(instance *packageRuntime) {
	packageState.Lock()
	defer packageState.Unlock()
	packageState.instance = instance
	packageState.initializing = false
}

func installRuntime(options Options, outputs []Output) *packageRuntime {
	runtime := New(options, outputs)
	logger := runtime.Logger()
	instance := &packageRuntime{
		runtime:           runtime,
		logger:            logger,
		previousLogger:    slog.Default(),
		previousLogWriter: log.Writer(),
		previousLogFlags:  log.Flags(),
	}
	slog.SetDefault(logger)
	return instance
}

// Consumer returns the package memory consumer, or nil if retention is disabled or initialization is incomplete.
func Consumer() MemoryConsumer {
	packageState.RLock()
	defer packageState.RUnlock()
	if packageState.instance == nil {
		return nil
	}
	return packageState.instance.runtime.Consumer()
}

// Sync synchronizes the package outputs, or does nothing when initialization is incomplete.
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
// It returns ErrInitializing without waiting or cancelling when initialization is in progress.
// Coordinate global logging changes with Init/Close; concurrent or repeated Close returns nil immediately.
func Close() error {
	packageState.Lock()
	if packageState.initializing {
		packageState.Unlock()
		return ErrInitializing
	}
	if packageState.instance == nil {
		packageState.Unlock()
		return nil
	}

	instance := packageState.instance
	packageState.instance = nil
	packageState.closing = true
	packageState.Unlock()
	defer func() {
		packageState.Lock()
		packageState.closing = false
		packageState.Unlock()
	}()

	if slog.Default() == instance.logger {
		slog.SetDefault(instance.previousLogger)
		log.SetOutput(instance.previousLogWriter)
		log.SetFlags(instance.previousLogFlags)
	}

	return instance.runtime.Close()
}
