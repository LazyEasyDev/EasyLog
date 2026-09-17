package easylog

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHandlerEncodingPreservesRetainedBytes(test *testing.T) {
	runtime := New(Options{}, nil)
	handlers := []struct {
		handler *handler
		fields  string
	}{
		{handler: runtime.root},
		{
			handler: runtime.root.WithAttrs([]slog.Attr{slog.String("service", "api")}).(*handler),
			fields:  `,"service":"api"`,
		},
		{
			handler: runtime.root.WithGroup("request").WithAttrs([]slog.Attr{slog.Int("id", 7)}).(*handler),
			fields:  `,"request":{"id":7}`,
		},
		{handler: runtime.root.WithGroup("level").WithAttrs([]slog.Attr{slog.String("ignored", "value")}).(*handler)},
	}
	var encoded [][]byte
	var snapshots []string
	for round := range 32 {
		for _, testCase := range handlers {
			source := slog.NewRecord(time.Time{}, slog.LevelInfo, "event-"+strconv.Itoa(round), 0)
			data, err := testCase.handler.state.encode(context.Background(), testCase.handler.prepare(source))
			if err != nil {
				test.Fatal(err)
			}
			want := `{"level":"INFO","msg":"` + source.Message + `"` + testCase.fields + "}"
			if string(data) != want {
				test.Fatalf("record = %s, want %s", data, want)
			}
			encoded = append(encoded, data)
			snapshots = append(snapshots, string(data))
		}
	}
	for index, data := range encoded {
		if string(data) != snapshots[index] {
			test.Fatalf("record %d changed after later encodings: %s, want %s", index, data, snapshots[index])
		}
	}
}

func TestHandlerEncodingPreservesBoundCallbacks(test *testing.T) {
	var valueCalls, replaceCalls int
	runtime := New(Options{
		MemoryMaxBytes: 4096,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == "bound" {
				replaceCalls++
				return slog.Int64("bound", attr.Value.Int64()+100)
			}
			return attr
		},
	}, nil)
	handler := runtime.Handler().WithGroup("data").WithAttrs([]slog.Attr{
		slog.Any("bound", handlerTestLogValuer(func() slog.Value {
			valueCalls++
			return slog.IntValue(valueCalls)
		})),
	})
	if valueCalls != 0 || replaceCalls != 0 {
		test.Fatal("binding attributes evaluated callbacks")
	}
	for range 4 {
		source := slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)
		if err := handler.Handle(context.Background(), source); err != nil {
			test.Fatal(err)
		}
	}
	if valueCalls != 4 || replaceCalls != 4 {
		test.Fatalf("callback counts: LogValue=%d ReplaceAttr=%d, want 4 each", valueCalls, replaceCalls)
	}
	records := retainedMemoryJSON(test, runtime.Consumer())
	if len(records) != 4 {
		test.Fatalf("records=%d, want four records", len(records))
	}
	for index, record := range records {
		want := `{"level":"INFO","msg":"event","data":{"bound":` + strconv.Itoa(index+101) + `}}`
		if got := string(record); got != want {
			test.Fatalf("record %d = %s, want %s", index, got, want)
		}
	}
}

