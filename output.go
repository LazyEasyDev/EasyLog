// Package easylog provides a synchronous slog handler with configurable
// output fan-out and optional bounded in-memory retention.
package easylog

import "log/slog"

// Output receives prepared records and their shared JSON encoding.
// A runtime serializes its calls to these methods. Outputs shared between
// runtimes must provide their own concurrency protection and lifecycle coordination.
// Methods must not log through or invoke lifecycle methods on the same runtime.
type Output interface {
	// WriteRecord receives final display fields in record.Attrs(), including metadata.
	// The fixed Time, Level, Message, and PC fields retain the original event metadata.
	// jsonContent contains the same display fields as a JSON object without a trailing newline.
	// Outputs add any framing or separators required by their destination.
	// Both arguments may be retained but must not be modified; their storage is shared
	// with other outputs and memory, and is not recycled for later log calls.
	WriteRecord(record slog.Record, jsonContent []byte) error
	// Sync flushes output-owned buffers and synchronizes storage where supported.
	Sync() error
	// Close finishes pending work and releases output-owned resources.
	// It must be safe to call more than once.
	Close() error
}
