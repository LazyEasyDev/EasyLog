// Package easylog provides a synchronous slog handler with configurable
// output fan-out and optional bounded in-memory retention.
package easylog

// Output receives complete JSON lines. Methods must not reenter the same runtime.
type Output interface {
	// The newline-terminated bytes may be retained but must not be modified.
	WriteRecord(jsonContent []byte) error
	Sync() error
	Close() error
}
