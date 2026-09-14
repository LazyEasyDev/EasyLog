package terminal_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	easylog "github.com/LazyEasyDev/EasyLog"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

func TestOutputWritesPlainNDJSON(t *testing.T) {
	var destination bytes.Buffer
	runtime := easylog.New(easylog.Options{}, easylog.Outputs{
		Terminal: terminal.New(&destination),
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
	runtime := easylog.New(easylog.Options{}, easylog.Outputs{
		Terminal: terminal.New(destination),
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
