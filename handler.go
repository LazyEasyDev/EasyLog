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
	return level >= h.state.level.Level()
}

func (h *handler) Handle(ctx context.Context, source slog.Record) error {
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
	var encoder slog.Handler = slog.NewJSONHandler(&buffer, &slog.HandlerOptions{
		AddSource:   h.state.addSource,
		ReplaceAttr: h.state.replaceAttr,
	})
	for _, operation := range h.operations {
		if operation.group != "" {
			encoder = encoder.WithGroup(operation.group)
		} else {
			encoder = encoder.WithAttrs(operation.attrs)
		}
	}
	if err := encoder.Handle(ctx, record); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}
