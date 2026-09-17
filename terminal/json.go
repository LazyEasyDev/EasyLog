package terminal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type jsonField struct {
	key      string
	raw      json.RawMessage
	text     string
	isString bool
}

func readJSONFields(jsonContent []byte) ([]jsonField, error) {
	decoder := json.NewDecoder(bytes.NewReader(jsonContent))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, errors.New("terminal: expected a JSON object")
	}
	var fields []jsonField
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("terminal: expected a JSON object key")
		}
		field := jsonField{key: key}
		if err := decoder.Decode(&field.raw); err != nil {
			return nil, err
		}
		if len(field.raw) > 0 && field.raw[0] == '"' {
			if err := json.Unmarshal(field.raw, &field.text); err != nil {
				return nil, err
			}
			field.isString = true
		}
		fields = append(fields, field)
	}
	token, err = decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('}') {
		return nil, errors.New("terminal: expected the end of a JSON object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("terminal: unexpected trailing JSON data")
	}
	return fields, nil
}
