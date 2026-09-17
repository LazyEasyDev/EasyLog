package easylog

import (
	"bytes"
	"context"
	"log/slog"
	"time"
)

const maxPooledEncoderBufferBytes = 64 * 1024

type recordEncoder struct {
	buffer           bytes.Buffer
	handler          *slog.JSONHandler
	metadataHandler  *slog.JSONHandler
	metadataToRemove int
}

func newRecordEncoder() *recordEncoder {
	encoder := &recordEncoder{}
	encoder.metadataHandler = slog.NewJSONHandler(&encoder.buffer, nil)
	encoder.handler = slog.NewJSONHandler(&encoder.buffer, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if encoder.metadataToRemove > 0 {
				encoder.metadataToRemove--
				return slog.Attr{}
			}
			return attr
		},
	})
	return encoder
}

func (state *runtimeState) encode(ctx context.Context, prepared slog.Record) ([]byte, error) {
	encoder := state.encoders.Get().(*recordEncoder)
	defer func() {
		if encoder.buffer.Cap() > maxPooledEncoderBufferBytes {
			encoder.buffer = bytes.Buffer{}
		} else {
			encoder.buffer.Reset()
		}
		state.encoders.Put(encoder)
	}()
	record, metadataOnly := metadataOnlyRecord(prepared)
	jsonHandler := encoder.metadataHandler
	if !metadataOnly {
		record = prepared
		record.Time = time.Time{}
		encoder.metadataToRemove = 2
		jsonHandler = encoder.handler
	}
	if err := jsonHandler.Handle(ctx, record); err != nil {
		return nil, err
	}
	encoded := encoder.buffer.Bytes()
	return bytes.Clone(encoded[:len(encoded)-1]), nil
}

func metadataOnlyRecord(prepared slog.Record) (slog.Record, bool) {
	count := 2
	if !prepared.Time.IsZero() {
		count++
	}
	if prepared.NumAttrs() != count {
		return slog.Record{}, false
	}
	expected := [3]slog.Attr{}
	position := 0
	if !prepared.Time.IsZero() {
		expected[position] = slog.Time(slog.TimeKey, prepared.Time.Round(0))
		position++
	}
	expected[position] = slog.String(slog.LevelKey, prepared.Level.String())
	expected[position+1] = slog.String(slog.MessageKey, prepared.Message)
	position = 0
	matched := true
	prepared.Attrs(func(attr slog.Attr) bool {
		want := expected[position]
		position++
		matched = attr.Key == want.Key && attr.Value.Kind() == want.Value.Kind()
		if matched {
			if attr.Value.Kind() == slog.KindTime {
				matched = attr.Value.Time() == want.Value.Time()
			} else {
				matched = attr.Value.String() == want.Value.String()
			}
		}
		return matched
	})
	if !matched {
		return slog.Record{}, false
	}
	return slog.NewRecord(prepared.Time, prepared.Level, prepared.Message, prepared.PC), true
}
