package webbeta

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// encoding/json otherwise accepts duplicate object keys with last-write-wins.
// Reject ambiguity both in the tool envelope and nested tool arguments.
func uniqueJSONKeys(raw string) error {
	d := json.NewDecoder(strings.NewReader(raw))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("tool JSON nesting too deep")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'):
			keys := make(map[string]bool)
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || keys[name] {
					return errors.New("duplicate or invalid tool JSON key")
				}
				keys[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil {
				return err
			}
			if end != json.Delim('}') {
				return errors.New("invalid tool JSON object")
			}
		case json.Delim('['):
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil {
				return err
			}
			if end != json.Delim(']') {
				return errors.New("invalid tool JSON array")
			}
		case json.Delim('}'), json.Delim(']'):
			return errors.New("invalid tool JSON delimiter")
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing tool JSON payload")
	}
	return nil
}
