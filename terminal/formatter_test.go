package terminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/LazyEasyDev/EasyLog/internal/core"
)

func TestTextFormatterOptions(test *testing.T) {
	record := core.NewRecord(slog.LevelInfo, []byte(`{"time":"2026-09-16T10:11:12.123456789+02:00","level":"INFO","msg":"hello world","answer":42}`))
	for _, testCase := range []struct {
		name      string
		formatter TextFormatter
		want      string
	}{
		{
			name: "zero_value",
			want: "[0000] hello world \x1b[36manswer\x1b[0m=42\n",
		},
		{
			name:      "colored_timestamp_and_level",
			formatter: TextFormatter{TimestampFormat: "15:04:05.000", ShowLevel: true},
			want:      "\x1b[36mINFO\x1b[0m[10:11:12.123] hello world \x1b[36manswer\x1b[0m=42\n",
		},
		{
			name:      "plain_with_level",
			formatter: TextFormatter{DisableColors: true, ShowLevel: true},
			want:      "INFO[0000] hello world answer=42\n",
		},
		{
			name:      "without_timestamp_or_level",
			formatter: TextFormatter{DisableColors: true, DisableTimestamp: true},
			want:      "hello world answer=42\n",
		},
		{
			name:      "custom_layout",
			formatter: TextFormatter{DisableColors: true, TimestampFormat: "15:04:05.000", ShowLevel: true},
			want:      "INFO[10:11:12.123] hello world answer=42\n",
		},
		{
			name:      "explicit_rfc3339",
			formatter: TextFormatter{DisableColors: true, TimestampFormat: time.RFC3339},
			want:      "[2026-09-16T10:11:12+02:00] hello world answer=42\n",
		},
		{
			name:      "layout_with_spaces",
			formatter: TextFormatter{DisableColors: true, TimestampFormat: "2006-01-02 15:04:05"},
			want:      "[2026-09-16 10:11:12] hello world answer=42\n",
		},
		{
			name:      "layout_with_control_characters",
			formatter: TextFormatter{DisableColors: true, TimestampFormat: "15:04:05\n\x1b"},
			want:      "[10:11:12\\n\\x1b] hello world answer=42\n",
		},
		{
			name:      "disabled_timestamp_overrides_layout",
			formatter: TextFormatter{DisableColors: true, DisableTimestamp: true, TimestampFormat: "15:04:05", ShowLevel: true},
			want:      "INFO hello world answer=42\n",
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			data, err := testCase.formatter.format(record, 0)
			if err != nil {
				test.Fatal(err)
			}
			if string(data) != testCase.want {
				test.Fatalf("output = %q, want %q", data, testCase.want)
			}
		})
	}
}

func TestTextFormatterElapsedTime(test *testing.T) {
	record := core.NewRecord(slog.LevelInfo, []byte(`{"time":"2000-01-01T00:00:00Z","msg":"event"}`))
	formatter := TextFormatter{DisableColors: true}
	for _, testCase := range []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		{name: "start", want: "[0000] event\n"},
		{name: "fractional_second", elapsed: 999 * time.Millisecond, want: "[0000] event\n"},
		{name: "one_second", elapsed: time.Second, want: "[0001] event\n"},
		{name: "twelve_seconds", elapsed: 12 * time.Second, want: "[0012] event\n"},
		{name: "beyond_four_digits", elapsed: 10000 * time.Second, want: "[10000] event\n"},
		{name: "negative_duration", elapsed: -time.Second, want: "[0000] event\n"},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			data, err := formatter.format(record, testCase.elapsed)
			if err != nil {
				test.Fatal(err)
			}
			if string(data) != testCase.want {
				test.Fatalf("output = %q, want %q", data, testCase.want)
			}
		})
	}
}

func TestTextFormatterElapsedIgnoresRecordTime(test *testing.T) {
	formatter := TextFormatter{DisableColors: true}
	for _, data := range []string{
		`{"time":"2000-01-01T00:00:00Z","msg":"event"}`,
		`{"time":"2099-01-01T00:00:00Z","msg":"event"}`,
		`{"time":"custom","msg":"event"}`,
		`{"time":42,"msg":"event"}`,
		`{"msg":"event"}`,
	} {
		test.Run(data, func(test *testing.T) {
			record := core.NewRecord(slog.LevelInfo, []byte(data))
			formatted, err := formatter.format(record, 12*time.Second)
			if err != nil {
				test.Fatal(err)
			}
			if want := "[0012] event\n"; string(formatted) != want {
				test.Fatalf("output = %q, want %q", formatted, want)
			}
			if string(record.JSON()) != data {
				test.Fatal("elapsed formatting changed the record")
			}
		})
	}
}

