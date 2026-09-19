package terminal

import (
	"bytes"
	"strconv"
	"unicode"
)

// TextFormatter renders prepared records as a level, timestamp, message, and key=value fields.
type TextFormatter struct {
	// ForceColors emits ANSI even when detection or console preparation fails.
	// New still attempts to prepare Windows console files for ANSI output.
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
