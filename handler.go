package easylog

import (
	"bytes"
	"context"
	"log/slog"

	"github.com/LazyEasyDev/EasyLog/internal/core"
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

	data, err := h.encode(ctx, source)
	if err != nil {
		return err
	}
	record := core.NewRecord(source.Level, data)

	if h.state.memory != nil {
		h.state.memory.append(record)
	}

	return h.state.write(record)
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

func (h *handler) encode(ctx context.Context, record slog.Record) ([]byte, error) {
	var buffer bytes.Buffer
	replaceAttr := h.state.replaceAttr
	if replaceAttr != nil {
		replaceAttr = func(groups []string, attr slog.Attr) slog.Attr {
			replacement := h.state.replaceAttr(groups, attr)
			if len(groups) == 0 && !isReservedKey(attr.Key) {
				return filterReservedAttr(replacement)
			}
			return replacement
		}
	}
	var encoder slog.Handler = slog.NewJSONHandler(&buffer, &slog.HandlerOptions{
		AddSource:   h.state.addSource,
		ReplaceAttr: replaceAttr,
	})
	grouped := false
	ignoreAttrs := false
	for _, operation := range h.operations {
		if operation.group != "" {
			if !grouped && isReservedKey(operation.group) {
				ignoreAttrs = true
				break
			}
			encoder = encoder.WithGroup(operation.group)
			grouped = true
		} else if grouped {
			encoder = encoder.WithAttrs(operation.attrs)
		} else {
			encoder = encoder.WithAttrs(filterReservedAttrs(operation.attrs))
		}
	}
	if !grouped {
		filtered := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
		if !ignoreAttrs {
			record.Attrs(func(attr slog.Attr) bool {
				filtered.AddAttrs(filterReservedAttr(attr))
				return true
			})
		}
		record = filtered
	}
	if err := encoder.Handle(ctx, record); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
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
