package easylog

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

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
			records, err := runtime.Consumer().Take(0)
			if err != nil || len(records) != 1 {
				test.Fatalf("records=%d err=%v, want one record", len(records), err)
			}
			want := `{"level":"INFO","msg":"event"` + testCase.want + `}`
			if got := string(records[0].JSON()); got != want {
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
			records, err := runtime.Consumer().Take(0)
			if err != nil || len(records) != 1 {
				test.Fatalf("records=%d err=%v, want one record", len(records), err)
			}
			want := `{"level":"INFO","msg":"event","service":"api"}`
			if got := string(records[0].JSON()); got != want {
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
				records, err := runtime.Consumer().Take(0)
				if err != nil || len(records) != 1 {
					test.Fatalf("records=%d err=%v, want one record", len(records), err)
				}
				want := `{"level":"INFO","message":"event"` + testCase.want + `}`
				if got := string(records[0].JSON()); got != want {
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
	records, err := runtime.Consumer().Take(0)
	if err != nil || len(records) != 1 {
		test.Fatalf("records=%d err=%v, want one record", len(records), err)
	}
	want := `{"level":"INFO","msg":"event","value":"resolved","value":"resolved"}`
	if got := string(records[0].JSON()); got != want {
		test.Fatalf("record = %s, want %s", got, want)
	}
	if ignoredCalls != 0 || inlineCalls != 2 || valueCalls != 2 {
		test.Fatalf("LogValue calls: ignored=%d inline=%d value=%d, want 0, 2, 2", ignoredCalls, inlineCalls, valueCalls)
	}
	if attrs[0].Value.Kind() != slog.KindLogValuer || attrs[1].Value.Kind() != slog.KindLogValuer {
		test.Fatal("filtering changed the caller's attributes")
	}
}

type handlerTestLogValuer func() slog.Value

func (resolve handlerTestLogValuer) LogValue() slog.Value {
	return resolve()
}
