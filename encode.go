package easylog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"slices"
	"sync"
	"time"
	"unicode/utf8"
)

type jsonWriter struct {
	mu       sync.Mutex
	jsonLine []byte
}

func (w *jsonWriter) Write(jsonLine []byte) (int, error) {
	w.jsonLine = bytes.Clone(jsonLine)
	return len(jsonLine), nil
}

func newJSONHandler(writer *jsonWriter, addSource bool) *slog.JSONHandler {
	return slog.NewJSONHandler(writer, &slog.HandlerOptions{AddSource: addSource})
}

func normalizeJSONTimestamp(timestamp time.Time) time.Time {
	timestamp = timestamp.UTC()
	if timestamp.Nanosecond()%10 == 0 {
		timestamp = timestamp.Add(time.Nanosecond)
	}
	return timestamp
}

func (h *handler) encodeJSON(ctx context.Context, record slog.Record, callAttrs []slog.Attr) ([]byte, error) {
	if !record.Time.IsZero() {
		record.Time = normalizeJSONTimestamp(record.Time)
	}
	if h.state.addSource || len(h.bound) != 0 || len(h.groups) != 0 {
		record = slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
		record.AddAttrs(callAttrs...)
	}
	h.jsonWriter.mu.Lock()
	defer func() {
		h.jsonWriter.jsonLine = nil
		h.jsonWriter.mu.Unlock()
	}()
	if err := h.jsonHandler.Handle(ctx, record); err != nil {
		return nil, err
	}
	return h.jsonWriter.jsonLine, nil
}

func (h *handler) prepareRecord(source slog.Record) (slog.Record, []slog.Attr) {
	record := slog.NewRecord(source.Time, source.Level, validString(source.Message), source.PC)
	if h.state.addSource {
		if location := source.Source(); location != nil {
			if attrs := sourceAttrs(location); len(attrs) != 0 {
				record.AddAttrs(slog.Attr{Key: slog.SourceKey, Value: slog.GroupValue(attrs...)})
			}
		}
	}
	var direct []slog.Attr
	if !h.ignoreAttrs {
		direct = make([]slog.Attr, 0, source.NumAttrs())
		source.Attrs(func(attr slog.Attr) bool {
			direct = h.appendPreparedAttr(direct, attr, h.groups)
			return true
		})
	}
	callAttrs := direct
	for depth := len(h.groups); depth >= 0; depth-- {
		if depth < len(h.bound) && len(h.bound[depth]) != 0 {
			if len(direct) == 0 {
				direct = h.bound[depth]
			} else {
				combined := make([]slog.Attr, 0, len(h.bound[depth])+len(direct))
				combined = append(combined, h.bound[depth]...)
				direct = append(combined, direct...)
			}
		}
		if depth > 0 && len(direct) != 0 {
			direct = []slog.Attr{{Key: h.groups[depth-1], Value: slog.GroupValue(direct...)}}
		}
	}
	record.AddAttrs(direct...)
	return record, callAttrs
}

func (h *handler) prepareAttrs(attrs []slog.Attr, groups []string) []slog.Attr {
	prepared := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		prepared = h.appendPreparedAttr(prepared, attr, groups)
	}
	return prepared
}

func (h *handler) appendPreparedAttr(prepared []slog.Attr, attr slog.Attr, groups []string) []slog.Attr {
	if len(groups) == 0 && isReservedKey(attr.Key) {
		return prepared
	}
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() != slog.KindGroup && h.state.replaceAttr != nil {
		attr = h.state.replaceAttr(groups, attr)
		if len(groups) == 0 && isReservedKey(attr.Key) {
			return prepared
		}
		attr.Value = attr.Value.Resolve()
	}
	if attr.Equal(slog.Attr{}) {
		return prepared
	}
	attr.Key = validString(attr.Key)
	if attr.Value.Kind() == slog.KindAny {
		if location, ok := attr.Value.Any().(*slog.Source); ok && location != nil {
			attr.Value = slog.GroupValue(sourceAttrs(location)...)
		}
	}
	if attr.Value.Kind() == slog.KindGroup {
		path := groups
		if attr.Key != "" {
			path = append(slices.Clone(groups), attr.Key)
		}
		children := h.prepareAttrs(attr.Value.Group(), path)
		if len(children) == 0 {
			return prepared
		}
		if attr.Key == "" {
			return append(prepared, children...)
		}
		attr.Value = slog.GroupValue(children...)
	} else {
		attr.Value = freezeValue(attr.Value)
	}
	return append(prepared, attr)
}

func sourceAttrs(location *slog.Source) []slog.Attr {
	var attrs []slog.Attr
	if location.Function != "" {
		attrs = append(attrs, slog.String("function", validString(location.Function)))
	}
	if location.File != "" {
		attrs = append(attrs, slog.String("file", validString(location.File)))
	}
	if location.Line != 0 {
		attrs = append(attrs, slog.Int("line", location.Line))
	}
	return attrs
}

func validString(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return string([]rune(value))
}

func freezeValue(value slog.Value) (result slog.Value) {
	defer func() {
		if recovered := recover(); recovered != nil {
			underlying := reflect.ValueOf(value.Any())
			if underlying.Kind() == reflect.Pointer && underlying.IsNil() {
				result = slog.StringValue("<nil>")
			} else {
				result = slog.StringValue(validString(fmt.Sprintf("!PANIC: %v", recovered)))
			}
		}
	}()
	switch value.Kind() {
	case slog.KindString:
		return slog.StringValue(validString(value.String()))
	case slog.KindInt64, slog.KindUint64, slog.KindBool, slog.KindDuration:
		return value
	case slog.KindFloat64:
		if number := value.Float64(); !math.IsNaN(number) && !math.IsInf(number, 0) {
			return value
		}
	case slog.KindTime:
		if timestamp := value.Time(); timestamp.Year() >= 0 && timestamp.Year() <= 9999 {
			return slog.StringValue(normalizeJSONTimestamp(timestamp).Format(time.RFC3339Nano))
		}
	case slog.KindAny:
		if _, marshaler := value.Any().(json.Marshaler); !marshaler {
			if failure, ok := value.Any().(error); ok {
				return slog.StringValue(validString(failure.Error()))
			}
		}
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value.Any()); err != nil {
		return slog.StringValue(validString("!ERROR:" + err.Error()))
	}
	raw := buffer.Bytes()[:buffer.Len()-1]
	if len(raw) > 0 && raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return slog.StringValue(validString("!ERROR:" + err.Error()))
		}
		return slog.StringValue(text)
	}
	return slog.AnyValue(json.RawMessage(raw))
}
