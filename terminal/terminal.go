// Package terminal provides text and explicit NDJSON output for EasyLog.
package terminal

import (
	"io"
	"os"
	"sync"
	"time"
)

// Output writes complete records to a caller-owned writer.
type Output struct {
	mu        sync.Mutex
	writer    io.Writer
	formatter *TextFormatter
	startedAt time.Time
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

// New creates text output; a nil writer uses os.Stderr and a nil formatter uses defaults.
// The formatter is copied; colors are detected and elapsed time starts here.
func New(writer io.Writer, formatter *TextFormatter) *Output {
	output := NewJSON(writer)
	configured := GetDefaultTextFormatter()
	if formatter != nil {
		configured = *formatter
	}
	configured.ForceColors = !configured.DisableColors && (configured.ForceColors || autoColors(output.writer))
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

// WriteRecord forwards JSON bytes unchanged, with empty input a no-op.
// Text mode validates one JSON object before writing its formatted line.
func (o *Output) WriteRecord(jsonContent []byte) error {
	if len(jsonContent) == 0 {
		return nil
	}
	data := jsonContent
	if o.formatter != nil {
		var err error
		data, err = o.formatter.format(jsonContent, time.Since(o.startedAt))
		if err != nil {
			return err
		}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return writeAll(o.writer, data)
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
