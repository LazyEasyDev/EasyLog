// Package terminal provides a plain NDJSON EasyLog output.
package terminal

import (
	"io"
	"os"
	"sync"

	"github.com/LazyEasyDev/EasyLog/internal/core"
)

// Output writes complete records to a caller-owned writer.
type Output struct {
	mu     sync.Mutex
	writer io.Writer
}

// New creates a terminal output. A nil writer defaults to os.Stderr.
func New(writer io.Writer) *Output {
	if writer == nil {
		writer = os.Stderr
	}
	return &Output{writer: writer}
}

// WriteRecord writes one JSON object followed by exactly one newline.
func (o *Output) WriteRecord(record core.Record) error {
	data := append(record.JSON(), '\n')
	o.mu.Lock()
	defer o.mu.Unlock()
	return writeAll(o.writer, data)
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
