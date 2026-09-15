package easylog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Enricher extracts attributes from a log call's context.
type Enricher func(context.Context) []slog.Attr

// DefaultMemoryMaxBytes is the recommended memory retention limit.
const DefaultMemoryMaxBytes int64 = 8 * 1024 * 1024

// Options configures an EasyLog runtime.
type Options struct {
	// Level defaults to INFO when nil. It must not contain a typed-nil Leveler.
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

	sequence atomic.Uint64
	memory   *memoryStore
	outputs  Outputs
	outputMu sync.Mutex
}

// Runtime coordinates the handler, outputs, and optional memory consumer.
type Runtime struct {
	state *runtimeState
	root  *handler
}

// New constructs a runtime around the configured outputs.
func New(options Options, outputs Outputs) *Runtime {
	if options.Level == nil {
		options.Level = slog.LevelInfo
	}

	state := &runtimeState{
		level:       options.Level,
		addSource:   options.AddSource,
		replaceAttr: options.ReplaceAttr,
		enrichers:   append([]Enricher(nil), options.Enrichers...),
		outputs:     outputs,
	}

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

func (s *runtimeState) write(record Record) error {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()

	var failures []error
	if s.outputs.Terminal != nil {
		if err := s.outputs.Terminal.WriteRecord(record); err != nil {
			failures = append(failures, fmt.Errorf("terminal output: %w", err))
		}
	}
	if s.outputs.File != nil {
		if err := s.outputs.File.WriteRecord(record); err != nil {
			failures = append(failures, fmt.Errorf("file output: %w", err))
		}
	}
	return errors.Join(failures...)
}
