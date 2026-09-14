package easylog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	fileoutput "github.com/LazyEasyDev/EasyLog/file"
	terminaloutput "github.com/LazyEasyDev/EasyLog/terminal"
)

func TestNewAllowsNoOutputsOrMemory(t *testing.T) {
	runtime := New(Options{}, Outputs{})
	if runtime.Consumer() != nil {
		t.Fatal("runtime without memory has a consumer")
	}
	runtime.Logger().Info("discarded")
}

func TestNewMemoryMaxBytes(t *testing.T) {
	if DefaultMemoryMaxBytes != 8*1024*1024 {
		t.Fatalf("default memory limit = %d, want 8 MiB", DefaultMemoryMaxBytes)
	}

	runtime := New(Options{MemoryMaxBytes: -1}, Outputs{})
	if runtime.Consumer() != nil {
		t.Fatal("negative memory limit enabled retention")
	}

	runtime = New(Options{MemoryMaxBytes: DefaultMemoryMaxBytes}, Outputs{})
	if runtime.Consumer() == nil {
		t.Fatal("positive memory limit did not enable retention")
	}

	runtime = New(Options{}, Outputs{Terminal: terminaloutput.New(io.Discard)})
	if runtime.Consumer() != nil {
		t.Fatal("zero memory limit enabled retention")
	}
}