func TestHandlerReservedAttributePaths(test *testing.T) {
	enrich := func(context.Context) []slog.Attr {
		return []slog.Attr{
			slog.String("time", "user time"),
			slog.String("level", "AUDIT"),
			slog.String("msg", "user message"),
			slog.String("source", "user source"),
			slog.String("request_id", "req-123"),
		}
	}
	for _, testCase := range []struct {
		name      string
		configure func(slog.Handler) slog.Handler
		attrs     []slog.Attr
		enrichers []Enricher
		want      string
	}{
		{
			name: "with_attrs",
			configure: func(handler slog.Handler) slog.Handler {
				return handler.WithAttrs(enrich(context.Background()))
			},
			attrs: []slog.Attr{slog.String("status", "ok")},
			want:  `,"request_id":"req-123","status":"ok"`,
		},
		{
			name: "inline_groups",
			attrs: []slog.Attr{slog.Group("",
				slog.String("time", "user time"),
				slog.Group("", slog.String("level", "AUDIT"), slog.String("status", "ok")),
				slog.Group("data", slog.String("msg", "nested message"), slog.String("source", "nested source")),
			)},
			want: `,"status":"ok","data":{"msg":"nested message","source":"nested source"}`,
		},
		{
			name: "with_inline_group",
			configure: func(handler slog.Handler) slog.Handler {
				return handler.WithAttrs([]slog.Attr{slog.Group("", slog.String("msg", "hidden"), slog.String("service", "api"))})
			},
			want: `,"service":"api"`,
		},
		{
			name: "reserved_group_attributes",
			attrs: []slog.Attr{
				slog.Group("time", slog.String("detail", "hidden")),
				slog.Group("level", slog.String("detail", "hidden")),
				slog.Group("msg", slog.String("detail", "hidden")),
				slog.Group("source", slog.String("detail", "hidden")),
				slog.String("status", "ok"),
			},
			want: `,"status":"ok"`,
		},
		{
			name: "nested_with_groups",
			configure: func(handler slog.Handler) slog.Handler {
				return handler.WithAttrs([]slog.Attr{slog.String("service", "api"), slog.String("level", "hidden")}).
					WithGroup("data").WithAttrs([]slog.Attr{slog.String("level", "AUDIT")}).WithGroup("msg")
			},
			attrs: []slog.Attr{slog.String("time", "nested time"), slog.String("source", "nested source")},
			want:  `,"service":"api","data":{"level":"AUDIT","msg":{"time":"nested time","source":"nested source"}}`,
		},
		{
			name:      "enricher",
			enrichers: []Enricher{enrich},
			want:      `,"request_id":"req-123"`,
		},
		{
			name: "nested_enricher",
			configure: func(handler slog.Handler) slog.Handler {
				return handler.WithGroup("data")
			},
			enrichers: []Enricher{enrich},
			want:      `,"data":{"time":"user time","level":"AUDIT","msg":"user message","source":"user source","request_id":"req-123"}`,
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			runtime := New(Options{MemoryMaxBytes: 4096, Enrichers: testCase.enrichers}, nil)
			handler := runtime.Handler()
			if testCase.configure != nil {
				handler = testCase.configure(handler)
			}
			source := slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)
			source.AddAttrs(testCase.attrs...)
			if err := handler.Handle(context.Background(), source); err != nil {
				test.Fatal(err)
			}
			records := retainedMemoryJSON(test, runtime.Consumer())
			if len(records) != 1 {
				test.Fatalf("records=%d, want one record", len(records))
			}
			want := `{"level":"INFO","msg":"event"` + testCase.want + `}`
			if got := string(records[0]); got != want {
				test.Fatalf("record = %s, want %s", got, want)
			}
		})
	}
}

func TestHandlerIgnoresReservedRootGroups(test *testing.T) {
	for _, key := range []string{slog.TimeKey, slog.LevelKey, slog.MessageKey, slog.SourceKey} {
		test.Run(key, func(test *testing.T) {
			runtime := New(Options{MemoryMaxBytes: 1024}, nil)
			handler := runtime.Logger().With("service", "api").WithGroup(key).
				With("hidden", "value").WithGroup("child").Handler()
			source := slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)
			source.AddAttrs(slog.String("status", "hidden"))
			if err := handler.Handle(context.Background(), source); err != nil {
				test.Fatal(err)
			}
			records := retainedMemoryJSON(test, runtime.Consumer())
			if len(records) != 1 {
				test.Fatalf("records=%d, want one record", len(records))
			}
			want := `{"level":"INFO","msg":"event","service":"api"}`
			if got := string(records[0]); got != want {
				test.Fatalf("record = %s, want %s", got, want)
			}
		})
	}
}

