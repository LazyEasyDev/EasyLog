package easylog

import (
	"context"
	"log/slog"
)

type handler struct {
	state       *runtimeState
	jsonHandler slog.Handler
	grouped     bool
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

	return h.encode(ctx, source)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	if h.ignoreAttrs {
		return h
	}
	if !h.grouped {
		attrs = filterReservedAttrs(attrs)
	}
	child := *h
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
	if !h.grouped && isReservedKey(name) {
		child.ignoreAttrs = true
		return &child
	}
	child.jsonHandler = h.jsonHandler.WithGroup(name)
	child.grouped = true
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

func filterReservedAttrs(attrs []slog.Attr) []slog.Attr {
	filtered := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		if attr, keep := filterReservedAttr(attr); keep {
			filtered = append(filtered, attr)
		}
	}
	return filtered
}

func filterReservedAttr(attr slog.Attr) (slog.Attr, bool) {
	if isReservedKey(attr.Key) {
		return slog.Attr{}, false
	}
	attr.Value = attr.Value.Resolve()
	if attr.Key == "" && attr.Value.Kind() == slog.KindGroup {
		attr.Value = slog.GroupValue(filterReservedAttrs(attr.Value.Group())...)
	}
	return attr, true
}
