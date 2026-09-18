package terminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// FormatRecord renders a complete, resolved record with zero elapsed time.
func (formatter TextFormatter) FormatRecord(record slog.Record) ([]byte, error) {
	var buffer bytes.Buffer
	if err := formatter.formatRecord(&buffer, record, 0); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (formatter TextFormatter) formatRecord(buffer *bytes.Buffer, record slog.Record, elapsed time.Duration) error {
	color, reset := "", ""
	level := record.Level.String()
	if formatter.ForceColors && !formatter.DisableColors {
		color = levelColor(level)
		if color != "" {
			reset = "\x1b[0m"
		}
	}
	if formatter.ShowLevel {
		buffer.WriteString(color)
		switch level {
		case "DEBUG":
			level = "DEBU"
		case "ERROR":
			level = "ERRO"
		}
		writeTextString(buffer, level)
		buffer.WriteString(reset)
	}
	if !formatter.DisableTimestamp {
		if formatter.TimestampFormat == "" {
			buffer.WriteByte('[')
			var scratch [20]byte
			digits := strconv.AppendInt(scratch[:0], max(int64(elapsed/time.Second), 0), 10)
			for padding := len(digits); padding < 4; padding++ {
				buffer.WriteByte('0')
			}
			buffer.Write(digits)
			buffer.WriteByte(']')
		} else if !record.Time.IsZero() {
			formatted := strconv.Quote(record.Time.Format(formatter.TimestampFormat))
			buffer.WriteByte('[')
			buffer.WriteString(formatted[1 : len(formatted)-1])
			buffer.WriteByte(']')
		}
	}
	if record.Message != "" {
		if buffer.Len() > 0 {
			buffer.WriteByte(' ')
		}
		writeMessageString(buffer, record.Message)
	}
	var failure error
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Equal(slog.Attr{}) {
			return true
		}
		if buffer.Len() > 0 {
			buffer.WriteByte(' ')
		}
		buffer.WriteString(color)
		writeTextString(buffer, attr.Key)
		buffer.WriteString(reset)
		buffer.WriteByte('=')
		if attr.Value.Kind() == slog.KindString {
			writeTextString(buffer, attr.Value.String())
		} else {
			failure = writeRecordJSON(buffer, attr.Value)
		}
		return failure == nil
	})
	if failure != nil {
		return failure
	}
	buffer.WriteByte('\n')
	return nil
}

func writeRecordJSON(buffer *bytes.Buffer, value slog.Value) error {
	var scratch [32]byte
	switch value.Kind() {
	case slog.KindInt64:
		buffer.Write(strconv.AppendInt(scratch[:0], value.Int64(), 10))
	case slog.KindUint64:
		buffer.Write(strconv.AppendUint(scratch[:0], value.Uint64(), 10))
	case slog.KindDuration:
		buffer.Write(strconv.AppendInt(scratch[:0], int64(value.Duration()), 10))
	case slog.KindBool:
		buffer.Write(strconv.AppendBool(scratch[:0], value.Bool()))
	case slog.KindGroup:
		buffer.WriteByte('{')
		for index, attr := range value.Group() {
			if index != 0 {
				buffer.WriteByte(',')
			}
			if err := writeJSONValue(buffer, attr.Key); err != nil {
				return err
			}
			buffer.WriteByte(':')
			if err := writeRecordJSON(buffer, attr.Value); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	case slog.KindLogValuer:
		return fmt.Errorf("terminal: record contains unresolved LogValuer")
	case slog.KindAny:
		if raw, ok := value.Any().(json.RawMessage); ok {
			buffer.Write(raw)
			return nil
		}
		return writeJSONValue(buffer, value.Any())
	default:
		return writeJSONValue(buffer, value.Any())
	}
	return nil
}

func writeJSONValue(buffer *bytes.Buffer, value any) error {
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	buffer.Truncate(buffer.Len() - 1)
	return nil
}
