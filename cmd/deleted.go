package cmd

import (
	"bytes"
	"encoding/json"
)

// isDeletedAt is the one "is this object deleted?" rule: a deletedAt that is
// present and not null. The server sends null for a live object; "" counts as
// live too.
func isDeletedAt(v json.RawMessage) bool {
	s := bytes.TrimSpace(v)
	return len(s) > 0 && string(s) != "null" && string(s) != `""`
}

// deletedAtPath reads the deletedAt of the object at path in a response, not
// a deletedAt found elsewhere in it. ok is false when the response is not
// JSON or a path segment is missing, null or not an object; at is the value
// for an error message.
func deletedAtPath(data []byte, path ...string) (deleted bool, at string, ok bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil || obj == nil {
		return false, "", false
	}
	for _, seg := range path {
		next, found := obj[seg]
		if !found {
			return false, "", false
		}
		obj = nil
		if json.Unmarshal(next, &obj) != nil || obj == nil {
			return false, "", false
		}
	}
	raw := obj["deletedAt"]
	if !isDeletedAt(raw) {
		return false, "", true
	}
	if json.Unmarshal(raw, &at) != nil {
		at = string(bytes.TrimSpace(raw))
	}
	return true, at, true
}
