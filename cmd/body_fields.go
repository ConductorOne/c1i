package cmd

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// The server reads a field as its camelCase JSON name or its snake_case
// proto name, exactly ("pageSize", "page_size"; "PageSize" is an unknown
// field), and 400s on both. A guard on user JSON (a --*-file object, or a
// message nested in one) reads keys through these helpers, never
// m["camelCase"], which lets the snake_case spelling skip it;
// TestBodyKeyReadsAreFolded fails on one. Never use them on a free-form map
// (labels, configFields, a --context-file): its keys are data.

// fieldSpellings returns the two spellings the server accepts for a field
// named in either: its camelCase and snake_case forms.
func fieldSpellings(name string) (camel, snake string) {
	if strings.Contains(name, "_") {
		parts := strings.Split(name, "_")
		for i := 1; i < len(parts); i++ {
			if parts[i] != "" {
				parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
			}
		}
		return strings.Join(parts, ""), name
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsUpper(r) {
			b.WriteByte('_')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return name, b.String()
}

// bodyKeys returns the keys of obj that spell field, sorted.
func bodyKeys(obj map[string]any, field string) []string {
	camel, snake := fieldSpellings(field)
	var keys []string
	for _, k := range []string{camel, snake} {
		if _, ok := obj[k]; ok && (len(keys) == 0 || keys[0] != k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// bodyField returns obj's value for field in either spelling and the key it
// was found under ("" when absent). Both spellings are a usage error: which
// one the server would read is not the guard's to guess.
func bodyField(obj map[string]any, field string) (v any, key string, err error) {
	keys := bodyKeys(obj, field)
	switch len(keys) {
	case 0:
		return nil, "", nil
	case 1:
		return obj[keys[0]], keys[0], nil
	}
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

// bodySetsField reports whether obj sets field in either spelling.
func bodySetsField(obj map[string]any, field string) bool {
	return len(bodyKeys(obj, field)) > 0
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

// isFieldSpelling reports whether key is one of field's two spellings.
func isFieldSpelling(key, field string) bool {
	camel, snake := fieldSpellings(field)
	return key == camel || key == snake
}

// refuseBothSpellings refuses obj when any of its keys is also present in its
// other spelling.
func refuseBothSpellings(obj map[string]any) error {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, _, err := bodyField(obj, k); err != nil {
			return err
		}
	}
	return nil
}
