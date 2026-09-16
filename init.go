package easylog

import (
	"errors"
	"io"
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

// TerminalOptions configures the terminal output created by Init.
// A nil Writer disables output. A nil Formatter uses elapsed timestamps, a
// visible level, and automatic colors. The formatter is copied, and the writer
// remains caller-owned.
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
	runtime        *Runtime
	logger         *slog.Logger
	previousLogger *slog.Logger
}

var packageState struct {
	sync.RWMutex
	instance *packageRuntime
	closing  bool
}

// Init creates the package-level runtime and installs its logger as slog's default.
// Its zero-value options enable no outputs or memory retention.
// Terminal formatting changes only display; file and memory records remain JSON.
// Init returns ErrAlreadyInitialized until the previous Close has finished.
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

// InitWithOutputs creates a package-level runtime using the supplied outputs
// and installs its logger as slog's default.
// On success, Sync and Close manage these outputs; the slice is copied.
// Nil entries are skipped; typed-nil outputs are not supported.
// It returns ErrAlreadyInitialized while initialized or closing, without
// invoking or closing the supplied outputs.
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
		runtime:        runtime,
		logger:         logger,
		previousLogger: slog.Default(),
	}
	slog.SetDefault(logger)
}

// Consumer returns the package-level memory consumer, or nil when retention is
// disabled or EasyLog is not initialized.
func Consumer() MemoryConsumer {
	packageState.RLock()
	defer packageState.RUnlock()
	if packageState.instance == nil {
		return nil
	}
	return packageState.instance.runtime.Consumer()
}

// Sync synchronizes the current package runtime's outputs.
// It is a no-op when no runtime is installed. If shutdown starts after the
// runtime is captured, it may return ErrClosed.
func Sync() error {
	packageState.RLock()
	instance := packageState.instance
	packageState.RUnlock()
	if instance == nil {
		return nil
	}
	return instance.runtime.Sync()
}

// Close restores the previous slog default and closes the package runtime.
// Outputs release their owned resources; terminal output leaves its writer open.
// It is safe to call more than once.
// Calls made while shutdown is in progress return nil immediately; only the call
// that starts shutdown waits for output and returns any output-close errors.
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
	}

	closeErr := instance.runtime.Close()

	packageState.Lock()
	packageState.closing = false
	packageState.Unlock()
	return closeErr
}
