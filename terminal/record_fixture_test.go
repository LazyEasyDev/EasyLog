package terminal

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"
)

func recordFromJSONFixture(level slog.Level, data []byte) slog.Record {
	record := slog.NewRecord(time.Time{}, level, "", 0)
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		panic("record fixture must be a JSON object")
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			panic(err)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			panic(err)
		}
		var value slog.Value
		if raw[0] == '"' {
			var text string
			if err := json.Unmarshal(raw, &text); err != nil {
				panic(err)
			}
			value = slog.StringValue(text)
		} else {
			var compact bytes.Buffer
			if err := json.Compact(&compact, raw); err != nil {
				panic(err)
			}
			value = slog.AnyValue(json.RawMessage(compact.Bytes()))
		}
		record.AddAttrs(slog.Attr{Key: token.(string), Value: value})
	}
	if _, err := decoder.Token(); err != nil {
		panic(err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		panic("unexpected data after record fixture")
	}
	return record
}

func assertRecordMatchesJSONFixture(test *testing.T, record slog.Record, data string) {
	test.Helper()
	if !reflect.DeepEqual(record, recordFromJSONFixture(record.Level, []byte(data))) {
		test.Fatal("formatting changed the prepared record")
	}
}
