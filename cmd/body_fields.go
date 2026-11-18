package cmd

import (
	"fmt"
	"sort"
)

// The server reads a field's camelCase and snake_case spellings as one field,
// so a guard on user JSON (a --*-file object, or a message nested in one)
// matches keys the way mask paths are matched (foldMaskName). An exact
// m["camelCase"] read lets the snake_case spelling skip the guard;
// TestBodyKeyReadsAreFolded fails on one. Never use these on a free-form map
// (labels, configFields, a --context-file): its keys are data.

// bodyField returns obj's value for field in any spelling and the key it was
// found under ("" when absent). Two keys naming the field are a usage error:
// which one the server would read is not the guard's to guess.
func bodyField(obj map[string]any, field string) (v any, key string, err error) {
	want := foldMaskName(field)
	var keys []string
	for k := range obj {
		if foldMaskName(k) == want {
			keys = append(keys, k)
		}
	}
	switch len(keys) {
	case 0:
		return nil, "", nil
	case 1:
		return obj[keys[0]], keys[0], nil
	}
	sort.Strings(keys)
	return nil, "", &usageError{fmt.Errorf("the JSON input sets both %q and %q, which name the same field; keep one", keys[0], keys[1])}
}

// bodyFieldAs is bodyField with a type assertion: ok is false when the field
// is absent or not a T, like v, ok := m[k].(T).
func bodyFieldAs[T any](obj map[string]any, field string) (v T, ok bool, err error) {
	raw, _, err := bodyField(obj, field)
	if err != nil {
		return v, false, err
	}
	v, ok = raw.(T)
	return v, ok, nil
}

// bodySetsField reports whether obj sets field in any spelling.
func bodySetsField(obj map[string]any, field string) bool {
	want := foldMaskName(field)
	for k := range obj {
		if foldMaskName(k) == want {
			return true
		}
	}
	return false
}

// setBodyField sets field to v under the spelling obj already uses, else as
// field, so a fill never sends a second spelling.
func setBodyField(obj map[string]any, field string, v any) error {
	_, key, err := bodyField(obj, field)
	if err != nil {
		return err
	}
	if key == "" {
		key = field
	}
	obj[key] = v
	return nil
}