func TestTextOutputElapsedStartsAtConstruction(test *testing.T) {
	formatter := TextFormatter{DisableColors: true}
	var olderData, newerData bytes.Buffer
	before := time.Now()
	older := New(&olderData, &formatter)
	newer := New(&newerData, &formatter)
	after := time.Now()
	for _, output := range []*Output{older, newer} {
		if output.startedAt.Before(before) || output.startedAt.After(after) {
			test.Fatal("elapsed timer did not start during output construction")
		}
	}
	older.startedAt = older.startedAt.Add(-12 * time.Second)
	record := core.NewRecord(slog.LevelInfo, []byte(`{"time":"2000-01-01T00:00:00Z","msg":"event"}`))
	for _, testCase := range []struct {
		name   string
		output *Output
		data   *bytes.Buffer
	}{
		{name: "older", output: older, data: &olderData},
		{name: "newer", output: newer, data: &newerData},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			minimum := int64(time.Since(testCase.output.startedAt) / time.Second)
			if err := testCase.output.WriteRecord(record); err != nil {
				test.Fatal(err)
			}
			maximum := int64(time.Since(testCase.output.startedAt) / time.Second)
			var seconds int64
			if _, err := fmt.Sscanf(testCase.data.String(), "[%d] event\n", &seconds); err != nil {
				test.Fatalf("invalid elapsed output %q: %v", testCase.data.String(), err)
			}
			if seconds < minimum || seconds > maximum {
				test.Fatalf("elapsed seconds = %d, want between %d and %d", seconds, minimum, maximum)
			}
			if want := fmt.Sprintf("[%04d] event\n", seconds); testCase.data.String() != want {
				test.Fatalf("output = %q, want %q", testCase.data.String(), want)
			}
		})
	}
}

func TestTextFormatterPreservesFieldsAndRecord(test *testing.T) {
	data := []byte(`{"level":"WARN","msg":"line\n\u001b[31m","key with space":"a=b","id":9007199254740993,"service":"api","service":"worker","request":{"time":"nested","level":"nested","ok":true},"items":[1,null,"two"]}`)
	record := core.NewRecord(slog.LevelWarn, data)
	var destination bytes.Buffer
	if err := New(&destination, &TextFormatter{DisableColors: true, DisableTimestamp: true}).WriteRecord(record); err != nil {
		test.Fatal(err)
	}
	want := "line\\n\\x1b[31m \"key with space\"=\"a=b\" id=9007199254740993 service=api service=worker request={\"time\":\"nested\",\"level\":\"nested\",\"ok\":true} items=[1,null,\"two\"]\n"
	if destination.String() != want {
		test.Fatalf("output = %q, want %q", destination.String(), want)
	}
	if !bytes.Equal(record.JSON(), data) {
		test.Fatal("formatting changed the encoded record")
	}
}

func TestTextFormatterMessageStrings(test *testing.T) {
	for _, testCase := range []struct {
		name    string
		message string
		want    string
	}{
		{name: "plain", message: "hello world", want: "hello world"},
		{name: "quotes_and_backslashes", message: `say "hello" at C:\logs`, want: `say "hello" at C:\logs`},
		{name: "whitespace", message: "  hello   world  ", want: "  hello   world  "},
		{name: "equals", message: "answer=42", want: "answer=42"},
		{name: "controls", message: "line\nnext\r\t\x1b[31m", want: `line\nnext\r\t\x1b[31m`},
		{name: "unicode", message: "caf\u00e9 \u4e16\u754c", want: "caf\u00e9 \u4e16\u754c"},
		{name: "line_separator", message: "hello\u2028world", want: `hello\u2028world`},
		{name: "empty"},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			message, err := json.Marshal(testCase.message)
			if err != nil {
				test.Fatal(err)
			}
			encoded := []byte(fmt.Sprintf(`{"level":"INFO","msg":%s,"answer":42}`, message))
			record := core.NewRecord(slog.LevelInfo, encoded)
			formatter := TextFormatter{DisableColors: true, ShowLevel: true}
			data, err := formatter.format(record, 12*time.Second)
			if err != nil {
				test.Fatal(err)
			}
			want := "INFO[0012]"
			if testCase.want != "" {
				want += " " + testCase.want
			}
			want += " answer=42\n"
			if string(data) != want {
				test.Fatalf("output = %q, want %q", data, want)
			}
			if !bytes.Equal(record.JSON(), encoded) {
				test.Fatal("message formatting changed the encoded record")
			}
		})
	}
}

