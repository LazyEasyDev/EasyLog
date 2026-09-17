package terminal_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

func TestJSONOutputPreservesWriteBehavior(test *testing.T) {
	writeFailure := errors.New("write failed")
	content := `{"msg":"event"}`
	line := content + "\n"
	for _, testCase := range []struct {
		name      string
		data      []byte
		limit     int
		writeErr  error
		want      string
		wantErr   error
		wantCalls int
	}{
		{name: "full", data: []byte(content), limit: len(line), want: line, wantCalls: 1},
		{name: "spare_capacity", data: []byte(content + "!")[:len(content)], limit: len(line), want: line, wantCalls: 1},
		{name: "short", data: []byte(content), limit: 3, want: line, wantCalls: (len(line) + 2) / 3},
		{name: "empty", data: []byte{}, limit: 1, want: "\n", wantCalls: 1},
		{name: "nil", limit: 1, want: "\n", wantCalls: 1},
		{name: "zero_progress", data: []byte(content), wantErr: io.ErrShortWrite, wantCalls: 1},
		{name: "partial_error", data: []byte(content), limit: 4, writeErr: writeFailure, want: line[:4], wantErr: writeFailure, wantCalls: 1},
		{name: "newline_error", data: []byte(content), limit: len(content), writeErr: writeFailure, want: content, wantErr: writeFailure, wantCalls: 1},
		{name: "zero_error", data: []byte(content), writeErr: writeFailure, wantErr: writeFailure, wantCalls: 1},
		{name: "full_error", data: []byte(content), limit: len(line), writeErr: writeFailure, want: line, wantErr: writeFailure, wantCalls: 1},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			original := bytes.Clone(testCase.data[:cap(testCase.data)])
			var destination bytes.Buffer
			var calls int
			writer := jsonWriterFunc(func(data []byte) (int, error) {
				calls++
				written, err := destination.Write(data[:min(len(data), testCase.limit)])
				if err != nil {
					return written, err
				}
				return written, testCase.writeErr
			})
			err := terminal.NewJSON(writer).WriteRecord(slog.Record{}, testCase.data)
			if !errors.Is(err, testCase.wantErr) || destination.String() != testCase.want || calls != testCase.wantCalls {
				test.Fatalf("output=%q calls=%d err=%v, want %q, %d calls, and %v", destination.String(), calls, err, testCase.want, testCase.wantCalls, testCase.wantErr)
			}
			if !bytes.Equal(testCase.data[:cap(testCase.data)], original) {
				test.Fatal("writing changed the input bytes")
			}
		})
	}
}

type jsonWriterFunc func([]byte) (int, error)

func (write jsonWriterFunc) Write(data []byte) (int, error) {
	return write(data)
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
