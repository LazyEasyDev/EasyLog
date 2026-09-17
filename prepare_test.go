package easylog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestPrepareRecordFields(test *testing.T) {
	var valueCalls int
	var replaced []string
	runtime := New(Options{ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
		replaced = append(replaced, attr.Key)
		if len(groups) == 0 && attr.Key == slog.MessageKey {
			return slog.String("message", attr.Value.String())
		}
		return attr
	}}, nil)
	handler := runtime.root.WithAttrs([]slog.Attr{
		slog.String("service", "api"),
		slog.String("level", "ignored"),
	}).WithGroup("request").WithAttrs([]slog.Attr{
		slog.Any("id", handlerTestLogValuer(func() slog.Value {
			valueCalls++
			return slog.IntValue(valueCalls)
		})),
	}).(*handler)
	source := slog.NewRecord(time.Time{}, slog.LevelWarn, "event", 0)
	source.AddAttrs(slog.String("msg", "nested"), slog.Group("", slog.Bool("ok", true)))
	prepared := handler.prepare(source)
	want := []slog.Attr{
		slog.String("level", "WARN"),
		slog.String("message", "event"),
		slog.String("service", "api"),
		slog.Group("request", slog.Int("id", 1), slog.String("msg", "nested"), slog.Bool("ok", true)),
	}
	if got := preparedRecordAttrs(prepared); !reflect.DeepEqual(got, want) {
		test.Fatalf("prepared attrs = %v, want %v", got, want)
	}
	if prepared.Level != source.Level || prepared.Message != source.Message || prepared.Time != source.Time || prepared.PC != source.PC {
		test.Fatal("preparation changed routing metadata")
	}
	if valueCalls != 1 {
		test.Fatalf("LogValue calls = %d, want one", valueCalls)
	}
	if want := []string{"service", "", "id", "level", "msg", "msg", "ok"}; !reflect.DeepEqual(replaced, want) {
		test.Fatalf("replacement order = %v, want %v", replaced, want)
	}
}

func TestPrepareOmitsReplacedMetadataAndEmptyGroups(test *testing.T) {
	runtime := New(Options{ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
		if isReservedKey(attr.Key) || attr.Key == "hidden" {
			return slog.Attr{}
		}
		return attr
	}}, nil)
	source := slog.NewRecord(time.Now(), slog.LevelInfo, "event", 0)
	source.AddAttrs(slog.Group("empty", slog.String("hidden", "value")), slog.String("status", "ok"))
	prepared := runtime.root.prepare(source)
	want := []slog.Attr{slog.String("status", "ok")}
	if got := preparedRecordAttrs(prepared); !reflect.DeepEqual(got, want) {
		test.Fatalf("prepared attrs = %v, want %v", got, want)
	}
}

func preparedRecordAttrs(record slog.Record) []slog.Attr {
	var attrs []slog.Attr
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	return attrs
}

func TestPreparedJSONMatchesStandardHandler(test *testing.T) {
	for _, testCase := range []struct {
		name    string
		replace func([]string, slog.Attr) slog.Attr
	}{
		{name: "unchanged"},
		{name: "renamed_metadata", replace: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.MessageKey {
				return slog.String("message", attr.Value.String())
			}
			return attr
		}},
		{name: "removed_metadata", replace: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && isReservedKey(attr.Key) {
				return slog.Attr{}
			}
			return attr
		}},
		{name: "replacement_group", replace: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == "replace" {
				return slog.Group("", slog.String("added", "value"), slog.Group("child", slog.Int("id", 9)))
			}
			return attr
		}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			timestamp := time.Date(2026, time.September, 17, 10, 0, 0, 123, time.FixedZone("offset", 2*60*60))
			source := slog.NewRecord(timestamp, slog.LevelWarn, "event\nnext", 0)
			source.AddAttrs(
				slog.Group("duplicate", slog.Int("first", 1)),
				slog.Group("duplicate", slog.Int("second", 2)),
				slog.Group("", slog.String("inline", "<value>")),
				slog.String("replace", "original"),
				slog.Duration("duration", 1500*time.Microsecond),
				slog.Time("at", timestamp),
				slog.Uint64("maximum", math.MaxUint64),
				slog.Float64("ratio", 0.75),
				slog.Float64("invalid", math.NaN()),
				slog.Any("error", errors.New("failure")),
				slog.Any("bytes", []byte("hello")),
				slog.Any("object", map[string]any{"count": 1, "text": "<value>"}),
				slog.Any("raw", json.RawMessage(` { "id": 9007199254740993 } `)),
				slog.Any("location", &slog.Source{Function: "function", File: "file.go", Line: 7}),
				slog.Any("nil", nil),
			)
			bound := []slog.Attr{slog.String("service", "api")}
			var expected bytes.Buffer
			reference := slog.NewJSONHandler(&expected, &slog.HandlerOptions{AddSource: true, ReplaceAttr: testCase.replace}).WithAttrs(bound).WithGroup("data")
			if err := reference.Handle(context.Background(), source); err != nil {
				test.Fatal(err)
			}
			runtime := New(Options{AddSource: true, ReplaceAttr: testCase.replace}, nil)
			handler := runtime.root.WithAttrs(bound).WithGroup("data").(*handler)
			got, err := runtime.state.encode(context.Background(), handler.prepare(source))
			if err != nil {
				test.Fatal(err)
			}
			want := bytes.TrimSuffix(expected.Bytes(), []byte{'\n'})
			if !bytes.Equal(got, want) {
				test.Fatalf("prepared JSON = %s, want %s", got, want)
			}
		})
	}
}