func TestHandlerFiltersReservedReplacements(test *testing.T) {
	for _, testCase := range []struct {
		name        string
		replacement slog.Attr
		want        string
	}{
		{name: "time", replacement: slog.String("time", "user time")},
		{name: "level", replacement: slog.String("level", "AUDIT")},
		{name: "msg", replacement: slog.String("msg", "user message")},
		{name: "source", replacement: slog.String("source", "user source")},
		{name: "group", replacement: slog.Group("level", slog.String("detail", "hidden"))},
		{
			name:        "inline_group",
			replacement: slog.Group("", slog.String("msg", "hidden"), slog.String("status", "ok")),
			want:        `,"status":"ok"`,
		},
		{
			name: "lazy_inline_group",
			replacement: slog.Any("", handlerTestLogValuer(func() slog.Value {
				return slog.GroupValue(slog.String("msg", "hidden"), slog.String("status", "ok"))
			})),
			want: `,"status":"ok"`,
		},
	} {
		for _, useWith := range []bool{false, true} {
			name := testCase.name
			if useWith {
				name += "_with"
			}
			test.Run(name, func(test *testing.T) {
				runtime := New(Options{
					MemoryMaxBytes: 1024,
					ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
						if attr.Key == "input" {
							return testCase.replacement
						}
						if attr.Key == slog.MessageKey {
							return slog.String("message", attr.Value.String())
						}
						return attr
					},
				}, nil)
				handler := runtime.Handler()
				source := slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)
				attr := slog.String("input", "value")
				if useWith {
					handler = handler.WithAttrs([]slog.Attr{attr})
				} else {
					source.AddAttrs(attr)
				}
				if err := handler.Handle(context.Background(), source); err != nil {
					test.Fatal(err)
				}
				records := retainedMemoryJSON(test, runtime.Consumer())
				if len(records) != 1 {
					test.Fatalf("records=%d, want one record", len(records))
				}
				want := `{"level":"INFO","message":"event"` + testCase.want + `}`
				if got := string(records[0]); got != want {
					test.Fatalf("record = %s, want %s", got, want)
				}
			})
		}
	}
}

func TestHandlerResolvesFilteredLogValuersOnce(test *testing.T) {
	var ignoredCalls, inlineCalls, valueCalls int
	attrs := []slog.Attr{
		slog.Any("level", handlerTestLogValuer(func() slog.Value {
			ignoredCalls++
			return slog.StringValue("AUDIT")
		})),
		slog.Any("", handlerTestLogValuer(func() slog.Value {
			inlineCalls++
			return slog.GroupValue(
				slog.String("msg", "hidden"),
				slog.Any("value", handlerTestLogValuer(func() slog.Value {
					valueCalls++
					return slog.StringValue("resolved")
				})),
			)
		})),
	}
	runtime := New(Options{MemoryMaxBytes: 1024}, nil)
	handler := runtime.Handler().WithAttrs(attrs)
	source := slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)
	source.AddAttrs(attrs...)
	if err := handler.Handle(context.Background(), source); err != nil {
		test.Fatal(err)
	}
	records := retainedMemoryJSON(test, runtime.Consumer())
	if len(records) != 1 {
		test.Fatalf("records=%d, want one record", len(records))
	}
	want := `{"level":"INFO","msg":"event","value":"resolved","value":"resolved"}`
	if got := string(records[0]); got != want {
		test.Fatalf("record = %s, want %s", got, want)
	}
	if ignoredCalls != 0 || inlineCalls != 2 || valueCalls != 2 {
		test.Fatalf("LogValue calls: ignored=%d inline=%d value=%d, want 0, 2, 2", ignoredCalls, inlineCalls, valueCalls)
	}
	if attrs[0].Value.Kind() != slog.KindLogValuer || attrs[1].Value.Kind() != slog.KindLogValuer {
		test.Fatal("filtering changed the caller's attributes")
	}
}

