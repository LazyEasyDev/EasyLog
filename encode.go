package easylog

import (
	"bytes"
	"context"
	"log/slog"
)

type jsonWriter struct {
	state *runtimeState
}

func (w jsonWriter) Write(jsonLine []byte) (int, error) {
	if err := w.state.write(bytes.Clone(jsonLine)); err != nil {
		return 0, err
	}
	return len(jsonLine), nil
}

func newJSONHandler(state *runtimeState) *slog.JSONHandler {
	options := &slog.HandlerOptions{AddSource: state.addSource}
	if state.replaceAttr != nil {
		options.ReplaceAttr = func(groups []string, attr slog.Attr) slog.Attr {
			originalKey := attr.Key
			attr = state.replaceAttr(groups, attr)
			if len(groups) == 0 && !isReservedKey(originalKey) {
				attr = filterReservedAttr(attr)
			}
			return attr
		}
	}
	return slog.NewJSONHandler(jsonWriter{state: state}, options)
}

func (h *handler) encode(ctx context.Context, source slog.Record) error {
	record := source
	if h.ignoreAttrs || (!h.grouped && source.NumAttrs() != 0) {
		record = slog.NewRecord(source.Time, source.Level, source.Message, source.PC)
		if !h.ignoreAttrs {
			source.Attrs(func(attr slog.Attr) bool {
				record.AddAttrs(filterReservedAttr(attr))
				return true
			})
		}
	}
	return h.jsonHandler.Handle(ctx, record)
}
