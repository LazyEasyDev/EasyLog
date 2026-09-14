package core

import (
	"bytes"
	"log/slog"
)

// Record is an immutable encoded log record.
type Record struct {
	sequence uint64
	level    slog.Level
	json     []byte
}

// NewRecord creates an immutable encoded log record.
func NewRecord(sequence uint64, level slog.Level, data []byte) Record {
	return Record{sequence: sequence, level: level, json: data}
}

// Sequence returns the process-local ingestion sequence.
func (r Record) Sequence() uint64 {
	return r.sequence
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