func TestTextFormatterMessageOnlyHasNoColors(test *testing.T) {
	for _, testCase := range []struct {
		data string
		want string
	}{
		{data: `{"msg":"hello world"}`, want: "hello world\n"},
		{data: `{"msg":""}`, want: "\n"},
		{data: `{"level":"INFO"}`, want: "\n"},
		{data: `{}`, want: "\n"},
	} {
		test.Run(testCase.data, func(test *testing.T) {
			formatter := TextFormatter{DisableTimestamp: true}
			data, err := formatter.format(core.NewRecord(slog.LevelInfo, []byte(testCase.data)), 0)
			if err != nil {
				test.Fatal(err)
			}
			if string(data) != testCase.want {
				test.Fatalf("output = %q, want %q", data, testCase.want)
			}
		})
	}
}

func TestTextFormatterLevelColors(test *testing.T) {
	for _, testCase := range []struct {
		level slog.Level
		label string
		color string
	}{
		{level: slog.LevelDebug, label: "DEBU", color: "\x1b[90m"},
		{level: slog.LevelInfo, label: "INFO", color: "\x1b[36m"},
		{level: slog.LevelWarn, label: "WARN", color: "\x1b[33m"},
		{level: slog.LevelError, label: "ERRO", color: "\x1b[31m"},
	} {
		test.Run(testCase.level.String(), func(test *testing.T) {
			var destination bytes.Buffer
			record := core.NewRecord(testCase.level, []byte(fmt.Sprintf(`{"level":%q,"msg":"event","size":10,"ok":true,"data":{"id":7}}`, testCase.level.String())))
			if err := New(&destination, &TextFormatter{ForceColors: true, DisableTimestamp: true, ShowLevel: true}).WriteRecord(record); err != nil {
				test.Fatal(err)
			}
			want := testCase.color + testCase.label + "\x1b[0m event " + testCase.color + "size\x1b[0m=10 " +
				testCase.color + "ok\x1b[0m=true " + testCase.color + "data\x1b[0m={\"id\":7}\n"
			if destination.String() != want {
				test.Fatalf("output = %q, want %q", destination.String(), want)
			}
		})
	}
}

func TestTextFormatterLevelPrefixes(test *testing.T) {
	for _, testCase := range []struct {
		level  slog.Level
		prefix string
	}{
		{level: slog.LevelDebug, prefix: "DEBU"},
		{level: slog.LevelDebug + 1, prefix: "DEBUG+1"},
		{level: slog.LevelError, prefix: "ERRO"},
		{level: slog.LevelError + 1, prefix: "ERROR+1"},
	} {
		test.Run(testCase.level.String(), func(test *testing.T) {
			label := testCase.level.String()
			encoded := fmt.Sprintf(`{"level":%q,"msg":%q,"state":%q,"data":{"level":%q}}`, label, label+" occurred", label, label)
			record := core.NewRecord(testCase.level, []byte(encoded))
			formatter := TextFormatter{DisableColors: true, ShowLevel: true}
			data, err := formatter.format(record, 12*time.Second)
			if err != nil {
				test.Fatal(err)
			}
			want := fmt.Sprintf("%s[0012] %s occurred state=%s data={\"level\":%q}\n", testCase.prefix, label, label, label)
			if string(data) != want {
				test.Fatalf("output = %q, want %q", data, want)
			}
			if string(record.JSON()) != encoded {
				test.Fatal("abbreviating the prefix changed the encoded record")
			}
		})
	}
}

func TestTextOutputColorOptions(test *testing.T) {
	record := core.NewRecord(slog.LevelInfo, []byte(`{"msg":"event","size":10}`))
	for _, testCase := range []struct {
		name      string
		formatter TextFormatter
		noColor   string
		term      string
		colored   bool
	}{
		{name: "automatic_buffer_is_plain"},
		{name: "forced_buffer_is_colored", formatter: TextFormatter{ForceColors: true}, colored: true},
		{name: "disabled", formatter: TextFormatter{DisableColors: true}},
		{name: "disabled_overrides_force", formatter: TextFormatter{ForceColors: true, DisableColors: true}},
		{name: "force_overrides_no_color", formatter: TextFormatter{ForceColors: true}, noColor: "1", colored: true},
		{name: "force_overrides_dumb_terminal", formatter: TextFormatter{ForceColors: true}, term: "dumb", colored: true},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Setenv("NO_COLOR", testCase.noColor)
			test.Setenv("TERM", testCase.term)
			formatter := testCase.formatter
			formatter.DisableTimestamp = true
			var destination bytes.Buffer
			if err := New(&destination, &formatter).WriteRecord(record); err != nil {
				test.Fatal(err)
			}
			want := "event size=10\n"
			if testCase.colored {
				want = "event \x1b[36msize\x1b[0m=10\n"
			}
			if destination.String() != want {
				test.Fatalf("output = %q, want %q", destination.String(), want)
			}
		})
	}
}

