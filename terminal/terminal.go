// Package terminal provides text and explicit NDJSON output for EasyLog.
package terminal

import (
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"
)

const maxLineBufferBytes = 64 * 1024

// Output writes complete records to a caller-owned writer.
type Output struct {
	mu         sync.Mutex
	writer     io.Writer
	formatter  *TextFormatter
	startedAt  time.Time
	lineBuffer []byte
}

func GetDefaultTextFormatter() TextFormatter {
	return TextFormatter{
		ForceColors:      false,
		DisableColors:    false,
		DisableTimestamp: false,
		TimestampFormat:  "",
		ShowLevel:        true,
	}
}

// New creates a text output. A nil writer defaults to os.Stderr.
// A nil formatter uses elapsed timestamps, a visible level, and automatic colors.
// A supplied formatter is copied; its zero value hides the level.
// Automatic colors are detected and elapsed timestamps start at output creation.
func New(writer io.Writer, formatter *TextFormatter) *Output {
	output := NewJSON(writer)
	configured := GetDefaultTextFormatter()
	if formatter != nil {
		configured = *formatter
	}
	configured.DisableColors = configured.DisableColors || (!configured.ForceColors && !autoColors(output.writer))
	output.formatter = &configured
	output.startedAt = time.Now()
	return output
}

// NewJSON creates an NDJSON output without text formatting or ANSI colors.
// A nil writer defaults to os.Stderr. The writer remains caller-owned.
func NewJSON(writer io.Writer) *Output {
	if writer == nil {
		writer = os.Stderr
	}
	return &Output{writer: writer}
}

func autoColors(writer io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := writer.(*os.File)
	if !ok || file == nil {
		return false
	}
	connection, err := file.SyscallConn()
	if err != nil {
		return false
	}
	supported := false
	err = connection.Control(func(descriptor uintptr) {
		supported = isColorTerminal(descriptor)
	})
	return err == nil && supported
}

// WriteRecord writes one JSON or text record followed by exactly one newline.
func (o *Output) WriteRecord(record slog.Record, jsonContent []byte) error {
	var data []byte
	if o.formatter != nil {
		var err error
		data, err = o.formatter.format(record, time.Since(o.startedAt))
		if err != nil {
			return err
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.formatter != nil {
		return writeAll(o.writer, data)
	}
	o.lineBuffer = slices.Grow(o.lineBuffer[:0], len(jsonContent)+1)
	o.lineBuffer = append(o.lineBuffer, jsonContent...)
	o.lineBuffer = append(o.lineBuffer, '\n')
	err := writeAll(o.writer, o.lineBuffer)
	if cap(o.lineBuffer) > maxLineBufferBytes {
		o.lineBuffer = nil
	}
	return err
}

// Sync is a no-op because Output has no pending buffered writes.
// Any buffering in the caller-owned writer must be flushed by the caller.
func (o *Output) Sync() error {
	return nil
}

// Close is a no-op. The caller retains ownership of the writer.
func (o *Output) Close() error {
	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written > 0 {
			data = data[written:]
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
