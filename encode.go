package easylog

import (
	"bytes"
	"context"
	"log/slog"
)

const maxPooledEncoderBufferBytes = 64 * 1024

type recordEncoder struct {
	buffer  bytes.Buffer
	handler *slog.JSONHandler
}

func newRecordEncoder(state *runtimeState) *recordEncoder {
	encoder := &recordEncoder{}
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
	encoder.handler = slog.NewJSONHandler(&encoder.buffer, options)
	return encoder
}

func (h *handler) encode(ctx context.Context, source slog.Record) ([]byte, error) {
	encoder := h.state.encoders.Get().(*recordEncoder)
	defer func() {
		if encoder.buffer.Cap() > maxPooledEncoderBufferBytes {
			encoder.buffer = bytes.Buffer{}
		} else {
			encoder.buffer.Reset()
		}
		h.state.encoders.Put(encoder)
	}()
	var handler slog.Handler = encoder.handler
	grouped := false
	ignoreAttrs := false
	for _, operation := range h.operations {
		if operation.group != "" {
			if !grouped && isReservedKey(operation.group) {
				ignoreAttrs = true
				break
			}
			handler = handler.WithGroup(operation.group)
			grouped = true
		} else if grouped {
			handler = handler.WithAttrs(operation.attrs)
		} else {
			handler = handler.WithAttrs(filterReservedAttrs(operation.attrs))
		}
	}
	record := source
	if ignoreAttrs || (!grouped && source.NumAttrs() != 0) {
		record = slog.NewRecord(source.Time, source.Level, source.Message, source.PC)
		if !ignoreAttrs {
			source.Attrs(func(attr slog.Attr) bool {
				record.AddAttrs(filterReservedAttr(attr))
				return true
			})
		}
	}
	if err := handler.Handle(ctx, record); err != nil {
		return nil, err
	}
	return bytes.Clone(encoder.buffer.Bytes()), nil
}
