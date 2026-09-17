package terminal

import (
	"bytes"
	"fmt"
	"strconv"
	"time"
	"unicode"
)

// TextFormatter renders JSON fields as a level, timestamp, message, and key=value fields.
type TextFormatter struct {
	// ForceColors emits ANSI colors without terminal detection.
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

// Format renders one JSON object with zero elapsed time and no automatic colors.
func (formatter TextFormatter) Format(jsonContent []byte) ([]byte, error) {
	return formatter.format(jsonContent, 0)
}

func (formatter TextFormatter) format(jsonContent []byte, elapsed time.Duration) ([]byte, error) {
	fields, err := readJSONFields(jsonContent)
	if err != nil {
		return nil, err
	}
	var buffer, levels, timestamps bytes.Buffer
	colors := formatter.ForceColors && !formatter.DisableColors
	color, reset := "", ""
	if colors {
		for _, field := range fields {
			if field.key == "level" && field.isString {
				if recognized := levelColor(field.text); recognized != "" {
					color, reset = recognized, "\x1b[0m"
				}
			}
		}
	}
	separator := ""
	if !formatter.DisableTimestamp && formatter.TimestampFormat == "" {
		fmt.Fprintf(&timestamps, "[%04d]", max(int64(elapsed/time.Second), 0))
	}
	for _, field := range fields {
		if field.key == "time" {
			if formatter.DisableTimestamp || formatter.TimestampFormat == "" {
				continue
			}
			if field.isString {
				if timestamp, err := time.Parse(time.RFC3339Nano, field.text); err == nil {
					formatted := strconv.Quote(timestamp.Format(formatter.TimestampFormat))
					if timestamps.Len() > 0 {
						timestamps.WriteByte(' ')
					}
					timestamps.WriteByte('[')
					timestamps.WriteString(formatted[1 : len(formatted)-1])
					timestamps.WriteByte(']')
					continue
				}
			}
		}
		text := field.text
		switch field.key {
		case "level":
			if !formatter.ShowLevel {
				continue
			}
			if levels.Len() > 0 {
				levels.WriteByte(' ')
			}
			fieldColor := ""
			if colors && field.isString {
				fieldColor = levelColor(text)
			}
			levels.WriteString(fieldColor)
			if field.isString {
				switch text {
				case "DEBUG":
					text = "DEBU"
				case "ERROR":
					text = "ERRO"
				}
				writeTextString(&levels, text)
			} else {
				levels.Write(field.raw)
			}
			if fieldColor != "" {
				levels.WriteString("\x1b[0m")
			}
			continue
		case "msg":
			if field.isString {
				if text != "" {
					buffer.WriteString(separator)
					writeMessageString(&buffer, text)
					separator = " "
				}
				continue
			}
		}
		buffer.WriteString(separator)
		buffer.WriteString(color)
		writeTextString(&buffer, field.key)
		buffer.WriteString(reset)
		buffer.WriteByte('=')
		if field.isString {
			writeTextString(&buffer, text)
		} else {
			buffer.Write(field.raw)
		}
		separator = " "
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

func levelColor(level string) string {
	switch level {
	case "DEBUG", "DEBU":
		return "\x1b[90m"
	case "INFO":
		return "\x1b[36m"
	case "WARN", "WARNING":
		return "\x1b[33m"
	case "ERROR", "ERRO":
		return "\x1b[31m"
	default:
		return ""
	}
}