func TestTextFormatterColorsPreservePlainText(test *testing.T) {
	data := []byte(`{"time":"2026-09-16T10:11:12Z","level":"WARN","msg":"line\n\u001b[31m","key with space":"a=b","service":"api","service":"worker","data":{"user":{"id":7}},"items":[1,null,"two"]}`)
	record := core.NewRecord(slog.LevelWarn, data)
	for _, testCase := range []struct {
		name      string
		formatter TextFormatter
	}{
		{name: "elapsed", formatter: TextFormatter{ShowLevel: true}},
		{name: "absolute", formatter: TextFormatter{ShowLevel: true, TimestampFormat: "15:04:05"}},
		{name: "hidden_metadata", formatter: TextFormatter{DisableTimestamp: true}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			colored, err := testCase.formatter.format(record, 12*time.Second)
			if err != nil {
				test.Fatal(err)
			}
			plainFormatter := testCase.formatter
			plainFormatter.DisableColors = true
			plain, err := plainFormatter.format(record, 12*time.Second)
			if err != nil {
				test.Fatal(err)
			}
			withoutColors := bytes.ReplaceAll(colored, []byte("\x1b[33m"), nil)
			withoutColors = bytes.ReplaceAll(withoutColors, []byte("\x1b[0m"), nil)
			if !bytes.Equal(withoutColors, plain) {
				test.Fatalf("colors changed text: colored=%q plain=%q", colored, plain)
			}
			if bytes.Contains(plain, []byte{'\x1b'}) || bytes.Count(colored, []byte{'\n'}) != 1 {
				test.Fatalf("unexpected escape or newline: colored=%q plain=%q", colored, plain)
			}
			if !bytes.Equal(record.JSON(), data) {
				test.Fatal("color formatting changed the encoded record")
			}
		})
	}
}

func TestTextFormatterTimestampOnlyHasNoColors(test *testing.T) {
	record := core.NewRecord(slog.LevelInfo, []byte(`{"time":"2026-09-16T10:11:12Z"}`))
	data, err := (TextFormatter{}).format(record, 12*time.Second)
	if err != nil {
		test.Fatal(err)
	}
	if want := "[0012]\n"; string(data) != want {
		test.Fatalf("output = %q, want %q", data, want)
	}
}

func TestTextFormatterHandlesMissingOrCustomMetadata(test *testing.T) {
	for _, testCase := range []struct {
		name string
		data string
		want string
	}{
		{
			name: "missing_timestamp",
			data: `{"level":"INFO","msg":"event"}`,
			want: "INFO event\n",
		},
		{
			name: "custom_timestamp_text",
			data: `{"time":"relative","level":"INFO","msg":"event"}`,
			want: "INFO time=relative event\n",
		},
		{
			name: "numeric_timestamp",
			data: `{"time":42,"level":"INFO","msg":"event"}`,
			want: "INFO time=42 event\n",
		},
		{
			name: "missing_level",
			data: `{"msg":"event"}`,
			want: "event\n",
		},
		{
			name: "reordered_metadata_and_custom_level",
			data: `{"msg":"event","id":7,"level":"NOTICE","time":"2026-09-16T10:11:12Z"}`,
			want: "NOTICE[10:11:12] event id=7\n",
		},
		{
			name: "renamed_metadata",
			data: `{"timestamp":"2026-09-16T10:11:12Z","severity":"NOTICE","message":"event"}`,
			want: "timestamp=2026-09-16T10:11:12Z severity=NOTICE message=event\n",
		},
		{
			name: "prefix_without_message",
			data: `{"level":"INFO","time":"2026-09-16T10:11:12Z"}`,
			want: "INFO[10:11:12]\n",
		},
		{
			name: "numeric_level",
			data: `{"level":7,"msg":"event"}`,
			want: "7 event\n",
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			var destination bytes.Buffer
			formatter := TextFormatter{DisableColors: true, TimestampFormat: "15:04:05", ShowLevel: true}
			if err := New(&destination, &formatter).WriteRecord(core.NewRecord(slog.LevelInfo, []byte(testCase.data))); err != nil {
				test.Fatal(err)
			}
			if destination.String() != testCase.want {
				test.Fatalf("output = %q, want %q", destination.String(), testCase.want)
			}
		})
	}
}

func TestTextFormatterRejectsMalformedJSON(test *testing.T) {
	for _, data := range []string{"", "[]", `{"msg":`, `{"msg":"ok"} {}`} {
		test.Run(data, func(test *testing.T) {
			var destination bytes.Buffer
			err := New(&destination, &TextFormatter{}).WriteRecord(core.NewRecord(slog.LevelInfo, []byte(data)))
			if err == nil || destination.Len() != 0 {
				test.Fatalf("error=%v output=%q, want error without partial output", err, destination.String())
			}
		})
	}
}
