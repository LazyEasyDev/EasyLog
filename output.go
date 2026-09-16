// Package easylog provides a synchronous slog handler with configurable
// output fan-out and optional bounded in-memory FIFO consumption.
package easylog

import "github.com/LazyEasyDev/EasyLog/internal/core"

// Record is an immutable encoded log record.
type Record = core.Record

// Output receives encoded records and manages its own resources.
// A runtime serializes its calls to these methods. Outputs shared between
// runtimes must provide their own concurrency protection and lifecycle coordination.
// Methods must not log through or invoke lifecycle methods on the same runtime.
type Output interface {
	WriteRecord(Record) error
	// Sync flushes output-owned buffers and synchronizes storage where supported.
	Sync() error
	// Close finishes pending work and releases output-owned resources.
	// It must be safe to call more than once.
	Close() error
}
