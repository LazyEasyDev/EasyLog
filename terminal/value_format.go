package terminal

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"
)

func textValue(value slog.Value) (string, bool) {
	switch value.Kind() {
	case slog.KindString:
		return value.String(), true
	case slog.KindTime:
		return value.Time().Format(time.RFC3339Nano), true
	default:
		return "", false
	}
}

func writeJSONValue(buffer *bytes.Buffer, value slog.Value) error {
	switch value.Kind() {
	case slog.KindString:
		return writeJSONEncoded(buffer, value.String())
	case slog.KindInt64:
		buffer.Write(strconv.AppendInt(buffer.AvailableBuffer(), value.Int64(), 10))
	case slog.KindUint64:
		buffer.Write(strconv.AppendUint(buffer.AvailableBuffer(), value.Uint64(), 10))
	case slog.KindFloat64:
		return writeJSONEncoded(buffer, value.Float64())
	case slog.KindBool:
		buffer.Write(strconv.AppendBool(buffer.AvailableBuffer(), value.Bool()))
	case slog.KindDuration:
		buffer.Write(strconv.AppendInt(buffer.AvailableBuffer(), int64(value.Duration()), 10))
	case slog.KindTime:
		return writeJSONEncoded(buffer, value.Time())
	case slog.KindGroup:
		buffer.WriteByte('{')
		for index, attr := range value.Group() {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := writeJSONEncoded(buffer, attr.Key); err != nil {
				return err
			}
			buffer.WriteByte(':')
			if err := writeJSONValue(buffer, attr.Value); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	case slog.KindAny:
		if data, ok := value.Any().(json.RawMessage); ok {
			buffer.Write(data)
		} else {
			return writeJSONEncoded(buffer, value.Any())
		}
	default:
		return errors.New("terminal: attribute value was not prepared")
	}
	return nil
}

func writeJSONEncoded(buffer *bytes.Buffer, value any) error {
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	buffer.Truncate(buffer.Len() - 1)
	return nil
}
