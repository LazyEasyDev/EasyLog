package terminal

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTextFormatterPaletteAndResets(t *testing.T) {
	for _, tc := range []struct {
		level slog.Level
		label string
		color string
	}{
		{slog.LevelDebug, "DEBU", "\x1b[90m"},
		{slog.LevelInfo, "INFO", "\x1b[36m"},
		{slog.LevelWarn, "WARN", "\x1b[33m"},
		{slog.LevelError, "ERRO", "\x1b[31m"},
		{slog.LevelInfo + 1, "INFO+1", ""},
	} {
		t.Run(tc.label, func(t *testing.T) {
			record := slog.NewRecord(time.Time{}, tc.level, "message", 0)
			record.AddAttrs(slog.String("field", "value"))
			for _, disabled := range []bool{false, true} {
				f := TextFormatter{ForceColors: true, DisableColors: disabled, DisableTimestamp: true, ShowLevel: true}
				got, err := f.FormatRecord(record)
				color, reset := tc.color, "\x1b[0m"
				if disabled || color == "" {
					color, reset = "", ""
				}
				want := color + tc.label + reset + " message " + color + "field" + reset + "=value\n"
				if err != nil || string(got) != want {
					t.Fatalf("disabled=%v: got %q, %v; want %q", disabled, got, err, want)
				}
			}
		})
	}
}

func TestTextFormatterEscapesAndUnicode(t *testing.T) {
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "hello\n\r\t\x1b[31m\x00 café 世界", 0)
	record.AddAttrs(slog.String("bad\nkey", "quoted=\"x\"\n\x1b[0m"), slog.String("empty", ""),
		slog.Group("group", slog.String("s", "\x1b[31m"), slog.Int("n", 2)))
	f := TextFormatter{ShowLevel: true, DisableTimestamp: true}
	got, err := f.FormatRecord(record)
	want := "INFO hello\\n\\r\\t\\x1b[31m\\x00 café 世界 \"bad\\nkey\"=\"quoted=\\\"x\\\"\\n\\x1b[0m\" empty=\"\" group={\"s\":\"\\u001b[31m\",\"n\":2}\n"
	if err != nil || string(got) != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
	if bytes.ContainsRune(got, '\x1b') || bytes.Count(got, []byte{'\n'}) != 1 {
		t.Fatal("unescaped terminal control sequence or extra physical line")
	}
}

func TestTextFormatterTimestampAndHiddenLevel(t *testing.T) {
	record := slog.NewRecord(time.Date(2026, 9, 18, 12, 34, 56, 0, time.UTC), slog.LevelInfo, "message", 0)
	record.AddAttrs(slog.String("field", "value"))
	for _, tc := range []struct {
		name string
		f    TextFormatter
		want string
	}{
		{"default", DefaultTextFormatter(), "INFO[0000] message field=value\n"},
		{"zero", TextFormatter{}, "[0000] message field=value\n"},
		{"layout", TextFormatter{ShowLevel: true, TimestampFormat: "15:04:05"}, "INFO[12:34:56] message field=value\n"},
		{"layout-controls", TextFormatter{TimestampFormat: "15:04:05\n\x1b"}, "[12:34:56\\n\\x1b] message field=value\n"},
		{"hidden-level", TextFormatter{ForceColors: true, DisableTimestamp: true}, "message \x1b[36mfield\x1b[0m=value\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.f.FormatRecord(record)
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestOutputCopiesFormatterAndDoesNotColorBuffers(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	var buffer bytes.Buffer
	f := DefaultTextFormatter()
	f.DisableTimestamp = true
	output := New(&buffer, &f)
	f.ForceColors = true
	f.ShowLevel = false
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "message", 0)
	if err := output.WriteRecord(record, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if got := buffer.String(); got != "INFO message\n" {
		t.Fatalf("buffer or copied configuration changed: %q", got)
	}
}

func TestExplicitColorPrecedence(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	for _, disabled := range []bool{false, true} {
		var buffer bytes.Buffer
		f := TextFormatter{ForceColors: true, DisableColors: disabled, ShowLevel: true, DisableTimestamp: true}
		output := New(&buffer, &f)
		if err := output.WriteRecord(slog.NewRecord(time.Time{}, slog.LevelInfo, "message", 0), []byte("{}\n")); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buffer.String(), "\x1b[") == disabled {
			t.Fatalf("DisableColors=%v: incorrect precedence: %q", disabled, buffer.String())
		}
	}
}

func TestJSONOutputDoesNotAddANSI(t *testing.T) {
	var buffer bytes.Buffer
	content := []byte("{\"msg\":\"café 世界\"}\n")
	if err := NewJSON(&buffer).WriteRecord(slog.Record{}, content); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buffer.Bytes(), content) {
		t.Fatalf("JSON changed: %q", buffer.Bytes())
	}
}

func TestClosedFileIsNotColorTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	file, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if useColors(file, false) {
		t.Fatal("closed regular file detected as color terminal")
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(data []byte) (int, error) {
	return w.Buffer.Write(data[:min(3, len(data))])
}

func TestOutputCompletesShortWrites(t *testing.T) {
	w := &shortWriter{}
	content := []byte("{\"msg\":\"complete\"}\n")
	if err := NewJSON(w).WriteRecord(slog.Record{}, content); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.Bytes(), content) {
		t.Fatalf("short write lost bytes: %q", w.Bytes())
	}
	if err := writeAll(zeroWriter{}, content); err != io.ErrShortWrite {
		t.Fatalf("zero write: %v", err)
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }
