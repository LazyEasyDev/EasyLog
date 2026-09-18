package easylog

import (
	"context"
	"log/slog"
	"slices"
)

type handler struct {
	state       *runtimeState
	jsonHandler slog.Handler
	jsonWriter  *jsonWriter
	groups      []string
	bound       [][]slog.Attr
	ignoreAttrs bool
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return !h.state.closed.Load() && level >= h.state.level.Level()
}

func (h *handler) Handle(ctx context.Context, source slog.Record) error {
	if h.state.closed.Load() {
		return ErrClosed
	}
	if source.Level < h.state.level.Level() {
		return nil
	}

	source = source.Clone()
	for _, enrich := range h.state.enrichers {
		if enrich != nil {
			source.AddAttrs(enrich(ctx)...)
		}
	}

	record, callAttrs := h.prepareRecord(source)
	jsonLine, err := h.encodeJSON(ctx, record, callAttrs)
	if err != nil {
		return err
	}
	return h.state.write(record, jsonLine)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	if h.ignoreAttrs {
		return h
	}
	attrs = h.prepareAttrs(attrs, h.groups)
	if len(attrs) == 0 {
		return h
	}
	child := *h
	child.bound = make([][]slog.Attr, len(h.groups)+1)
	copy(child.bound, h.bound)
	depth := len(h.groups)
	child.bound[depth] = append(slices.Clone(child.bound[depth]), attrs...)
	child.jsonHandler = h.jsonHandler.WithAttrs(attrs)
	return &child
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	if h.ignoreAttrs {
		return h
	}
	child := *h
	if len(h.groups) == 0 && isReservedKey(name) {
		child.ignoreAttrs = true
		return &child
	}
	child.groups = append(slices.Clone(h.groups), validString(name))
	child.jsonHandler = h.jsonHandler.WithGroup(validString(name))
	return &child
}

func isReservedKey(key string) bool {
	switch key {
	case slog.TimeKey, slog.LevelKey, slog.MessageKey, slog.SourceKey:
		return true
	default:
		return false
	}
}