func TestHandlerEncoderReuseKeepsRuntimeOptions(test *testing.T) {
	custom := New(Options{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.MessageKey {
				return slog.String("message", "custom")
			}
			return attr
		},
	}, nil)
	plain := New(Options{}, nil)
	for range 32 {
		for _, testCase := range []struct {
			runtime *Runtime
			want    string
		}{
			{runtime: custom, want: `{"level":"INFO","message":"custom"}`},
			{runtime: plain, want: `{"level":"INFO","msg":"event"}`},
		} {
			data, err := testCase.runtime.state.encode(context.Background(), testCase.runtime.root.prepare(slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)))
			if err != nil {
				test.Fatal(err)
			}
			if string(data) != testCase.want {
				test.Fatalf("record = %s, want %s", data, testCase.want)
			}
		}
	}
}

func TestHandlerEncoderReuseReleasesLargeBuffer(test *testing.T) {
	runtime := New(Options{}, nil)
	var encoder *recordEncoder
	runtime.state.encoders.New = func() any {
		encoder = newRecordEncoder()
		return encoder
	}
	message := strings.Repeat("x", 2*maxPooledEncoderBufferBytes)
	data, err := runtime.state.encode(context.Background(), runtime.root.prepare(slog.NewRecord(time.Time{}, slog.LevelInfo, message, 0)))
	if err != nil {
		test.Fatal(err)
	}
	if encoder.buffer.Cap() != 0 {
		test.Fatalf("oversized scratch buffer retained %d bytes of capacity", encoder.buffer.Cap())
	}
	want := `{"level":"INFO","msg":"` + message + `"}`
	if string(data) != want {
		test.Fatal("releasing the scratch buffer changed the completed record")
	}
	short, err := runtime.state.encode(context.Background(), runtime.root.prepare(slog.NewRecord(time.Time{}, slog.LevelInfo, "short", 0)))
	if err != nil {
		test.Fatal(err)
	}
	if string(short) != `{"level":"INFO","msg":"short"}` || string(data) != want {
		test.Fatal("encoding after an oversized record changed the output")
	}
}

func TestHandlerEncoderReuseRetainedConcurrentRecords(test *testing.T) {
	const workers, recordsPerWorker = 8, 32
	var retained [][]byte
	output := &lifecycleTestOutput{writeRecord: func(_ slog.Record, jsonContent []byte) error {
		retained = append(retained, jsonContent)
		return nil
	}}
	runtime := New(Options{MemoryMaxBytes: 1 << 20}, []Output{output})
	test.Cleanup(func() { _ = runtime.Close() })
	var producers sync.WaitGroup
	failures := make(chan error, workers)
	for worker := range workers {
		handler := runtime.Handler().WithAttrs([]slog.Attr{slog.Int("worker", worker)}).WithGroup("data")
		producers.Go(func() {
			for index := range recordsPerWorker {
				source := slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)
				source.AddAttrs(slog.Int("index", index))
				if err := handler.Handle(context.Background(), source); err != nil {
					failures <- err
					return
				}
			}
		})
	}
	producers.Wait()
	close(failures)
	for err := range failures {
		test.Fatal(err)
	}
	stored := retainedMemoryJSON(test, runtime.Consumer())
	for _, records := range [][][]byte{retained, stored} {
		if len(records) != workers*recordsPerWorker {
			test.Fatalf("retained %d records, want %d", len(records), workers*recordsPerWorker)
		}
		remaining := make(map[string]bool, workers*recordsPerWorker)
		for worker := range workers {
			for index := range recordsPerWorker {
				want := `{"level":"INFO","msg":"event","worker":` + strconv.Itoa(worker) + `,"data":{"index":` + strconv.Itoa(index) + `}}`
				remaining[want] = true
			}
		}
		for _, record := range records {
			data := string(record)
			if !remaining[data] {
				test.Fatalf("unexpected or overwritten record: %s", data)
			}
			delete(remaining, data)
		}
	}
}

type handlerTestLogValuer func() slog.Value

func (resolve handlerTestLogValuer) LogValue() slog.Value {
	return resolve()
}
