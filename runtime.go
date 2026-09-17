package easylog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

// ErrClosed indicates that runtime shutdown has started.
var ErrClosed = errors.New("easylog: closed")

// Enricher extracts attributes from a log call's context.
type Enricher func(context.Context) []slog.Attr

// DefaultMemoryMaxBytes is the recommended memory retention limit.
const DefaultMemoryMaxBytes int64 = 8 * 1024 * 1024

// Options configures an EasyLog runtime.
type Options struct {
	// Level defaults to INFO when nil or a nil *slog.LevelVar.
	// Other typed-nil Levelers are not supported.
	Level       slog.Leveler
	AddSource   bool
	ReplaceAttr func(groups []string, attr slog.Attr) slog.Attr
	// MemoryMaxBytes enables bounded memory retention when positive.
	MemoryMaxBytes int64
	Enrichers      []Enricher
}

type runtimeState struct {
	level       slog.Leveler
	addSource   bool
	replaceAttr func([]string, slog.Attr) slog.Attr
	enrichers   []Enricher
	encoders    sync.Pool

	memory   *memoryStore
	outputs  []Output
	outputMu sync.Mutex
	closed   atomic.Bool
}

// Runtime coordinates logging, outputs, and optional memory retention.
// Stop producers before Close; output methods must not reenter the runtime.
type Runtime struct {
	state *runtimeState
	root  *handler
}

// New copies the output slice, skipping nil entries; typed-nil outputs are unsupported.
// It manages output writes, Sync, and Close; outputs own their underlying resources.
func New(options Options, outputs []Output) *Runtime {
	level, isLevelVar := options.Level.(*slog.LevelVar)
	if options.Level == nil || (isLevelVar && level == nil) {
		options.Level = slog.LevelInfo
	}

	state := &runtimeState{
		level:       options.Level,
		addSource:   options.AddSource,
		replaceAttr: options.ReplaceAttr,
		enrichers:   append([]Enricher(nil), options.Enrichers...),
		outputs:     append([]Output(nil), outputs...),
	}
	state.encoders.New = func() any { return newRecordEncoder(state) }

	if options.MemoryMaxBytes > 0 {
		state.memory = newMemoryStore(options.MemoryMaxBytes)
	}

	runtime := &Runtime{state: state}
	runtime.root = &handler{state: state}
	return runtime
}

// Handler returns the shared EasyLog slog handler.
func (r *Runtime) Handler() slog.Handler {
	return r.root
}

// Logger returns a standard slog logger backed by this runtime.
func (r *Runtime) Logger() *slog.Logger {
	return slog.New(r.root)
}

// Consumer returns the memory consumer, or nil when retention is disabled.
func (r *Runtime) Consumer() MemoryConsumer {
	if r == nil || r.state == nil || r.state.memory == nil {
		return nil
	}
	return r.state.memory
}

// Sync synchronizes every output in order, attempting all even if one fails.
// It waits for active output I/O and returns ErrClosed after shutdown starts.
func (r *Runtime) Sync() error {
	r.state.outputMu.Lock()
	defer r.state.outputMu.Unlock()
	if r.state.closed.Load() {
		return ErrClosed
	}

	var failures []error
	for index, output := range r.state.outputs {
		if output == nil {
			continue
		}
		if err := output.Sync(); err != nil {
			failures = append(failures, fmt.Errorf("output %d sync: %w", index, err))
		}
	}
	return errors.Join(failures...)
}

// Close stops logging, waits for active output I/O, and closes all outputs in order without Sync.
// Concurrent or repeated calls return nil immediately; only the first reports close errors.
func (r *Runtime) Close() error {
	if !r.state.closed.CompareAndSwap(false, true) {
		return nil
	}
	r.state.outputMu.Lock()
	defer r.state.outputMu.Unlock()

	var failures []error
	for index, output := range r.state.outputs {
		if output == nil {
			continue
		}
		if err := output.Close(); err != nil {
			failures = append(failures, fmt.Errorf("output %d close: %w", index, err))
		}
	}
	return errors.Join(failures...)
}

func (s *runtimeState) write(jsonLine []byte) error {
	if s.memory != nil {
		s.memory.append(jsonLine)
	}
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	if s.closed.Load() {
		return ErrClosed
	}

	var failures []error
	for index, output := range s.outputs {
		if output == nil {
			continue
		}
		if err := output.WriteRecord(jsonLine); err != nil {
			failures = append(failures, fmt.Errorf("output %d write: %w", index, err))
		}
	}
	return errors.Join(failures...)
}
