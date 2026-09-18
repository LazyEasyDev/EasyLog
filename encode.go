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
				attr, _ = filterReservedAttr(attr)
			}
			return attr
		}
	}
	return slog.NewJSONHandler(jsonWriter{state: state}, options)
}

func (h *handler) encode(ctx context.Context, source slog.Record) error {
	record := source
	if h.ignoreAttrs {
		record = slog.NewRecord(source.Time, source.Level, source.Message, source.PC)
	} else if !h.grouped && source.NumAttrs() != 0 {
		needsFiltering := false
		source.Attrs(func(attr slog.Attr) bool {
			needsFiltering = attr.Key == "" || isReservedKey(attr.Key) || attr.Value.Kind() == slog.KindLogValuer
			return !needsFiltering
		})
		if needsFiltering {
			record = slog.NewRecord(source.Time, source.Level, source.Message, source.PC)
			var buffer [16]slog.Attr
			filtered := buffer[:0]
			if count := source.NumAttrs(); count > cap(filtered) {
				filtered = make([]slog.Attr, 0, count)
			}
			source.Attrs(func(attr slog.Attr) bool {
				if attr, keep := filterReservedAttr(attr); keep {
					filtered = append(filtered, attr)
				}
				return true
			})
			record.AddAttrs(filtered...)
		}
	}
	return h.jsonHandler.Handle(ctx, record)
}
