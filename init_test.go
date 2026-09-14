package easylog

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInitUsesConfiguredOutputs(t *testing.T) {
	directory := t.TempDir()
	var terminalData bytes.Buffer
	if err := Init(
		Options{Level: slog.LevelDebug},
		&FileOptions{Directory: directory},
		&terminalData,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	instance := packageState.instance
	if instance.runtime.state.level.Level() != slog.LevelDebug {
		t.Fatalf("level = %v, want DEBUG", instance.runtime.state.level.Level())
	}
	if instance.runtime.state.outputs.Terminal == nil || instance.runtime.state.outputs.File == nil {
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
	if err := Init(Options{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	instance := packageState.instance
	if instance.file != nil {
		t.Fatal("empty Init arguments created a file output")
	}
	if instance.runtime.state.outputs.Terminal != nil || instance.runtime.state.outputs.File != nil {
		t.Fatal("empty Init arguments configured runtime outputs")
	}
	if _, err := os.Stat(filepath.Join(directory, "logs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty Init arguments created a log directory: %v", err)
	}
}

func TestInitRejectsRelativeFileDirectory(t *testing.T) {
	previousLogger := slog.Default()
	err := Init(Options{}, &FileOptions{Directory: "."}, nil)
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
	if err := Init(
		Options{MemoryMaxBytes: 1024},
		&FileOptions{Directory: directory},
		&terminalData,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })

	runtime := packageState.instance.runtime
	if slog.Default().Handler() != runtime.Handler() {
		t.Fatal("Init did not install the EasyLog handler as slog's default")
	}
	slog.Info("initialized", "answer", 42)
	if !bytes.Contains(terminalData.Bytes(), []byte(`"msg":"initialized"`)) {
		t.Fatalf("terminal output = %q", terminalData.Bytes())
	}
	records, err := Consumer().Take(0)
	if err != nil || len(records) != 1 {
		t.Fatalf("memory records = %d, error = %v", len(records), err)
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
	if err := Init(Options{}, &FileOptions{Directory: directory}, terminal); err != nil {
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

func TestInitRejectsSecondInitialization(t *testing.T) {
	if err := Init(
		Options{},
		&FileOptions{Directory: t.TempDir()},
		&bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	installed := packageState.instance

	secondDirectory := filepath.Join(t.TempDir(), "second")
	err := Init(
		Options{},
		&FileOptions{Directory: secondDirectory},
		&bytes.Buffer{},
	)
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

func TestInitFailureDoesNotInstallRuntime(t *testing.T) {
	previousLogger := slog.Default()
	err := Init(
		Options{},
		&FileOptions{Directory: t.TempDir(), MaxSegmentBytes: -1},
		nil,
	)
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
