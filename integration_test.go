package easylog_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	easylog "github.com/LazyEasyDev/EasyLog"
	fileoutput "github.com/LazyEasyDev/EasyLog/file"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

func TestTerminalFileAndMemoryRemainIndependent(t *testing.T) {
	directory := t.TempDir()
	fileOutput, err := fileoutput.New(fileoutput.Options{
		Directory: directory, MaxSegmentBytes: 1 << 20, MaxSegments: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	var terminalData bytes.Buffer
	runtime := easylog.New(easylog.Options{
		MemoryMaxBytes: 1 << 20,
	}, []easylog.Output{
		terminal.NewJSON(&terminalData),
		fileOutput,
	})

	for _, message := range []string{"first", "second", "third"} {
		runtime.Logger().Info(message)
	}
	records, err := runtime.Consumer().Take(0)
	if err != nil || len(records) != 3 {
		t.Fatalf("consume: records=%d err=%v", len(records), err)
	}
	if err := runtime.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}

	logsDirectory := filepath.Join(directory, "logs")
	entries, err := os.ReadDir(logsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d file entries, want 1", len(entries))
	}
	fileData, err := os.ReadFile(filepath.Join(logsDirectory, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(terminalData.Bytes(), fileData) {
		t.Fatalf("terminal and file order differ:\nterminal=%s\nfile=%s", terminalData.Bytes(), fileData)
	}
	if bytes.Count(fileData, []byte{'\n'}) != 3 {
		t.Fatalf("consuming memory changed output data: %q", fileData)
	}
}

func TestConcurrentTerminalAndFileOrderMatches(t *testing.T) {
	directory := t.TempDir()
	fileOutput, err := fileoutput.New(fileoutput.Options{
		Directory: directory, MaxSegmentBytes: 1 << 20, MaxSegments: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	var terminalData bytes.Buffer
	runtime := easylog.New(easylog.Options{}, []easylog.Output{
		terminal.NewJSON(&terminalData),
		fileOutput,
	})
	logger := runtime.Logger()

	const recordCount = 100
	var wait sync.WaitGroup
	for id := range recordCount {
		wait.Add(1)
		go func() {
			defer wait.Done()
			logger.Info("concurrent", "id", id)
		}()
	}
	wait.Wait()
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}

	logsDirectory := filepath.Join(directory, "logs")
	entries, err := os.ReadDir(logsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d file entries, want 1", len(entries))
	}
	fileData, err := os.ReadFile(filepath.Join(logsDirectory, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(terminalData.Bytes(), fileData) {
		t.Fatal("terminal and file observed different concurrent record order")
	}
	if bytes.Count(fileData, []byte{'\n'}) != recordCount {
		t.Fatalf("got %d records, want %d", bytes.Count(fileData, []byte{'\n'}), recordCount)
	}
}
