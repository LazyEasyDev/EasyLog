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
	runtime := New(Options{}, nil)
	if runtime.Consumer() != nil {
		t.Fatal("runtime without memory has a consumer")
	}
	runtime.Logger().Info("discarded")
}

func TestNewLevelDefaults(test *testing.T) {
	var nilLevel *slog.LevelVar
	var dynamicLevel slog.LevelVar
	dynamicLevel.Set(slog.LevelWarn)
	ctx := context.Background()

	for _, testCase := range []struct {
		name      string
		level     slog.Leveler
		wantLevel slog.Level
	}{
		{name: "nil", level: nil, wantLevel: slog.LevelInfo},
		{name: "nil_level_var", level: nilLevel, wantLevel: slog.LevelInfo},
		{name: "fixed_level", level: slog.LevelDebug, wantLevel: slog.LevelDebug},
		{name: "dynamic_level", level: &dynamicLevel, wantLevel: slog.LevelWarn},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			runtime := New(Options{Level: testCase.level, MemoryMaxBytes: 1024}, nil)
			logger := runtime.Logger()
			if logger.Enabled(ctx, testCase.wantLevel-1) {
				test.Fatal("level below threshold is enabled")
			}
			if !logger.Enabled(ctx, testCase.wantLevel) {
				test.Fatal("threshold level is disabled")
			}

			logger.Log(ctx, testCase.wantLevel-1, "filtered")
			logger.Log(ctx, testCase.wantLevel, "retained")
			records := retainedMemoryJSON(test, runtime.Consumer())
			if len(records) != 1 {
				test.Fatalf("records=%d, want one record", len(records))
			}
			var decoded struct{ Level string }
			if err := json.Unmarshal(records[0], &decoded); err != nil {
				test.Fatal(err)
			}
			if decoded.Level != testCase.wantLevel.String() {
				test.Fatalf("record level = %s, want %v", decoded.Level, testCase.wantLevel)
			}
		})
	}
}

func TestNewMemoryMaxBytes(t *testing.T) {
	if DefaultMemoryMaxBytes != 8*1024*1024 {
		t.Fatalf("default memory limit = %d, want 8 MiB", DefaultMemoryMaxBytes)
	}

	runtime := New(Options{MemoryMaxBytes: -1}, nil)
	if runtime.Consumer() != nil {
		t.Fatal("negative memory limit enabled retention")
	}

	runtime = New(Options{MemoryMaxBytes: DefaultMemoryMaxBytes}, nil)
	if runtime.Consumer() == nil {
		t.Fatal("positive memory limit did not enable retention")
	}

	runtime = New(Options{}, []Output{terminaloutput.NewJSON(io.Discard)})
	if runtime.Consumer() != nil {
		t.Fatal("zero memory limit enabled retention")
	}
}

