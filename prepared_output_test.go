package easylog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	easylog "github.com/LazyEasyDev/EasyLog"
	fileoutput "github.com/LazyEasyDev/EasyLog/file"
	"github.com/LazyEasyDev/EasyLog/terminal"
)

func TestOutputsSharePreparedFieldsAndJSON(test *testing.T) {
	var events []string
	first := &recordingOutput{name: "first", events: &events}
	second := &recordingOutput{name: "second", events: &events}
	var jsonOutput, firstText, secondText bytes.Buffer
	directory := test.TempDir()
	files, err := fileoutput.New(fileoutput.Options{Directory: directory})
	if err != nil {
		test.Fatal(err)
	}
	var valueCalls, replaceCalls, marshalCalls int
	formatter := terminal.TextFormatter{DisableColors: true, DisableTimestamp: true, ShowLevel: true}
	runtime := easylog.New(easylog.Options{
		MemoryMaxBytes: 1 << 20,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 {
				switch attr.Key {
				case slog.TimeKey:
					return slog.Attr{}
				case slog.LevelKey:
					return slog.String("level", "NOTICE")
				case slog.MessageKey:
					return slog.String("message", "presented")
				}
			}
			if attr.Key == "dynamic" {
				replaceCalls++
			}
			return attr
		},
	}, []easylog.Output{
		first, terminal.NewJSON(&jsonOutput), terminal.New(&firstText, &formatter),
		files, terminal.New(&secondText, &formatter), second,
	})
	test.Cleanup(func() { _ = runtime.Close() })
	handler := runtime.Logger().With("service", "api").WithGroup("request").
		With("dynamic", preparedTestLogValuer{calls: &valueCalls}).Handler()
	if valueCalls != 0 || replaceCalls != 0 || marshalCalls != 0 {
		test.Fatal("binding evaluated a callback")
	}
	timestamp := time.Date(2026, time.September, 17, 10, 11, 12, 0, time.UTC)
	source := slog.NewRecord(timestamp, slog.LevelWarn, "original", 0)
	source.AddAttrs(slog.Any("payload", preparedTestMarshaler{calls: &marshalCalls}), slog.String("msg", "nested"))
	for range 2 {
		if err := handler.Handle(context.Background(), source); err != nil {
			test.Fatal(err)
		}
	}
	if valueCalls != 2 || replaceCalls != 2 || marshalCalls != 2 {
		test.Fatalf("callback counts: value=%d replace=%d marshal=%d, want two each", valueCalls, replaceCalls, marshalCalls)
	}
	if source.NumAttrs() != 2 {
		test.Fatal("preparation changed the caller's attributes")
	}
	if err := runtime.Close(); err != nil {
		test.Fatal(err)
	}
	retained := runtime.Consumer().Len()
	if retained != 2 || len(first.records) != 2 || len(second.records) != 2 {
		test.Fatalf("retained=%d first=%d second=%d, want two each", retained, len(first.records), len(second.records))
	}
	var expectedJSON, expectedText bytes.Buffer
	for index := range 2 {
		value := index + 1
		content := fmt.Sprintf("{\"level\":\"NOTICE\",\"message\":\"presented\",\"service\":\"api\",\"request\":{\"dynamic\":%d,\"payload\":{\"value\":%d},\"msg\":\"nested\"}}", value, value)
		expectedJSON.WriteString(content + "\n")
		fmt.Fprintf(&expectedText, "NOTICE message=presented service=api request={\"dynamic\":%d,\"payload\":{\"value\":%d},\"msg\":\"nested\"}\n", value, value)
		if string(first.jsonContents[index]) != content || string(second.jsonContents[index]) != content {
			test.Fatal("retained output bytes changed, included a newline, or differed across outputs")
		}
		if &first.jsonContents[index][0] != &second.jsonContents[index][0] {
			test.Fatal("outputs did not receive the same JSON storage")
		}
		for _, record := range []slog.Record{first.records[index], second.records[index]} {
			if record.Time != timestamp || record.Level != slog.LevelWarn || record.Message != "original" {
				test.Fatal("prepared record lost original routing metadata")
			}
			var attrs []slog.Attr
			record.Attrs(func(attr slog.Attr) bool {
				attrs = append(attrs, attr)
				return true
			})
			want := []slog.Attr{
				slog.String("level", "NOTICE"), slog.String("message", "presented"), slog.String("service", "api"),
				slog.Group("request", slog.Int("dynamic", value), slog.Any("payload", json.RawMessage(fmt.Sprintf(`{"value":%d}`, value))), slog.String("msg", "nested")),
			}
			if !reflect.DeepEqual(attrs, want) {
				test.Fatalf("prepared attrs = %v, want %v", attrs, want)
			}
		}
	}
	if size := runtime.Consumer().Bytes(); size != int64(expectedJSON.Len()-retained) {
		test.Fatalf("memory bytes=%d, want %d", size, expectedJSON.Len()-retained)
	}
	if !bytes.Equal(jsonOutput.Bytes(), expectedJSON.Bytes()) || !bytes.Equal(firstText.Bytes(), expectedText.Bytes()) || !bytes.Equal(secondText.Bytes(), expectedText.Bytes()) {
		test.Fatalf("output mismatch: JSON=%q first text=%q second text=%q", jsonOutput.String(), firstText.String(), secondText.String())
	}
	entries, err := os.ReadDir(filepath.Join(directory, "logs"))
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "warn_") {
		test.Fatalf("file routing entries=%v err=%v, want a WARN segment", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "logs", entries[0].Name()))
	if err != nil || !bytes.Equal(data, expectedJSON.Bytes()) {
		test.Fatalf("file JSON=%q err=%v, want %q", data, err, expectedJSON.String())
	}
}

type preparedTestLogValuer struct {
	calls *int
}

func (value preparedTestLogValuer) LogValue() slog.Value {
	(*value.calls)++
	return slog.IntValue(*value.calls)
}

type preparedTestMarshaler struct {
	calls *int
}

func (value preparedTestMarshaler) MarshalJSON() ([]byte, error) {
	(*value.calls)++
	return []byte(fmt.Sprintf(`{"value":%d}`, *value.calls)), nil
}