func TestPrepareSnapshotsCustomValuesOnce(test *testing.T) {
	var calls int
	object := map[string]int{"value": 7}
	source := slog.NewRecord(time.Time{}, slog.LevelInfo, "event", 0)
	source.AddAttrs(
		slog.Any("custom", prepareMarshalerFunc(func() ([]byte, error) {
			calls++
			return []byte(`"rendered"`), nil
		})),
		slog.Any("object", object),
	)
	runtime := New(Options{}, nil)
	prepared := runtime.root.prepare(source)
	object["value"] = 99
	for range 3 {
		data, err := runtime.state.encode(context.Background(), prepared)
		if err != nil {
			test.Fatal(err)
		}
		want := `{"level":"INFO","msg":"event","custom":"rendered","object":{"value":7}}`
		if string(data) != want {
			test.Fatalf("prepared JSON = %s, want %s", data, want)
		}
	}
	if calls != 1 {
		test.Fatalf("MarshalJSON calls = %d, want one", calls)
	}
}

type prepareMarshalerFunc func() ([]byte, error)

func (marshal prepareMarshalerFunc) MarshalJSON() ([]byte, error) {
	return marshal()
}

func TestPreparedMetadataEncoding(test *testing.T) {
	for _, timestamp := range []time.Time{time.Time{}, time.Date(2026, time.September, 17, 10, 11, 12, 0, time.UTC)} {
		for _, replacement := range []string{"unchanged", "timezone", "removed_message", "renamed_level", "changed_message"} {
			test.Run(timestamp.String()+"/"+replacement, func(test *testing.T) {
				replace := func(_ []string, attr slog.Attr) slog.Attr {
					switch replacement {
					case "timezone":
						if attr.Key == slog.TimeKey {
							return slog.Time(attr.Key, attr.Value.Time().In(time.FixedZone("offset", 2*60*60)))
						}
					case "removed_message":
						if attr.Key == slog.MessageKey {
							return slog.Attr{}
						}
					case "renamed_level":
						if attr.Key == slog.LevelKey {
							return slog.String("severity", "NOTICE")
						}
					case "changed_message":
						if attr.Key == slog.MessageKey {
							return slog.String(attr.Key, "changed")
						}
					}
					return attr
				}
				source := slog.NewRecord(timestamp, slog.LevelInfo, "event", 0)
				var expected bytes.Buffer
				if err := slog.NewJSONHandler(&expected, &slog.HandlerOptions{ReplaceAttr: replace}).Handle(context.Background(), source); err != nil {
					test.Fatal(err)
				}
				runtime := New(Options{ReplaceAttr: replace}, nil)
				data, err := runtime.state.encode(context.Background(), runtime.root.prepare(source))
				want := bytes.TrimSuffix(expected.Bytes(), []byte{'\n'})
				if err != nil || !bytes.Equal(data, want) {
					test.Fatalf("JSON=%q err=%v, want %q", data, err, want)
				}
			})
		}
	}
}