func TestRuntimeFanoutAndMemoryShareEncodedJSON(t *testing.T) {
	var output bytes.Buffer
	runtime := New(Options{
		MemoryMaxBytes: 1024,
	}, []Output{terminaloutput.NewJSON(&output)})
	runtime.Logger().Info("hello", "answer", 42)
	records := retainedMemoryJSON(t, runtime.Consumer())
	if len(records) != 1 {
		t.Fatalf("got %d retained records, want 1", len(records))
	}

	outputRecord := bytes.TrimSuffix(output.Bytes(), []byte{'\n'})
	if !bytes.Equal(records[0], outputRecord) {
		t.Fatal("memory and output received different encodings")
	}

	var decoded map[string]any
	if err := json.Unmarshal(records[0], &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["level"] != "INFO" || decoded["msg"] != "hello" || decoded["answer"] != float64(42) {
		t.Fatalf("unexpected encoded record: %v", decoded)
	}
}

func TestHandlerPreservesWithAttrsAndGroups(t *testing.T) {
	runtime := New(Options{MemoryMaxBytes: 1024}, nil)
	runtime.Logger().
		With("service", "api").
		WithGroup("request").
		With("id", 7).
		Info("completed", "status", 200)

	records := retainedMemoryJSON(t, runtime.Consumer())
	if len(records) != 1 {
		t.Fatalf("records=%d, want one record", len(records))
	}
	data := records[0]
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	request, ok := decoded["request"].(map[string]any)
	if decoded["service"] != "api" || !ok || request["id"] != float64(7) || request["status"] != float64(200) {
		t.Fatalf("unexpected grouped record: %v", decoded)
	}
}

func TestHandlerIgnoresReservedAttributes(test *testing.T) {
	var output, textOutput bytes.Buffer
	directory := test.TempDir()
	files, err := fileoutput.New(fileoutput.Options{Directory: directory})
	if err != nil {
		test.Fatal(err)
	}
	runtime := New(Options{MemoryMaxBytes: 1024}, []Output{
		terminaloutput.NewJSON(&output),
		terminaloutput.New(&textOutput, &terminaloutput.TextFormatter{
			DisableColors:    true,
			DisableTimestamp: true,
			ShowLevel:        true,
		}),
		files,
	})
	test.Cleanup(func() { _ = runtime.Close() })
	source := slog.NewRecord(time.Date(2026, time.September, 17, 10, 11, 12, 0, time.UTC), slog.LevelInfo, "event", 0)
	source.AddAttrs(
		slog.String(slog.TimeKey, "user time"),
		slog.String(slog.LevelKey, "AUDIT"),
		slog.String(slog.MessageKey, "user message"),
		slog.String(slog.SourceKey, "user source"),
		slog.String("request_id", "req-123"),
		slog.String("request_id", "req-456"),
	)
	if err := runtime.Handler().Handle(context.Background(), source); err != nil {
		test.Fatal(err)
	}
	records := retainedMemoryJSON(test, runtime.Consumer())
	if len(records) != 1 {
		test.Fatalf("records=%d, want one record", len(records))
	}
	want := `{"time":"2026-09-17T10:11:12Z","level":"INFO","msg":"event","request_id":"req-123","request_id":"req-456"}`
	if got := string(records[0]); got != want {
		test.Errorf("memory record = %s, want %s", got, want)
	}
	if got := output.String(); got != want+"\n" {
		test.Errorf("output = %q, want %q", got, want+"\n")
	}
	if got, wantText := textOutput.String(), "INFO event request_id=req-123 request_id=req-456\n"; got != wantText {
		test.Errorf("terminal text = %q, want %q", got, wantText)
	}
	if err := runtime.Close(); err != nil {
		test.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, "logs"))
	if err != nil || len(entries) != 1 {
		test.Fatalf("file output entries=%d err=%v, want one segment", len(entries), err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "logs", entries[0].Name()))
	if err != nil {
		test.Fatal(err)
	}
	if string(data) != want+"\n" {
		test.Errorf("file record = %q, want %q", data, want+"\n")
	}
	if source.NumAttrs() != 6 {
		test.Fatal("filtering changed the caller's record")
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
	}, []Output{
		terminaloutput.NewJSON(terminalFailure),
		files,
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

func TestTakeWhileOutputBlockedPreservesSharedJSON(test *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	var received []byte
	output := &lifecycleTestOutput{writeRecord: func(_ slog.Record, jsonContent []byte) error {
		close(entered)
		<-release
		received = bytes.Clone(jsonContent)
		return nil
	}}
	var destination bytes.Buffer
	runtime := New(Options{
		MemoryMaxBytes: 1024,
	}, []Output{output, terminaloutput.NewJSON(&destination)})
	test.Cleanup(func() { _ = runtime.Close() })

	logged := make(chan struct{})
	var handleErr error
	go func() {
		handleErr = runtime.Handler().Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "blocked", 0))
		close(logged)
	}()
	test.Cleanup(func() {
		unblock()
		<-logged
	})
	<-entered

	const want = `{"level":"INFO","msg":"blocked"}`
	records, err := runtime.Consumer().Take(1)
	if err != nil || len(records) != 1 || string(records[0]) != want {
		test.Fatalf("take while blocked: records=%q err=%v", records, err)
	}
	records[0][0] = '!'
	if runtime.Consumer().Len() != 0 || runtime.Consumer().Bytes() != 0 {
		test.Fatal("Take did not drain the retained record while output was blocked")
	}
	select {
	case <-logged:
		test.Fatal("logging returned before output was released")
	default:
	}

	unblock()
	<-logged
	if handleErr != nil {
		test.Fatal(handleErr)
	}
	if string(received) != want || destination.String() != want+"\n" {
		test.Fatalf("Take mutation changed output: custom=%q terminal=%q", received, destination.String())
	}
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
	}, nil)
	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-123")
	runtime.Logger().InfoContext(ctx, "disabled")
	runtime.Logger().WarnContext(ctx, "enabled")
	if runtime.Consumer().Len() != 1 {
		t.Fatalf("memory records = %d, want 1", runtime.Consumer().Len())
	}

	records := retainedMemoryJSON(t, runtime.Consumer())
	data := records[0]
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["msg"] != "enabled" || decoded["request_id"] != "req-123" {
		t.Fatalf("unexpected enriched record: %v", decoded)
	}

	level.Set(slog.LevelDebug)
	runtime.Logger().DebugContext(ctx, "enabled after level update")
	if runtime.Consumer().Len() != 2 {
		t.Fatalf("memory records after level update = %d, want 2", runtime.Consumer().Len())
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
	}, nil)
	runtime.Logger().Info("original", "lazy", staticLogValuer("resolved"), "source", "user source")

	records := retainedMemoryJSON(t, runtime.Consumer())
	data := records[0]
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
	}, []Output{terminaloutput.NewJSON(&output)})
	runtime.Logger().Info("first")
	runtime.Logger().Info("second")

	records := retainedMemoryJSON(t, runtime.Consumer())
	if len(records) != 1 || !bytes.Contains(records[0], []byte(`"msg":"second"`)) {
		t.Fatalf("unexpected retained records: records=%d", len(records))
	}
	if count := bytes.Count(output.Bytes(), []byte{'\n'}); count != 2 {
		t.Fatalf("output records = %d, want 2", count)
	}

	var rejectOutput bytes.Buffer
	rejectRuntime := New(Options{
		MemoryMaxBytes: 1,
	}, []Output{terminaloutput.NewJSON(&rejectOutput)})
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
	runtime := New(Options{}, []Output{
		terminaloutput.NewJSON(terminalFailure),
		files,
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
	}, []Output{terminaloutput.NewJSON(io.Discard)})
	t.Cleanup(func() { _ = runtime.Close() })
	logger := runtime.Logger()
	start := make(chan struct{})
	errorsSeen := make(chan error, 16)
	recordAvailable := make(chan struct{}, 1)
	producersFinished := make(chan struct{})
	var consumed [][]byte

	const producers, recordsPerProducer = 8, 100
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
			for record := range recordsPerProducer {
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
				consumed = append(consumed, records...)
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
	remaining, err := runtime.Consumer().Take(0)
	if err != nil {
		t.Fatal(err)
	}
	consumed = append(consumed, remaining...)
	if len(consumed) != producers*recordsPerProducer || runtime.Consumer().Len() != 0 || runtime.Consumer().Bytes() != 0 {
		t.Fatalf("consumed=%d retained=%d bytes=%d, want %d consumed and an empty store", len(consumed), runtime.Consumer().Len(), runtime.Consumer().Bytes(), producers*recordsPerProducer)
	}
	seen := make(map[[2]int]bool, len(consumed))
	for _, content := range consumed {
		var record struct {
			Message  string `json:"msg"`
			Producer int
			Record   int
		}
		if err := json.Unmarshal(content, &record); err != nil {
			t.Fatal(err)
		}
		key := [2]int{record.Producer, record.Record}
		if record.Message != "concurrent" || record.Producer < 0 || record.Producer >= producers || record.Record < 0 || record.Record >= recordsPerProducer || seen[key] {
			t.Fatalf("unexpected or duplicate consumed JSON: %s", content)
		}
		seen[key] = true
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

type consumerCallingWriter struct {
	entered           chan struct{}
	consumerCompleted chan bool
	consumerReturned  chan struct{}
	once              sync.Once
}

func newConsumerCallingWriter() *consumerCallingWriter {
	return &consumerCallingWriter{
		entered:           make(chan struct{}),
		consumerCompleted: make(chan bool, 1),
		consumerReturned:  make(chan struct{}),
	}
}

func (w *consumerCallingWriter) Write(data []byte) (int, error) {
	w.once.Do(func() {
		close(w.entered)
		go func() {
			_ = Consumer()
			close(w.consumerReturned)
		}()

		select {
		case <-w.consumerReturned:
			w.consumerCompleted <- true
		case <-time.After(time.Second):
			w.consumerCompleted <- false
		}
	})
	return len(data), nil
}
