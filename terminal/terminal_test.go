package terminal_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	easylog "github.com/LazyEasyDev/EasyLog"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

func TestOutputWritesPlainNDJSON(t *testing.T) {
	var destination bytes.Buffer
	runtime := easylog.New(easylog.Options{}, []easylog.Output{
		terminal.NewJSON(&destination),
	})
	runtime.Logger().Info("hello", "answer", 42)

	data := destination.Bytes()
	if bytes.Contains(data, []byte("\x1b")) {
		t.Fatalf("output contains ANSI escape sequence: %q", data)
	}
	if bytes.Count(data, []byte{'\n'}) != 1 || !bytes.HasSuffix(data, []byte{'\n'}) {
		t.Fatalf("output is not one NDJSON record: %q", data)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(destination.String(), "\n")), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["msg"] != "hello" || decoded["answer"] != float64(42) {
		t.Fatalf("unexpected record: %v", decoded)
	}
}

func TestOutputHandlesShortWrites(t *testing.T) {
	destination := &shortWriter{limit: 3}
	runtime := easylog.New(easylog.Options{}, []easylog.Output{
		terminal.NewJSON(destination),
	})
	runtime.Logger().Info("short writes")

	data := bytes.TrimSuffix(destination.Bytes(), []byte{'\n'})
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["msg"] != "short writes" {
		t.Fatalf("unexpected record: %v", decoded)
	}
}

type shortWriter struct {
	bytes.Buffer
	limit int
}

func (w *shortWriter) Write(data []byte) (int, error) {
	if len(data) > w.limit {
		data = data[:w.limit]
	}
	return w.Buffer.Write(data)
}

func TestTextOutputHandlesShortWrites(test *testing.T) {
	destination := &shortWriter{limit: 3}
	runtime := easylog.New(easylog.Options{}, []easylog.Output{
		terminal.New(destination, &terminal.TextFormatter{DisableColors: true, DisableTimestamp: true}),
	})
	runtime.Logger().Info("short writes", "answer", 42)
	if want := "short writes answer=42\n"; destination.String() != want {
		test.Fatalf("output = %q, want %q", destination.String(), want)
	}
}

func TestTextOutputPreservesMultipleNestedObjects(test *testing.T) {
	var destination bytes.Buffer
	runtime := easylog.New(easylog.Options{}, []easylog.Output{
		terminal.New(&destination, &terminal.TextFormatter{
			DisableColors:    true,
			DisableTimestamp: true,
			ShowLevel:        false,
		}),
	})
	object := map[string]any{
		"user": map[string]any{"id": 7, "name": "Ann"},
	}
	object2 := map[string]any{"ok": true}
	runtime.Logger().Info("xxx", "data", object, "fff", object2)

	want := `xxx data={"user":{"id":7,"name":"Ann"}} fff={"ok":true}` + "\n"
	if destination.String() != want {
		test.Fatalf("output = %q, want %q", destination.String(), want)
	}
}

func TestTextOutputConcurrentRecords(test *testing.T) {
	var destination bytes.Buffer
	runtime := easylog.New(easylog.Options{}, []easylog.Output{
		terminal.New(&destination, &terminal.TextFormatter{DisableColors: true, DisableTimestamp: true}),
	})
	logger := runtime.Logger()
	const count = 100
	var producers sync.WaitGroup
	for index := range count {
		producers.Add(1)
		go func() {
			defer producers.Done()
			logger.Info("event", "id", index)
		}()
	}
	producers.Wait()
	lines := strings.Split(strings.TrimSuffix(destination.String(), "\n"), "\n")
	if len(lines) != count {
		test.Fatalf("line count = %d, want %d", len(lines), count)
	}
	seen := make(map[string]bool, count)
	for _, line := range lines {
		seen[line] = true
	}
	for index := range count {
		if !seen[fmt.Sprintf("event id=%d", index)] {
			test.Fatalf("missing or corrupted record %d", index)
		}
	}
}
