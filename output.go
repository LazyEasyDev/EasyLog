// Package EasyLog provides a synchronous slog handler with configurable
// output fan-out and optional bounded in-memory retention.
package EasyLog

import "log/slog"

// Output receives prepared records and complete JSON lines. Methods must not reenter the same runtime.
type Output interface {
	// The record includes bound fields, groups, and resolved/replaced custom attributes.
	// Source is a source attribute only when enabled; JSON-encoded values use json.RawMessage.
	// Treat attributes, group slices, and raw JSON as read-only; clone before adding attributes.
	// The newline-terminated bytes may be retained but must not be modified.
	WriteRecord(record slog.Record, jsonContent []byte) error
	Sync() error
	Close() error
}