func TestRuntimeFanoutAndConsumeShareEncodedRecord(t *testing.T) {
	var output bytes.Buffer
	runtime := New(Options{
		MemoryMaxBytes: 1024,
	}, Outputs{Terminal: terminaloutput.New(&output)})
	runtime.Logger().Info("hello", "answer", 42)
	records, err := runtime.Consumer().Take(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d consumed records, want 1", len(records))
	}

	outputRecord := bytes.TrimSuffix(output.Bytes(), []byte{'\n'})
	if !bytes.Equal(records[0].JSON(), outputRecord) {
		t.Fatal("consumer and output received different encodings")
	}
	if records[0].Sequence() != 1 {
		t.Fatalf("got sequence %d, want 1", records[0].Sequence())
	}
	if records[0].Level() != slog.LevelInfo {
		t.Fatalf("got level %v, want INFO", records[0].Level())
	}

	var decoded map[string]any
	if err := json.Unmarshal(records[0].JSON(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["msg"] != "hello" || decoded["answer"] != float64(42) {
		t.Fatalf("unexpected encoded record: %v", decoded)
	}
}

func TestHandlerPreservesWithAttrsAndGroups(t *testing.T) {
	runtime := New(Options{MemoryMaxBytes: 1024}, Outputs{})
	runtime.Logger().
		With("service", "api").
		WithGroup("request").
		With("id", 7).
		Info("completed", "status", 200)

	records, err := runtime.Consumer().Take(0)
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%d err=%v", len(records), err)
	}
	data := records[0].JSON()
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	request, ok := decoded["request"].(map[string]any)
	if decoded["service"] != "api" || !ok || request["id"] != float64(7) || request["status"] != float64(200) {
		t.Fatalf("unexpected grouped record: %v", decoded)
	}
}

func TestEveryOutputAttemptedAndMemoryRetainedAfterFailure(t *testing.T) {
	writeFailure := errors.New("write failed")
	terminalFailure := &failingWriter{err: writeFailure}
	directory := t.TempDir()
	files, err := fileoutput.New(fileoutput.Options{Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	runtime := New(Options{
		MemoryMaxBytes: 1024,
	}, Outputs{
		Terminal: terminaloutput.New(terminalFailure),
		File:     files,
	})
	record := slog.NewRecord(time.Now(), slog.LevelError, "failed operation", 0)
	if err := runtime.Handler().Handle(context.Background(), record); !errors.Is(err, writeFailure) {
		t.Fatalf("Handle error = %v, want %v", err, writeFailure)
	}
	if terminalFailure.writes != 1 {
		t.Fatalf("terminal writes = %d, want 1", terminalFailure.writes)
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, "logs"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("file output entries=%d err=%v", len(entries), err)
	}
	if runtime.Consumer().Len() != 1 {
		t.Fatalf("retained records = %d, want 1", runtime.Consumer().Len())
	}
}

func TestRecordAvailableWhileOutputBlocked(t *testing.T) {
	output := newBlockingWriter()
	runtime := New(Options{
		MemoryMaxBytes: 1024,
	}, Outputs{Terminal: terminaloutput.New(output)})

	logged := make(chan struct{})
	go func() {
		runtime.Logger().Info("blocked")
		close(logged)
	}()
	<-output.entered

	records, err := runtime.Consumer().Take(0)
	if err != nil || len(records) != 1 {
		t.Fatalf("take while blocked: records=%d err=%v", len(records), err)
	}
	select {
	case <-logged:
		t.Fatal("logging returned before output was released")
	default:
	}

	close(output.release)
	<-logged
}

func TestDynamicLevelAndContextEnricher(t *testing.T) {
	var level slog.LevelVar
	level.Set(slog.LevelWarn)
	type requestIDKey struct{}
	runtime := New(Options{
		Level:          &level,
		MemoryMaxBytes: 1024,
		Enrichers: []Enricher{func(ctx context.Context) []slog.Attr {
			requestID, _ := ctx.Value(requestIDKey{}).(string)
			if requestID == "" {
				return nil
			}
			return []slog.Attr{slog.String("request_id", requestID)}
		}},
	}, Outputs{})
	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-123")
	runtime.Logger().InfoContext(ctx, "disabled")
	runtime.Logger().WarnContext(ctx, "enabled")
	if runtime.Consumer().Len() != 1 {
		t.Fatalf("memory records = %d, want 1", runtime.Consumer().Len())
	}

	records, err := runtime.Consumer().Take(0)
	if err != nil {
		t.Fatal(err)
	}
	data := records[0].JSON()
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["msg"] != "enabled" || decoded["request_id"] != "req-123" {
		t.Fatalf("unexpected enriched record: %v", decoded)
	}
}

func TestEncodingSupportsSourceReplaceAttrAndLogValuer(t *testing.T) {
	runtime := New(Options{
		AddSource:      true,
		MemoryMaxBytes: 1024,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.MessageKey {
				return slog.String(slog.MessageKey, "rewritten")
			}
			return attr
		},
	}, Outputs{})
	runtime.Logger().Info("original", "lazy", staticLogValuer("resolved"))

	records, err := runtime.Consumer().Take(0)
	if err != nil {
		t.Fatal(err)
	}
	data := records[0].JSON()
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["msg"] != "rewritten" || decoded["lazy"] != "resolved" {
		t.Fatalf("unexpected customized record: %v", decoded)
	}
	if _, ok := decoded["source"].(map[string]any); !ok {
		t.Fatalf("source was not encoded: %v", decoded)
	}
}

func TestMemoryOverflowDoesNotAffectOutput(t *testing.T) {
	var output bytes.Buffer
	runtime := New(Options{
		MemoryMaxBytes: 100,
	}, Outputs{Terminal: terminaloutput.New(&output)})
	runtime.Logger().Info("first")
	runtime.Logger().Info("second")

	records, err := runtime.Consumer().Take(0)
	if err != nil || len(records) != 1 || !bytes.Contains(records[0].JSON(), []byte(`"msg":"second"`)) {
		t.Fatalf("unexpected retained records: records=%d err=%v", len(records), err)
	}
	if count := bytes.Count(output.Bytes(), []byte{'\n'}); count != 2 {
		t.Fatalf("output records = %d, want 2", count)
	}

	var rejectOutput bytes.Buffer
	rejectRuntime := New(Options{
		MemoryMaxBytes: 1,
	}, Outputs{Terminal: terminaloutput.New(&rejectOutput)})
	rejectRuntime.Logger().Info("too large")
	if rejectRuntime.Consumer().Len() != 0 || bytes.Count(rejectOutput.Bytes(), []byte{'\n'}) != 1 {
		t.Fatal("rejected memory record did not remain independent from output")
	}
}

func TestHandleJoinsMultipleOutputErrors(t *testing.T) {
	terminalErr := errors.New("terminal failure")
	terminalFailure := &failingWriter{err: terminalErr}
	files, err := fileoutput.New(fileoutput.Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := files.Close(); err != nil {
		t.Fatal(err)
	}
	runtime := New(Options{}, Outputs{
		Terminal: terminaloutput.New(terminalFailure),
		File:     files,
	})
	record := slog.NewRecord(time.Now(), slog.LevelError, "failed", 0)
	err = runtime.Handler().Handle(context.Background(), record)
	if !errors.Is(err, terminalErr) || !errors.Is(err, fileoutput.ErrClosed) {
		t.Fatalf("joined error does not contain both failures: %v", err)
	}
	if terminalFailure.writes != 1 {
		t.Fatalf("terminal writes = %d, want 1", terminalFailure.writes)
	}
}

type staticLogValuer string

func (value staticLogValuer) LogValue() slog.Value {
	return slog.StringValue(string(value))
}

func TestConcurrentHandleAndTake(t *testing.T) {
	runtime := New(Options{
		MemoryMaxBytes: 1 << 20,
	}, Outputs{Terminal: terminaloutput.New(io.Discard)})
	logger := runtime.Logger()
	start := make(chan struct{})
	errorsSeen := make(chan error, 16)
	recordAvailable := make(chan struct{}, 1)
	producersFinished := make(chan struct{})

	const producers = 8
	var ready sync.WaitGroup
	var producersDone sync.WaitGroup
	var consumer sync.WaitGroup
	ready.Add(producers + 1)
	producersDone.Add(producers)
	consumer.Add(1)
	for producer := range producers {
		go func() {
			defer producersDone.Done()
			ready.Done()
			<-start
			for record := range 100 {
				logger.Info("concurrent", "producer", producer, "record", record)
				select {
				case recordAvailable <- struct{}{}:
				default:
				}
			}
		}()
	}
	go func() {
		defer consumer.Done()
		ready.Done()
		<-start
		for {
			records, err := runtime.Consumer().Take(32)
			if err != nil {
				errorsSeen <- err
				return
			}
			if len(records) > 0 {
				continue
			}
			select {
			case <-recordAvailable:
			case <-producersFinished:
				return
			}
		}
	}()

	ready.Wait()
	close(start)
	producersDone.Wait()
	close(producersFinished)
	consumer.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent operation: %v", err)
	}
}

type failingWriter struct {
	err    error
	writes int
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, w.err
}

type blockingWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (o *blockingWriter) Write(data []byte) (int, error) {
	o.once.Do(func() { close(o.entered) })
	<-o.release
	return len(data), nil
}
