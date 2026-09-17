package easylog

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestInitUsesConfiguredOutputs(t *testing.T) {
	directory := t.TempDir()
	var terminalData bytes.Buffer
	if err := Init(InitOptions{
		Runtime:  Options{Level: slog.LevelDebug},
		File:     &FileOptions{Directory: directory},
		Terminal: &TerminalOptions{Writer: &terminalData},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	instance := packageState.instance
	if instance.runtime.state.level.Level() != slog.LevelDebug {
		t.Fatalf("level = %v, want DEBUG", instance.runtime.state.level.Level())
	}
	if len(instance.runtime.state.outputs) != 2 {
		t.Fatal("Init did not configure terminal and file outputs")
	}
	if Consumer() != nil {
		t.Fatal("Init enabled memory retention by default")
	}
	if _, err := os.Stat(filepath.Join(directory, "logs")); err != nil {
		t.Fatalf("default log directory: %v", err)
	}
}

func TestInitAllowsNoOutputs(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := Init(InitOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	instance := packageState.instance
	if len(instance.runtime.state.outputs) != 0 {
		t.Fatal("empty Init arguments configured runtime outputs")
	}
	if Consumer() != nil || instance.runtime.state.level.Level() != slog.LevelInfo {
		t.Fatal("zero-value InitOptions changed the default level or enabled memory retention")
	}
	if _, err := os.Stat(filepath.Join(directory, "logs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty Init arguments created a log directory: %v", err)
	}
}

func TestInitNilTerminalSettingsDisableOutput(test *testing.T) {
	for _, testCase := range []struct {
		name     string
		terminal *TerminalOptions
	}{
		{name: "nil_terminal"},
		{name: "nil_writer", terminal: &TerminalOptions{}},
		{
			name: "formatter_without_writer",
			terminal: &TerminalOptions{
				Formatter: &TerminalFormatter{ForceColors: true, ShowLevel: true},
			},
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			if err := Init(InitOptions{
				Runtime:  Options{MemoryMaxBytes: 1024},
				Terminal: testCase.terminal,
			}); err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() {
				if err := Close(); err != nil {
					test.Error(err)
				}
			})
			if len(packageState.instance.runtime.state.outputs) != 0 {
				test.Fatal("nil terminal writer enabled output")
			}
			slog.Info("retained")
			consumer := Consumer()
			if consumer == nil || consumer.Len() != 1 {
				test.Fatal("disabled terminal output changed memory retention")
			}
		})
	}
}

func TestInitRejectsRelativeFileDirectory(t *testing.T) {
	previousLogger := slog.Default()
	err := Init(InitOptions{File: &FileOptions{Directory: "."}})
	if err == nil {
		t.Fatal("Init accepted a relative file directory")
	}
	if packageState.instance != nil {
		t.Fatal("failed Init installed package state")
	}
	if slog.Default() != previousLogger {
		t.Fatal("failed Init changed slog's default")
	}
}

func TestInitInstallsDefaultAndCloseRestoresIt(t *testing.T) {
	if Consumer() != nil {
		t.Fatal("package consumer exists before initialization")
	}
	previousLogger := slog.Default()
	directory := t.TempDir()
	var terminalData bytes.Buffer
	if err := Init(InitOptions{
		Runtime:  Options{MemoryMaxBytes: 1024},
		File:     &FileOptions{Directory: directory},
		Terminal: &TerminalOptions{Writer: &terminalData},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	runtime := packageState.instance.runtime
	if slog.Default().Handler() != runtime.Handler() {
		t.Fatal("Init did not install the EasyLog handler as slog's default")
	}
	slog.Info("initialized", "answer", 42)
	if !bytes.HasPrefix(terminalData.Bytes(), []byte("INFO[")) || !bytes.HasSuffix(terminalData.Bytes(), []byte("] initialized answer=42\n")) {
		t.Fatalf("terminal output = %q", terminalData.Bytes())
	}
	if count := Consumer().Len(); count != 1 {
		t.Fatalf("memory records = %d, want one record", count)
	}

	if err := Close(); err != nil {
		t.Fatal(err)
	}
	if Consumer() != nil {
		t.Fatal("package consumer remains available after Close")
	}
	if slog.Default() != previousLogger {
		t.Fatal("Close did not restore the previous slog default")
	}
	if err := Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(directory, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("file segments = %d, want 1", len(entries))
	}
}

func TestCloseWaitsForInFlightWrite(t *testing.T) {
	directory := t.TempDir()
	terminal := newBlockingWriter()
	if err := Init(InitOptions{
		File:     &FileOptions{Directory: directory},
		Terminal: &TerminalOptions{Writer: terminal},
	}); err != nil {
		t.Fatal(err)
	}

	logged := make(chan struct{})
	go func() {
		slog.Info("in flight")
		close(logged)
	}()
	<-terminal.entered

	closed := make(chan error, 1)
	go func() { closed <- Close() }()
	select {
	case err := <-closed:
		close(terminal.release)
		<-logged
		t.Fatalf("Close returned before the in-flight write completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(terminal.release)
	<-logged
	if err := <-closed; err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(directory, "logs"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("file segments = %d, error = %v", len(entries), err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "logs", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"msg":"in flight"`)) {
		t.Fatalf("in-flight record missing from file: %q", data)
	}
}

func TestCloseDoesNotHoldPackageLockWhileDrainingOutput(t *testing.T) {
	terminal := newConsumerCallingWriter()
	if err := Init(InitOptions{
		Runtime:  Options{MemoryMaxBytes: 1024},
		File:     &FileOptions{Directory: t.TempDir()},
		Terminal: &TerminalOptions{Writer: terminal},
	}); err != nil {
		t.Fatal(err)
	}

	packageState.RLock()
	closed := make(chan error, 1)
	go func() { closed <- Close() }()

	deadline := time.Now().Add(time.Second)
	for packageState.TryRLock() {
		packageState.RUnlock()
		if time.Now().After(deadline) {
			packageState.RUnlock()
			<-closed
			t.Fatal("Close did not start")
		}
		runtime.Gosched()
	}

	logged := make(chan struct{})
	go func() {
		slog.Info("reentrant consumer")
		close(logged)
	}()
	select {
	case <-terminal.entered:
	case <-time.After(time.Second):
		packageState.RUnlock()
		<-closed
		t.Fatal("terminal write did not start")
	}
	packageState.RUnlock()

	consumerCompleted := <-terminal.consumerCompleted
	<-logged
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	<-terminal.consumerReturned
	if !consumerCompleted {
		t.Fatal("output callback blocked in Consumer while Close waited for output")
	}
}

func TestCloseWaitsForTerminalOnlyWrite(t *testing.T) {
	terminal := newBlockingWriter()
	if err := Init(InitOptions{Terminal: &TerminalOptions{Writer: terminal}}); err != nil {
		t.Fatal(err)
	}

	logged := make(chan struct{})
	go func() {
		slog.Info("terminal only")
		close(logged)
	}()
	<-terminal.entered

	closed := make(chan error, 1)
	go func() { closed <- Close() }()
	select {
	case err := <-closed:
		close(terminal.release)
		<-logged
		t.Fatalf("Close returned before the terminal write completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(terminal.release)
	<-logged
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
}

func TestInitRejectsSecondInitialization(t *testing.T) {
	if err := Init(InitOptions{
		File:     &FileOptions{Directory: t.TempDir()},
		Terminal: &TerminalOptions{Writer: &bytes.Buffer{}},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	installed := packageState.instance

	secondDirectory := filepath.Join(t.TempDir(), "second")
	err := Init(InitOptions{
		File:     &FileOptions{Directory: secondDirectory},
		Terminal: &TerminalOptions{Writer: &bytes.Buffer{}},
	})
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("Init error = %v, want %v", err, ErrAlreadyInitialized)
	}
	if packageState.instance != installed {
		t.Fatal("second Init replaced the package runtime")
	}
	if _, err := os.Stat(secondDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second Init touched its file directory: %v", err)
	}
}

func TestInitRejectsWhileClosing(test *testing.T) {
	for _, testCase := range []struct {
		name     string
		withFile bool
	}{
		{name: "terminal_only"},
		{name: "with_file", withFile: true},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			previousLogger := slog.Default()
			var fileOptions *FileOptions
			if testCase.withFile {
				fileOptions = &FileOptions{Directory: test.TempDir()}
			}
			if err := Init(InitOptions{
				Runtime:  Options{MemoryMaxBytes: 1024},
				File:     fileOptions,
				Terminal: &TerminalOptions{Writer: &bytes.Buffer{}},
			}); err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() {
				_ = Close()
				slog.SetDefault(previousLogger)
			})
			slog.Info("before close")
			finishClose := startBlockedClose(test)

			secondDirectory := filepath.Join(test.TempDir(), "second")
			err := Init(InitOptions{File: &FileOptions{Directory: secondDirectory}})
			if !errors.Is(err, ErrAlreadyInitialized) {
				test.Fatalf("Init during Close error = %v, want %v", err, ErrAlreadyInitialized)
			}
			if _, err := os.Stat(secondDirectory); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf("Init during Close touched its file directory: %v", err)
			}
			if Consumer() != nil {
				test.Fatal("package consumer remains available during Close")
			}

			if err := finishClose(); err != nil {
				test.Fatal(err)
			}
			if slog.Default() != previousLogger {
				test.Fatal("Close did not restore the previous slog default")
			}
			if err := Init(InitOptions{File: &FileOptions{Directory: secondDirectory}}); err != nil {
				test.Fatalf("Init after Close: %v", err)
			}
			if err := Close(); err != nil {
				test.Fatal(err)
			}
			if slog.Default() != previousLogger {
				test.Fatal("reinitialization restored the wrong slog default")
			}
		})
	}
}

func TestConcurrentCloseReturnsImmediately(test *testing.T) {
	previousLogger := slog.Default()
	if err := Init(InitOptions{}); err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		_ = Close()
		slog.SetDefault(previousLogger)
	})
	finishClose := startBlockedClose(test)

	closed := make(chan error, 1)
	go func() { closed <- Close() }()
	select {
	case err := <-closed:
		if err != nil {
			test.Fatalf("concurrent Close: %v", err)
		}
	case <-time.After(time.Second):
		test.Fatal("concurrent Close waited for the first shutdown")
	}

	if err := Init(InitOptions{}); !errors.Is(err, ErrAlreadyInitialized) {
		test.Fatalf("Init after concurrent Close error = %v, want %v", err, ErrAlreadyInitialized)
	}
	if err := finishClose(); err != nil {
		test.Fatal(err)
	}
	if err := Init(InitOptions{}); err != nil {
		test.Fatalf("Init after the first Close finished: %v", err)
	}
}

func TestInitFailureDoesNotInstallRuntime(t *testing.T) {
	previousLogger := slog.Default()
	err := Init(InitOptions{
		File: &FileOptions{Directory: t.TempDir(), MaxSegmentBytes: -1},
	})
	if err == nil {
		t.Fatal("Init accepted an invalid file size")
	}
	if packageState.instance != nil {
		t.Fatal("failed Init installed package state")
	}
	if slog.Default() != previousLogger {
		t.Fatal("failed Init changed slog's default")
	}
}

func TestInitWithOutputsRejectsWithoutTouchingOutputs(test *testing.T) {
	for _, testCase := range []struct {
		name    string
		custom  bool
		closing bool
	}{
		{name: "built_in_active"},
		{name: "custom_active", custom: true},
		{name: "built_in_closing", closing: true},
		{name: "custom_closing", custom: true, closing: true},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			previous := slog.Default()
			var err error
			if testCase.custom {
				err = InitWithOutputs(Options{}, nil)
			} else {
				err = Init(InitOptions{})
			}
			if err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() { _ = Close() })
			installed := packageState.instance
			logger := slog.Default()
			var finishClose func() error
			if testCase.closing {
				finishClose = startBlockedClose(test)
			}

			calls := 0
			output := &lifecycleTestOutput{
				writeRecord: func(slog.Record, []byte) error { calls++; return nil },
				syncOutput:  func() error { calls++; return nil },
				closeOutput: func() error { calls++; return nil },
			}
			if err := InitWithOutputs(Options{MemoryMaxBytes: 1024}, []Output{output}); !errors.Is(err, ErrAlreadyInitialized) {
				test.Fatalf("InitWithOutputs error = %v, want ErrAlreadyInitialized", err)
			}
			if !testCase.closing && (packageState.instance != installed || slog.Default() != logger) {
				test.Fatal("rejected initialization replaced package state or the default logger")
			}
			directory := filepath.Join(test.TempDir(), "rejected")
			if err := Init(InitOptions{File: &FileOptions{Directory: directory}}); !errors.Is(err, ErrAlreadyInitialized) {
				test.Fatalf("Init error = %v, want ErrAlreadyInitialized", err)
			}
			if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
				test.Fatalf("rejected Init touched the file directory: %v", err)
			}
			if finishClose != nil {
				err = finishClose()
			} else {
				err = Close()
			}
			if err != nil {
				test.Fatal(err)
			}
			if calls != 0 || slog.Default() != previous {
				test.Fatalf("rejected outputs received %d calls or previous logger was not restored", calls)
			}
			if err := InitWithOutputs(Options{}, []Output{output}); err != nil {
				test.Fatalf("InitWithOutputs after Close: %v", err)
			}
			if err := Close(); err != nil {
				test.Fatal(err)
			}
			if calls != 1 {
				test.Fatalf("output calls = %d, want only one close after successful initialization", calls)
			}
		})
	}
}

func TestConcurrentInitializersShareGuard(test *testing.T) {
	previous := slog.Default()
	test.Cleanup(func() { _ = Close() })
	const count = 8
	start := make(chan struct{})
	results := make(chan error, count)
	for index := range count {
		go func() {
			<-start
			if index%2 == 0 {
				results <- InitWithOutputs(Options{}, nil)
			} else {
				results <- Init(InitOptions{})
			}
		}()
	}
	close(start)
	succeeded := 0
	for range count {
		err := <-results
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrAlreadyInitialized) {
			test.Errorf("initialization returned unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		test.Fatalf("successful initializations = %d, want one", succeeded)
	}
	if err := Close(); err != nil {
		test.Fatal(err)
	}
	if slog.Default() != previous {
		test.Fatal("concurrent initialization lost the previous default logger")
	}
}

func TestPackageLifecycleUsesOutputInterface(test *testing.T) {
	if err := Sync(); err != nil {
		test.Fatalf("Sync before Init: %v", err)
	}
	previous := slog.Default()
	syncErr := errors.New("output sync failed")
	closeErr := errors.New("output close failed")
	syncCalls := 0
	closeCalls := 0
	lockHeld := false
	output := &lifecycleTestOutput{
		syncOutput: func() error {
			syncCalls++
			if packageState.TryLock() {
				packageState.Unlock()
			} else {
				lockHeld = true
			}
			return syncErr
		},
		closeOutput: func() error {
			closeCalls++
			if packageState.TryLock() {
				packageState.Unlock()
			} else {
				lockHeld = true
			}
			return closeErr
		},
	}
	if err := InitWithOutputs(Options{}, []Output{output}); err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = Close() })
	if err := Sync(); !errors.Is(err, syncErr) {
		test.Fatalf("package Sync = %v, want output error", err)
	}
	if err := Close(); !errors.Is(err, closeErr) {
		test.Fatalf("package Close = %v, want output error", err)
	}
	if lockHeld {
		test.Fatal("package lifecycle held the package lock during output I/O")
	}
	if syncCalls != 1 || closeCalls != 1 {
		test.Fatalf("output syncs=%d closes=%d, want one each", syncCalls, closeCalls)
	}
	if slog.Default() != previous || packageState.instance != nil || packageState.closing {
		test.Fatal("Close failure did not restore package state")
	}
	if err := Sync(); err != nil {
		test.Fatalf("Sync after Close: %v", err)
	}
	if err := Init(InitOptions{}); err != nil {
		test.Fatalf("Init after Close failure: %v", err)
	}
}

func startBlockedClose(test *testing.T) func() error {
	test.Helper()
	instance := packageState.instance
	instance.runtime.state.outputMu.Lock()
	closed := make(chan error, 1)
	go func() { closed <- Close() }()
	finishClose := sync.OnceValue(func() error {
		instance.runtime.state.outputMu.Unlock()
		return <-closed
	})
	test.Cleanup(func() {
		if err := finishClose(); err != nil {
			test.Errorf("Close: %v", err)
		}
	})

	deadline := time.Now().Add(time.Second)
	for {
		if packageState.TryRLock() {
			detached := packageState.instance == nil
			packageState.RUnlock()
			if detached {
				return finishClose
			}
		}
		if time.Now().After(deadline) {
			test.Fatal("Close did not release package state while draining output")
		}
		runtime.Gosched()
	}
}
