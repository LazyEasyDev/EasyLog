package easylog

import (
	"context"
	"log/slog"
)

type handlerOperation struct {
	attrs []slog.Attr
	group string
}

type handler struct {
	state      *runtimeState
	operations []handlerOperation
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

	return h.state.write(ctx, h.prepare(source))
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	copied := append([]slog.Attr(nil), attrs...)
	operations := append([]handlerOperation(nil), h.operations...)
	operations = append(operations, handlerOperation{attrs: copied})
	return &handler{state: h.state, operations: operations}
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	operations := append([]handlerOperation(nil), h.operations...)
	operations = append(operations, handlerOperation{group: name})
	return &handler{state: h.state, operations: operations}
}

func isReservedKey(key string) bool {
	switch key {
	case slog.TimeKey, slog.LevelKey, slog.MessageKey, slog.SourceKey:
		return true
	default:
		return false
	}
}

func filterReservedAttrs(attrs []slog.Attr) []slog.Attr {
	filtered := make([]slog.Attr, len(attrs))
	for index, attr := range attrs {
		filtered[index] = filterReservedAttr(attr)
	}
	return filtered
}

func filterReservedAttr(attr slog.Attr) slog.Attr {
	if isReservedKey(attr.Key) {
		return slog.Attr{}
	}
	attr.Value = attr.Value.Resolve()
	if attr.Key == "" && attr.Value.Kind() == slog.KindGroup {
		attr.Value = slog.GroupValue(filterReservedAttrs(attr.Value.Group())...)
	}
	return attr
}
