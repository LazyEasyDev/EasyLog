package terminal

import (
	"bytes"
	"fmt"
	"log/slog"
	"strconv"
	"time"
	"unicode"
)

// TextFormatter renders a level and timestamp prefix, a message, and key=value fields.
// It reads prepared attributes directly without decoding JSON or modifying the record.
type TextFormatter struct {
	// ForceColors emits ANSI colors even when terminal detection or environment checks disable them.
	// The destination must already support ANSI escape sequences.
	ForceColors bool
	// DisableColors removes ANSI colors and takes precedence over ForceColors.
	DisableColors bool
	// DisableTimestamp hides both the terminal timestamp and the top-level time field.
	DisableTimestamp bool
	// TimestampFormat is a Go time layout. Empty shows elapsed seconds since output creation.
	TimestampFormat string
	// ShowLevel displays the top-level level before the timestamp; false hides it without filtering.
	ShowLevel bool
}

func (formatter TextFormatter) format(record slog.Record, elapsed time.Duration) ([]byte, error) {
	var buffer, levels, timestamps bytes.Buffer
	color, reset := "", ""
	if !formatter.DisableColors {
		color, reset = levelColor(record.Level), "\x1b[0m"
	}
	separator := ""
	if !formatter.DisableTimestamp && formatter.TimestampFormat == "" {
		fmt.Fprintf(&timestamps, "[%04d]", max(int64(elapsed/time.Second), 0))
	}
	var formatErr error
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == slog.TimeKey {
			if formatter.DisableTimestamp || formatter.TimestampFormat == "" {
				return true
			}
			var timestamp time.Time
			var valid bool
			switch attr.Value.Kind() {
			case slog.KindTime:
				timestamp, valid = attr.Value.Time(), true
			case slog.KindString:
				var err error
				timestamp, err = time.Parse(time.RFC3339Nano, attr.Value.String())
				valid = err == nil
			}
			if valid {
				formatted := strconv.Quote(timestamp.Format(formatter.TimestampFormat))
				if timestamps.Len() > 0 {
					timestamps.WriteByte(' ')
				}
				timestamps.WriteByte('[')
				timestamps.WriteString(formatted[1 : len(formatted)-1])
				timestamps.WriteByte(']')
				return true
			}
		}
		text, isString := textValue(attr.Value)
		switch attr.Key {
		case slog.LevelKey:
			if !formatter.ShowLevel {
				return true
			}
			if levels.Len() > 0 {
				levels.WriteByte(' ')
			}
			levels.WriteString(color)
			if isString {
				switch text {
				case "DEBUG":
					text = "DEBU"
				case "ERROR":
					text = "ERRO"
				}
				writeTextString(&levels, text)
			} else if formatErr = writeJSONValue(&levels, attr.Value); formatErr != nil {
				return false
			}
			levels.WriteString(reset)
			return true
		case slog.MessageKey:
			if isString {
				if text != "" {
					buffer.WriteString(separator)
					writeMessageString(&buffer, text)
					separator = " "
				}
				return true
			}
		}
		buffer.WriteString(separator)
		buffer.WriteString(color)
		writeTextString(&buffer, attr.Key)
		buffer.WriteString(reset)
		buffer.WriteByte('=')
		if isString {
			writeTextString(&buffer, text)
		} else if formatErr = writeJSONValue(&buffer, attr.Value); formatErr != nil {
			return false
		}
		separator = " "
		return true
	})
	if formatErr != nil {
		return nil, formatErr
	}
	var output bytes.Buffer
	output.Grow(levels.Len() + timestamps.Len() + buffer.Len() + 2)
	output.Write(levels.Bytes())
	output.Write(timestamps.Bytes())
	if output.Len() > 0 && buffer.Len() > 0 {
		output.WriteByte(' ')
	}
	output.Write(buffer.Bytes())
	output.WriteByte('\n')
	return output.Bytes(), nil
}

func writeMessageString(buffer *bytes.Buffer, value string) {
	for _, character := range value {
		if unicode.IsPrint(character) {
			buffer.WriteRune(character)
		} else {
			quoted := strconv.QuoteRune(character)
			buffer.WriteString(quoted[1 : len(quoted)-1])
		}
	}
}

func writeTextString(buffer *bytes.Buffer, value string) {
	quote := value == ""
	for _, character := range value {
		if character == '=' || character == '"' || character == '\\' || unicode.IsSpace(character) || !unicode.IsPrint(character) {
			quote = true
			break
		}
	}
	if quote {
		buffer.WriteString(strconv.Quote(value))
	} else {
		buffer.WriteString(value)
	}
}

func levelColor(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "\x1b[90m"
	case level < slog.LevelWarn:
		return "\x1b[36m"
	case level < slog.LevelError:
		return "\x1b[33m"
	default:
		return "\x1b[31m"
	}
}
