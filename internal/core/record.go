package core

import (
	"bytes"
	"log/slog"
)

// Record is an immutable encoded log record.
type Record struct {
	level slog.Level
	json  []byte
}

// NewRecord creates an immutable encoded log record.
func NewRecord(level slog.Level, data []byte) Record {
	return Record{level: level, json: data}
}

// Level returns the record's log level.
func (r Record) Level() slog.Level {
	return r.level
}

// JSON returns a copy of the encoded JSON object without a trailing newline.
func (r Record) JSON() []byte {
	return bytes.Clone(r.json)
}

// Size returns the encoded JSON size in bytes.
func (r Record) Size() int64 {
	return int64(len(r.json))
}
