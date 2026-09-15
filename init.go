package easylog

import (
	"errors"
	"io"
	"log/slog"
	"sync"

	fileoutput "github.com/LazyEasyDev/EasyLog/file"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

// ErrAlreadyInitialized indicates that an initializer was called before Close.
var ErrAlreadyInitialized = errors.New("easylog: already initialized")

// FileOptions configures the file output created by Init.
type FileOptions = fileoutput.Options

type packageRuntime struct {
	runtime        *Runtime
	file           *fileoutput.Output
	logger         *slog.Logger
	previousLogger *slog.Logger
}

var packageState struct {
	sync.RWMutex
	instance *packageRuntime
}

// Init creates the package-level runtime and installs its logger as slog's default.
// A nil fileOptions or terminalWriter disables that output.
func Init(options Options, fileOptions *FileOptions, terminalWriter io.Writer) error {
	packageState.Lock()
	defer packageState.Unlock()
	if packageState.instance != nil {
		return ErrAlreadyInitialized
	}

	var files *fileoutput.Output
	if fileOptions != nil {
		var err error
		files, err = fileoutput.New(*fileOptions)
		if err != nil {
			return err
		}
	}
	outputs := Outputs{File: files}
	if terminalWriter != nil {
		outputs.Terminal = terminal.New(terminalWriter)
	}
	runtime := New(options, outputs)
	logger := runtime.Logger()
	packageState.instance = &packageRuntime{
		runtime:        runtime,
		file:           files,
		logger:         logger,
		previousLogger: slog.Default(),
	}
	slog.SetDefault(logger)
	return nil
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

// Close restores the previous slog default, waits for in-flight output writes,
// and closes the package-owned file output. It is safe to call more than once.
func Close() error {
	packageState.Lock()
	if packageState.instance == nil {
		packageState.Unlock()
		return nil
	}

	instance := packageState.instance
	packageState.instance = nil
	packageState.Unlock()

	if slog.Default() == instance.logger {
		slog.SetDefault(instance.previousLogger)
	}

	instance.runtime.state.outputMu.Lock()
	defer instance.runtime.state.outputMu.Unlock()
	if instance.file == nil {
		return nil
	}
	return instance.file.Close()
}
