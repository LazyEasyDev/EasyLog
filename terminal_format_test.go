package easylog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	easylog "github.com/LazyEasyDev/EasyLog"
)

func TestInitTerminalFormatterOnlyChangesDisplay(test *testing.T) {
	for _, testCase := range []struct {
		name      string
		formatter *easylog.TerminalFormatter
		pattern   string
	}{
		{
			name:    "nil_formatter_uses_default_text",
			pattern: `WARN\[[0-9]{4,}\] application started service=api\n`,
		},
		{
			name:      "zero_formatter_is_text",
			formatter: &easylog.TerminalFormatter{},
			pattern:   `\[[0-9]{4,}\] application started service=api\n`,
		},
		{
			name:      "elapsed_timestamp",
			formatter: &easylog.TerminalFormatter{DisableColors: true, ShowLevel: true},
			pattern:   `WARN\[[0-9]{4,}\] application started service=api\n`,
		},
		{
			name:      "custom_timestamp",
			formatter: &easylog.TerminalFormatter{DisableColors: true, TimestampFormat: "15:04:05", ShowLevel: true},
			pattern:   `WARN\[10:11:12\] application started service=api\n`,
		},
		{
			name:      "hidden_metadata",
			formatter: &easylog.TerminalFormatter{DisableColors: true, DisableTimestamp: true},
			pattern:   `application started service=api\n`,
		},
		{
			name:      "automatic_unknown_writer_is_plain",
			formatter: &easylog.TerminalFormatter{DisableTimestamp: true},
			pattern:   `application started service=api\n`,
		},
		{
			name:      "forced_color_without_level",
			formatter: &easylog.TerminalFormatter{ForceColors: true, DisableTimestamp: true},
			pattern:   regexp.QuoteMeta("application started \x1b[33mservice\x1b[0m=api\n"),
		},
		{
			name:      "forced_color_with_level",
			formatter: &easylog.TerminalFormatter{ForceColors: true, DisableTimestamp: true, ShowLevel: true},
			pattern:   regexp.QuoteMeta("\x1b[33mWARN\x1b[0m application started \x1b[33mservice\x1b[0m=api\n"),
		},
		{
			name:      "disabled_colors_override_force",
			formatter: &easylog.TerminalFormatter{ForceColors: true, DisableColors: true, DisableTimestamp: true},
			pattern:   `application started service=api\n`,
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			previous := slog.Default()
			directory := test.TempDir()
			var destination bytes.Buffer
			if err := easylog.Init(easylog.InitOptions{
				Runtime: easylog.Options{MemoryMaxBytes: 1024},
				File:    &easylog.FileOptions{Directory: directory},
				Terminal: &easylog.TerminalOptions{
					Writer:    &destination,
					Formatter: testCase.formatter,
				},
			}); err != nil {
				test.Fatal(err)
			}
			test.Cleanup(func() {
				if err := easylog.Close(); err != nil {
					test.Error(err)
				}
			})

			timestamp := time.Date(2026, time.September, 16, 10, 11, 12, 123456789, time.UTC)
			record := slog.NewRecord(timestamp, slog.LevelWarn, "application started", 0)
			record.AddAttrs(slog.String("service", "api"))
			if err := slog.Default().Handler().Handle(context.Background(), record); err != nil {
				test.Fatal(err)
			}
			if !regexp.MustCompile("^" + testCase.pattern + "$").Match(destination.Bytes()) {
				test.Fatalf("terminal output = %q, want pattern %q", destination.String(), testCase.pattern)
			}
			consumer := easylog.Consumer()
			if count := consumer.Len(); count != 1 {
				test.Fatalf("memory records=%d, want one record", count)
			}
			if err := easylog.Close(); err != nil {
				test.Fatal(err)
			}
			if slog.Default() != previous {
				test.Fatal("Close did not restore the previous default logger")
			}
			paths, err := filepath.Glob(filepath.Join(directory, "logs", "warn_*.jsonl"))
			if err != nil || len(paths) != 1 {
				test.Fatalf("file paths=%v error=%v, want one warn segment", paths, err)
			}
			fileData, err := os.ReadFile(paths[0])
			if err != nil {
				test.Fatal(err)
			}
			if bytes.Count(fileData, []byte{'\n'}) != 1 || !bytes.HasSuffix(fileData, []byte{'\n'}) {
				test.Fatalf("file output is not one complete JSON line: %q", fileData)
			}
			data := bytes.TrimSuffix(fileData, []byte{'\n'})
			var decoded map[string]any
			if err := json.Unmarshal(data, &decoded); err != nil {
				test.Fatal(err)
			}
			if decoded["time"] != timestamp.Format(time.RFC3339Nano) || decoded["level"] != "WARN" || decoded["msg"] != "application started" || decoded["service"] != "api" {
				test.Fatalf("terminal formatting changed JSON: %s", data)
			}
			if size := consumer.Bytes(); size != int64(len(data)) {
				test.Fatalf("memory bytes=%d, want %d", size, len(data))
			}
		})
	}
}

func TestInitCopiesTerminalConfiguration(test *testing.T) {
	var destination, replacement bytes.Buffer
	formatter := easylog.TerminalFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
		ShowLevel:        true,
	}
	terminalOptions := &easylog.TerminalOptions{
		Writer:    &destination,
		Formatter: &formatter,
	}
	original := formatter
	if err := easylog.Init(easylog.InitOptions{Terminal: terminalOptions}); err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() {
		if err := easylog.Close(); err != nil {
			test.Error(err)
		}
	})
	if formatter != original || terminalOptions.Writer != &destination || terminalOptions.Formatter != &formatter {
		test.Fatal("Init modified the caller's terminal configuration")
	}
	slog.Info("first")
	formatter = easylog.TerminalFormatter{ForceColors: true}
	terminalOptions.Writer = &replacement
	terminalOptions.Formatter = nil
	slog.Info("second")
	if want := "INFO first\nINFO second\n"; destination.String() != want {
		test.Fatalf("terminal output = %q, want %q", destination.String(), want)
	}
	if replacement.Len() != 0 {
		test.Fatal("changing terminal options replaced the installed writer")
	}
}
