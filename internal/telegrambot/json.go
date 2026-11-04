package telegrambot

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// decodeJSON rejects ambiguous JSON throughout the tree, including ignored
// values. JSON numbers are never rounded through float64.
func decodeJSON(data []byte, target any) error {
	if len(data) == 0 || len(data) > maxResponseBytes || !utf8.Valid(data) {
		return ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 32 {
			return ErrProtocol
		}
		token, err := d.Token()
		if err != nil {
			return ErrProtocol
		}
		delim, complex := token.(json.Delim)
		if !complex {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				token, err := d.Token()
				key, ok := token.(string)
				if err != nil || !ok || seen[key] {
					return ErrProtocol
				}
				seen[key] = true
				if visit(depth+1) != nil {
					return ErrProtocol
				}
			}
		case '[':
			for d.More() {
				if visit(depth+1) != nil {
					return ErrProtocol
				}
			}
		default:
			return ErrProtocol
		}
		if _, err := d.Token(); err != nil {
			return ErrProtocol
		}
		return nil
	}
	if visit(0) != nil {
		return ErrProtocol
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrProtocol
	}
	if json.Unmarshal(data, target) != nil {
		return ErrProtocol
	}
	return nil
}

// Server-owned action payloads have a closed shape, unlike forward-compatible
// Telegram API updates. Retain the duplicate-key/depth checks before rejecting
// fields that are not part of the reviewed action.
func decodeStrictJSON(data []byte, target any) error {
	if err := decodeJSON(data, target); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return ErrProtocol
	}
	return nil
}

func required(fields map[string]json.RawMessage, key string, out any) error {
	raw, ok := fields[key]
	if !ok || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, out) != nil {
		return ErrProtocol
	}
	return nil
}
