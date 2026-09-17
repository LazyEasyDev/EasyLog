package easylog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"slices"
	"unicode/utf8"
)

func (h *handler) prepare(source slog.Record) slog.Record {
	var fields []slog.Attr
	var parents [][]slog.Attr
	var groups []string
	ignoreAttrs := false
	for _, operation := range h.operations {
		if operation.group != "" {
			if len(groups) == 0 && isReservedKey(operation.group) {
				ignoreAttrs = true
				break
			}
			parents = append(parents, fields)
			groups = append(groups, operation.group)
			fields = nil
			continue
		}
		attrs := operation.attrs
		if len(groups) == 0 {
			attrs = filterReservedAttrs(attrs)
		}
		fields = h.prepareAttrs(fields, groups, attrs)
	}

	var callAttrs []slog.Attr
	if !ignoreAttrs {
		callAttrs = make([]slog.Attr, 0, source.NumAttrs())
		source.Attrs(func(attr slog.Attr) bool {
			if len(groups) == 0 {
				attr = filterReservedAttr(attr)
			}
			callAttrs = append(callAttrs, attr)
			return true
		})
	}
	prepared := h.prepareMetadata(source)
	fields = h.prepareAttrs(fields, groups, callAttrs)
	for index := len(groups) - 1; index >= 0; index-- {
		parent := parents[index]
		if len(fields) != 0 {
			parent = append(parent, slog.Attr{Key: groups[index], Value: slog.GroupValue(fields...)})
		}
		fields = parent
	}
	prepared.AddAttrs(fields...)
	return prepared
}

func (h *handler) prepareMetadata(source slog.Record) slog.Record {
	prepared := slog.NewRecord(source.Time, source.Level, source.Message, source.PC)
	if h.state.replaceAttr == nil && !h.state.addSource {
		if !source.Time.IsZero() {
			prepared.AddAttrs(slog.Time(slog.TimeKey, source.Time.Round(0)))
		}
		prepared.AddAttrs(
			slog.String(slog.LevelKey, source.Level.String()),
			slog.Attr{Key: slog.MessageKey, Value: snapshotValue(slog.StringValue(source.Message))},
		)
		return prepared
	}
	addMetadata := func(attr slog.Attr) {
		attr = h.prepareAttr(nil, attr)
		if attr.Equal(slog.Attr{}) {
			return
		}
		if attr.Key == "" && attr.Value.Kind() == slog.KindGroup {
			prepared.AddAttrs(attr.Value.Group()...)
		} else {
			prepared.AddAttrs(attr)
		}
	}
	if !source.Time.IsZero() {
		addMetadata(slog.Time(slog.TimeKey, source.Time.Round(0)))
	}
	addMetadata(slog.Any(slog.LevelKey, source.Level))
	if h.state.addSource {
		location := source.Source()
		if location == nil {
			location = &slog.Source{}
		}
		addMetadata(slog.Any(slog.SourceKey, location))
	}
	addMetadata(slog.String(slog.MessageKey, source.Message))
	return prepared
}

func (h *handler) prepareAttrs(destination []slog.Attr, groups []string, attrs []slog.Attr) []slog.Attr {
	destination = slices.Grow(destination, len(attrs))
	for _, attr := range attrs {
		attr = h.prepareAttr(groups, attr)
		if attr.Equal(slog.Attr{}) {
			continue
		}
		if attr.Key == "" && attr.Value.Kind() == slog.KindGroup {
			destination = append(destination, attr.Value.Group()...)
		} else {
			destination = append(destination, attr)
		}
	}
	return destination
}

func (h *handler) prepareAttr(groups []string, attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if h.state.replaceAttr != nil && attr.Value.Kind() != slog.KindGroup {
		originalKey := attr.Key
		attr = h.state.replaceAttr(groups, attr)
		if len(groups) == 0 && !isReservedKey(originalKey) {
			attr = filterReservedAttr(attr)
		}
		attr.Value = attr.Value.Resolve()
	}
	if attr.Equal(slog.Attr{}) {
		return slog.Attr{}
	}
	if attr.Value.Kind() == slog.KindAny {
		if location, ok := attr.Value.Any().(*slog.Source); ok {
			var attrs []slog.Attr
			if location != nil {
				if location.Function != "" {
					attrs = append(attrs, slog.String("function", location.Function))
				}
				if location.File != "" {
					attrs = append(attrs, slog.String("file", location.File))
				}
				if location.Line != 0 {
					attrs = append(attrs, slog.Int("line", location.Line))
				}
			}
			attr.Value = slog.GroupValue(attrs...)
		}
	}
	if attr.Value.Kind() == slog.KindGroup {
		if attr.Key != "" {
			groups = append(groups, attr.Key)
		}
		attrs := h.prepareAttrs(nil, groups, attr.Value.Group())
		if len(attrs) == 0 {
			return slog.Attr{}
		}
		attr.Value = slog.GroupValue(attrs...)
	} else {
		attr.Value = snapshotValue(attr.Value)
	}
	if !utf8.ValidString(attr.Key) {
		attr.Key = string([]rune(attr.Key))
	}
	return attr
}

func snapshotValue(value slog.Value) slog.Value {
	switch value.Kind() {
	case slog.KindString:
		if text := value.String(); !utf8.ValidString(text) {
			return slog.StringValue(string([]rune(text)))
		}
	case slog.KindFloat64:
		if number := value.Float64(); math.IsNaN(number) || math.IsInf(number, 0) {
			_, err := json.Marshal(number)
			return slog.StringValue(fmt.Sprintf("!ERROR:%v", err))
		}
	case slog.KindAny:
		if level, ok := value.Any().(slog.Level); ok {
			return slog.StringValue(level.String())
		}
		return snapshotAny(value.Any())
	}
	return value
}

func snapshotAny(original any) (snapshot slog.Value) {
	if original == nil {
		return slog.AnyValue(nil)
	}
	defer func() {
		if failure := recover(); failure != nil {
			reflected := reflect.ValueOf(original)
			if reflected.Kind() == reflect.Pointer && reflected.IsNil() {
				snapshot = slog.StringValue("<nil>")
			} else {
				snapshot = slog.StringValue(fmt.Sprintf("!PANIC: %v", failure))
			}
		}
	}()
	if _, marshalsJSON := original.(json.Marshaler); !marshalsJSON {
		if failure, ok := original.(error); ok {
			return snapshotValue(slog.StringValue(failure.Error()))
		}
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(original); err != nil {
		return slog.StringValue(fmt.Sprintf("!ERROR:%v", err))
	}
	data := buffer.Bytes()
	data = data[:len(data)-1]
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return slog.StringValue(fmt.Sprintf("!ERROR:%v", err))
		}
		return slog.StringValue(text)
	}
	return slog.AnyValue(json.RawMessage(data))
}
