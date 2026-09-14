package file

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LazyEasyDev/EasyLog/internal/core"
)

func TestDefaultOptions(t *testing.T) {
	options, err := normalizeOptions(Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if options.MaxSegmentBytes != 8<<20 {
		t.Fatalf("default segment size = %d, want %d", options.MaxSegmentBytes, 8<<20)
	}
	if options.MaxSegments != 7 {
		t.Fatalf("default segment count = %d, want 7", options.MaxSegments)
	}
}

func TestOptionsRequireAbsoluteDirectory(t *testing.T) {
	for _, directory := range []string{"", ".", "logs"} {
		if _, err := normalizeOptions(Options{Directory: directory}); err == nil {
			t.Fatalf("relative directory %q was accepted", directory)
		}
	}
}

func TestSegmentNamesUseDateAndSequence(t *testing.T) {
	directory := t.TempDir()
	day := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	times := []time.Time{
		day,
		day.Add(6 * time.Hour),
		day.Add(24 * time.Hour),
	}
	index := 0
	output, err := newWithClock(Options{
		Directory: directory, MaxSegmentBytes: 1, MaxSegments: 3,
	}, func() time.Time {
		result := times[index]
		index++
		return result
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"first", "second", "third"} {
		writeMessage(t, output, slog.LevelInfo, message)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(directory, logsDirectoryName))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	expected := []string{
		"info_20260914_0.jsonl",
		"info_20260914_1.jsonl",
		"info_20260915_0.jsonl",
	}
	if !slices.Equal(names, expected) {
		t.Fatalf("segment names = %v, want %v", names, expected)
	}
}

func TestOutputRotatesWhenUTCDateChanges(t *testing.T) {
	directory := t.TempDir()
	times := []time.Time{
		time.Date(2026, 9, 14, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
	}
	index := 0
	output, err := newWithClock(Options{
		Directory: directory, MaxSegmentBytes: 1 << 20, MaxSegments: 3,
	}, func() time.Time {
		result := times[index]
		index++
		return result
	})
	if err != nil {
		t.Fatal(err)
	}
	writeMessage(t, output, slog.LevelInfo, "before midnight")
	writeMessage(t, output, slog.LevelInfo, "after midnight")
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	segments := matchingFiles(t, filepath.Join(directory, logsDirectoryName), segmentPattern(infoPrefix))
	expected := []string{"info_20260914_0.jsonl", "info_20260915_0.jsonl"}
	if !slices.Equal(segments, expected) {
		t.Fatalf("segments = %v, want %v", segments, expected)
	}
}

func TestOutputRollsWholeRecordsAndRetainsNewestSegments(t *testing.T) {
	directory := t.TempDir()
	unrelated := filepath.Join(directory, "keep.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	current := time.Date(2026, 9, 13, 14, 32, 5, 123, time.UTC)
	output, err := newWithClock(Options{
		Directory: directory, MaxSegmentBytes: 1, MaxSegments: 2,
	}, func() time.Time {
		result := current
		current = current.Add(time.Nanosecond)
		return result
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"first", "second", "third"} {
		writeMessage(t, output, slog.LevelInfo, message)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	store := output.storeForLevel(slog.LevelInfo)
	logsDirectory := filepath.Join(directory, logsDirectoryName)
	segments := matchingFiles(t, logsDirectory, store.pattern)
	if len(segments) != 2 {
		t.Fatalf("got %d segments, want 2: %v", len(segments), segments)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated file changed: %v", err)
	}

	var messages []string
	for _, name := range segments {
		data, err := os.ReadFile(filepath.Join(logsDirectory, name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(data, []byte{'\n'}) != 1 || !bytes.HasSuffix(data, []byte{'\n'}) {
			t.Fatalf("segment %q does not contain one complete record: %q", name, data)
		}
		var decoded map[string]any
		if err := json.Unmarshal(bytes.TrimSuffix(data, []byte{'\n'}), &decoded); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, decoded["msg"].(string))
	}
	if messages[0] != "second" || messages[1] != "third" {
		t.Fatalf("retained messages = %v, want [second third]", messages)
	}
}

func TestSegmentNamesIgnoreTimeOfDay(t *testing.T) {
	directory := t.TempDir()
	initial := time.Date(2026, 9, 13, 14, 32, 5, 0, time.UTC)
	times := []time.Time{initial, initial.Add(-time.Hour)}
	index := 0
	output, err := newWithClock(Options{
		Directory: directory, MaxSegmentBytes: 1, MaxSegments: 3,
	}, func() time.Time {
		result := times[index]
		index++
		return result
	})
	if err != nil {
		t.Fatal(err)
	}
	writeMessage(t, output, slog.LevelInfo, "one")
	writeMessage(t, output, slog.LevelInfo, "two")
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	store := output.storeForLevel(slog.LevelInfo)
	segments := matchingFiles(t, filepath.Join(directory, logsDirectoryName), store.pattern)
	expected := []string{
		"info_20260913_0.jsonl",
		"info_20260913_1.jsonl",
	}
	if !slices.Equal(segments, expected) {
		t.Fatalf("segment names = %v, want %v", segments, expected)
	}
}

func TestWriteAllHandlesShortWrites(t *testing.T) {
	writer := &shortWriter{limit: 2}
	written, err := writeAll(writer, []byte("abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if written != 6 || writer.String() != "abcdef" {
		t.Fatalf("written=%d data=%q", written, writer.String())
	}
}

func TestWriteFailureRetiresActiveSegment(t *testing.T) {
	directory := t.TempDir()
	output, err := New(Options{Directory: directory, MaxSegments: 3})
	if err != nil {
		t.Fatal(err)
	}
	writeMessage(t, output, slog.LevelInfo, "first")

	store := output.storeForLevel(slog.LevelInfo)
	failedSegment := store.activeSegment.name
	if err := store.active.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.WriteRecord(encodedRecord(slog.LevelInfo, "failed")); err == nil {
		t.Fatal("write to closed segment succeeded")
	}
	if store.active != nil {
		t.Fatal("failed segment remains active")
	}

	writeMessage(t, output, slog.LevelInfo, "recovered")
	if store.activeSegment.name == failedSegment {
		t.Fatal("next write reused the failed segment")
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateFailureKeepsCurrentSegmentUsable(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "logs")
	output, err := newWithClock(Options{
		Directory: directory, MaxSegmentBytes: 1, MaxSegments: 2,
	}, func() time.Time {
		return time.Date(2026, 9, 13, 14, 32, 5, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	writeMessage(t, output, slog.LevelInfo, "first")

	movedDirectory := filepath.Join(parent, "moved")
	if err := os.Rename(directory, movedDirectory); err != nil {
		t.Fatal(err)
	}
	_ = output.WriteRecord(encodedRecord(slog.LevelInfo, "second"))
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(movedDirectory, logsDirectoryName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d files, want the original active segment", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(movedDirectory, logsDirectoryName, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte{'\n'}) != 2 {
		t.Fatalf("active segment did not receive both records: %q", data)
	}
}

func TestCreateFailureWithoutActiveSegmentReturnsError(t *testing.T) {
	directory := t.TempDir()
	output, err := New(Options{Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	logsDirectory := filepath.Join(directory, logsDirectoryName)
	if err := os.Rename(logsDirectory, logsDirectory+".moved"); err != nil {
		t.Fatal(err)
	}

	if err := output.WriteRecord(encodedRecord(slog.LevelInfo, "lost")); err == nil {
		t.Fatal("write without a writable segment succeeded")
	}
	if output.storeForLevel(slog.LevelInfo).active != nil {
		t.Fatal("failed segment creation installed an active segment")
	}
}

func TestCleanupFailureDoesNotDeleteReplacementOrStopNewActiveSegment(t *testing.T) {
	directory := t.TempDir()
	current := time.Date(2026, 9, 13, 14, 32, 5, 0, time.UTC)
	output, err := newWithClock(Options{
		Directory: directory, MaxSegmentBytes: 1, MaxSegments: 1,
	}, func() time.Time {
		result := current
		current = current.Add(time.Nanosecond)
		return result
	})
	if err != nil {
		t.Fatal(err)
	}
	writeMessage(t, output, slog.LevelInfo, "first")

	store := output.storeForLevel(slog.LevelInfo)
	oldPath := store.activeSegment.path
	if err := os.Rename(oldPath, oldPath+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(oldPath, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(oldPath, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := output.WriteRecord(encodedRecord(slog.LevelInfo, "second")); err != nil {
		t.Fatalf("successful write returned cleanup failure: %v", err)
	}
	latestPath := store.activeSegment.path
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("replacement path was modified: %v", err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"msg":"second"`)) {
		t.Fatalf("new active segment was not written: %q", data)
	}
}

func TestRestartAppendsToLatestSegmentWithRoom(t *testing.T) {
	directory := t.TempDir()
	fixedTime := time.Date(2026, 9, 13, 14, 32, 5, 0, time.UTC)
	options := Options{
		Directory: directory, MaxSegmentBytes: 1 << 20, MaxSegments: 2,
	}

	for _, message := range []string{"first", "second", "third"} {
		output, err := newWithClock(options, func() time.Time { return fixedTime })
		if err != nil {
			t.Fatal(err)
		}
		writeMessage(t, output, slog.LevelInfo, message)
		if err := output.Close(); err != nil {
			t.Fatal(err)
		}
	}

	pattern := segmentPattern(infoPrefix)
	logsDirectory := filepath.Join(directory, logsDirectoryName)
	segments := matchingFiles(t, logsDirectory, pattern)
	if !slices.Equal(segments, []string{"info_20260913_0.jsonl"}) {
		t.Fatalf("segments = %v, want [info_20260913_0.jsonl]", segments)
	}
	data, err := os.ReadFile(filepath.Join(logsDirectory, segments[0]))
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'}) {
		var decoded map[string]any
		if err := json.Unmarshal(line, &decoded); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, decoded["msg"].(string))
	}
	if !slices.Equal(messages, []string{"first", "second", "third"}) {
		t.Fatalf("messages = %v, want [first second third]", messages)
	}
}

func TestRestartCreatesNextSequenceWhenLatestIsFull(t *testing.T) {
	directory := t.TempDir()
	logsDirectory := filepath.Join(directory, logsDirectoryName)
	if err := os.MkdirAll(logsDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logsDirectory, "info_20260913_0.jsonl")
	existing := []byte("{\"msg\":\"existing\"}\n")
	if err := os.WriteFile(path, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDirectory, "info_20260913_2.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := newWithClock(Options{
		Directory: directory, MaxSegmentBytes: 3, MaxSegments: 3,
	}, func() time.Time {
		return time.Date(2026, 9, 13, 14, 32, 5, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	writeMessage(t, output, slog.LevelInfo, "new")
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("existing segment was removed: %v", readErr)
	}
	if !bytes.Equal(data, existing) {
		t.Fatalf("existing segment changed: %q", data)
	}
	newPath := filepath.Join(logsDirectory, "info_20260913_3.jsonl")
	data, err = os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"msg":"new"`)) {
		t.Fatalf("new segment data = %q", data)
	}
}

func TestRestartCreatesNextSequenceWhenLatestIsIncomplete(t *testing.T) {
	directory := t.TempDir()
	logsDirectory := filepath.Join(directory, logsDirectoryName)
	if err := os.MkdirAll(logsDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	incompletePath := filepath.Join(logsDirectory, "info_20260913_0.jsonl")
	incomplete := []byte(`{"msg":"incomplete"}`)
	if err := os.WriteFile(incompletePath, incomplete, 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := newWithClock(Options{
		Directory: directory, MaxSegmentBytes: 1 << 20, MaxSegments: 2,
	}, func() time.Time {
		return time.Date(2026, 9, 13, 14, 32, 5, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := output.WriteRecord(encodedRecord(slog.LevelInfo, "new")); err != nil {
		t.Fatalf("successful fallback write returned resume failure: %v", err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(incompletePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, incomplete) {
		t.Fatalf("incomplete segment changed: %q", data)
	}
	data, err = os.ReadFile(filepath.Join(logsDirectory, "info_20260913_1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"msg":"new"`)) {
		t.Fatalf("new segment data = %q", data)
	}
}

func TestRestartRetentionOrdersSuffixesNumerically(t *testing.T) {
	directory := t.TempDir()
	logsDirectory := filepath.Join(directory, logsDirectoryName)
	if err := os.MkdirAll(logsDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"info_20260913_0.jsonl",
		"info_20260913_2.jsonl",
		"info_20260913_10.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(logsDirectory, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	output, err := newWithClock(Options{
		Directory: directory, MaxSegmentBytes: 3, MaxSegments: 2,
	}, func() time.Time {
		return time.Date(2026, 9, 13, 14, 32, 6, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	writeMessage(t, output, slog.LevelInfo, "new")
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(logsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	expected := []string{
		"info_20260913_10.jsonl",
		"info_20260913_11.jsonl",
	}
	if !slices.Equal(names, expected) {
		t.Fatalf("retained segments = %v, want %v", names, expected)
	}
}

func TestOutputRoutesStandardLevelsToPrefixedFiles(t *testing.T) {
	directory := t.TempDir()
	current := time.Date(2026, 9, 13, 14, 32, 5, 0, time.UTC)
	output, err := newWithClock(Options{Directory: directory}, func() time.Time {
		result := current
		current = current.Add(time.Nanosecond)
		return result
	})
	if err != nil {
		t.Fatal(err)
	}
	writeMessage(t, output, slog.LevelDebug, "debug")
	writeMessage(t, output, slog.LevelInfo, "info")
	writeMessage(t, output, slog.LevelWarn, "warning")
	writeMessage(t, output, slog.LevelError, "error")
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}

	rootEntries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootEntries) != 1 || rootEntries[0].Name() != "logs" || !rootEntries[0].IsDir() {
		t.Fatalf("root entries = %v, want one logs directory", rootEntries)
	}

	expected := map[string]string{
		"debug_": "debug",
		"info_":  "info",
		"warn_":  "warning",
		"err_":   "error",
	}
	entries, err := os.ReadDir(filepath.Join(directory, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(expected) {
		t.Fatalf("log files = %d, want %d", len(entries), len(expected))
	}
	for _, entry := range entries {
		var message string
		for prefix, expectedMessage := range expected {
			if strings.HasPrefix(entry.Name(), prefix) {
				message = expectedMessage
				break
			}
		}
		if message == "" {
			t.Fatalf("unexpected log filename %q", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(directory, "logs", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(bytes.TrimSuffix(data, []byte{'\n'}), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["msg"] != message {
			t.Fatalf("%s message = %v, want %q", entry.Name(), decoded["msg"], message)
		}
	}
}

func writeMessage(t *testing.T, output *Output, level slog.Level, message string) {
	t.Helper()
	if err := output.WriteRecord(encodedRecord(level, message)); err != nil {
		t.Fatal(err)
	}
}

func encodedRecord(level slog.Level, message string) core.Record {
	data, _ := json.Marshal(map[string]string{
		"level": level.String(),
		"msg":   message,
	})
	return core.NewRecord(0, level, data)
}

func matchingFiles(t *testing.T, directory string, pattern interface{ MatchString(string) bool }) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	for _, entry := range entries {
		if pattern.MatchString(entry.Name()) {
			result = append(result, entry.Name())
		}
	}
	sort.Strings(result)
	return result
}

type shortWriter struct {
	bytes.Buffer
	limit int
}

func (w *shortWriter) Write(data []byte) (int, error) {
	if len(data) > w.limit {
		data = data[:w.limit]
	}
	return w.Buffer.Write(data)
}
