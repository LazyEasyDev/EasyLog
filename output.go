// Package easylog provides a synchronous slog handler with independent
// terminal/file fan-out and optional bounded in-memory FIFO consumption.
package easylog

import (
	fileoutput "github.com/LazyEasyDev/EasyLog/file"
	"github.com/LazyEasyDev/EasyLog/internal/core"
	terminaloutput "github.com/LazyEasyDev/EasyLog/terminal"
)

// Record is an immutable encoded log record.
type Record = core.Record

// Outputs contains the optional built-in synchronous outputs used by a Runtime.
// Runtime does not own or close these outputs. Both fields may be nil.
type Outputs struct {
	Terminal *terminaloutput.Output
	File     *fileoutput.Output
}
