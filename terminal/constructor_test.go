package terminal

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/LazyEasyDev/EasyLog/internal/core"
)

func TestNewSelectsTextFormatter(test *testing.T) {
	record := core.NewRecord(slog.LevelInfo, []byte(`{"time":"2026-09-16T10:11:12Z","level":"INFO","msg":"event"}`))
	for _, testCase := range []struct {
		name      string
		formatter *TextFormatter
		pattern   string
	}{
		{
			name:    "nil_uses_default",
			pattern: `^INFO\[[0-9]{4,}\] event\n$`,
		},
		{
			name:      "zero_value_hides_level",
			formatter: &TextFormatter{},
			pattern:   `^\[[0-9]{4,}\] event\n$`,
		},
		{
			name:      "explicit_hidden_metadata",
			formatter: &TextFormatter{DisableTimestamp: true},
			pattern:   `^event\n$`,
		},
		{
			name:      "explicit_timestamp_layout",
			formatter: &TextFormatter{TimestampFormat: "15:04:05", ShowLevel: true},
			pattern:   `^INFO\[10:11:12\] event\n$`,
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			var destination bytes.Buffer
			before := time.Now()
			output := New(&destination, testCase.formatter)
			after := time.Now()
			if output.formatter == nil {
				test.Fatal("New did not configure text formatting")
			}
			if output.startedAt.Before(before) || output.startedAt.After(after) {
				test.Fatal("elapsed timer did not start during construction")
			}
			if err := output.WriteRecord(record); err != nil {
				test.Fatal(err)
			}
			if !regexp.MustCompile(testCase.pattern).Match(destination.Bytes()) {
				test.Fatalf("output = %q, want pattern %q", destination.String(), testCase.pattern)
			}
		})
	}
}

func TestNewFormatterCopiesAreIndependent(test *testing.T) {
	for _, testCase := range []struct {
		name      string
		formatter *TextFormatter
	}{
		{name: "default"},
		{name: "supplied", formatter: &TextFormatter{DisableTimestamp: true, ShowLevel: true}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			var original TextFormatter
			if testCase.formatter != nil {
				original = *testCase.formatter
			}
			first := New(io.Discard, testCase.formatter)
			second := New(io.Discard, testCase.formatter)
			if first.formatter == second.formatter || first.formatter == testCase.formatter || second.formatter == testCase.formatter {
				test.Fatal("outputs share a formatter instead of independent copies")
			}
			if testCase.formatter != nil && *testCase.formatter != original {
				test.Fatal("automatic color detection modified the supplied formatter")
			}
			want := *second.formatter
			if testCase.formatter != nil {
				*testCase.formatter = TextFormatter{ForceColors: true}
			}
			if *first.formatter != want || *second.formatter != want {
				test.Fatal("changing the supplied formatter reconfigured an output")
			}
			first.formatter.ShowLevel = false
			first.formatter.DisableColors = false
			if *second.formatter != want {
				test.Fatal("changing one output changed another output's formatter")
			}
			if testCase.formatter == nil {
				third := New(io.Discard, nil)
				if *third.formatter != want {
					test.Fatal("an output changed the default for later outputs")
				}
			}
		})
	}
}

func TestConstructorsDefaultToStderr(test *testing.T) {
	for _, testCase := range []struct {
		name   string
		output *Output
		text   bool
	}{
		{name: "default_text", output: New(nil, nil), text: true},
		{name: "configured_text", output: New(nil, &TextFormatter{}), text: true},
		{name: "json", output: NewJSON(nil)},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			if testCase.output.writer != os.Stderr {
				test.Fatal("nil writer did not select os.Stderr")
			}
			if (testCase.output.formatter != nil) != testCase.text {
				test.Fatal("constructor selected the wrong output format")
			}
			if testCase.output.startedAt.IsZero() == testCase.text {
				test.Fatal("elapsed timer should be initialized only for text output")
			}
		})
	}
}
