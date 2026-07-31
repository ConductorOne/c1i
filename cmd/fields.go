package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Field projection (`--fields`) trims emitted JSON objects to a caller-chosen
// set of keys, which slashes output size — the dominant cost for the AI agents
// this CLI targets. Selection is by dot-path (e.g. `id,user.email`) matched
// against the keys as they appear in the command's output, and nesting is
// preserved in the result. It applies to both NDJSON list rows and single
// pretty-printed objects, via the emitter and writeObject helpers below.

// parseFieldPaths splits a `--fields` value ("id,user.email") into dot-path
// segments ([["id"],["user","email"]]). Empty and whitespace-only entries are
// dropped, so a trailing comma or a bare "" yields no path.
func parseFieldPaths(spec string) [][]string {
	if strings.TrimSpace(spec) == "" {
		return nil
	}
	var paths [][]string
	for _, raw := range strings.Split(spec, ",") {
		field := strings.TrimSpace(raw)
		if field == "" {
			continue
		}
		var segs []string
		for _, seg := range strings.Split(field, ".") {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				segs = nil
				break
			}
			segs = append(segs, seg)
		}
		if len(segs) > 0 {
			paths = append(paths, segs)
		}
	}
	return paths
}

// fieldPaths returns the projection requested via --fields / C1I_FIELDS, or nil
// when none was set (meaning "emit everything").
func fieldPaths() [][]string {
	return parseFieldPaths(viper.GetString("fields"))
}

// projectValue returns a copy of v containing only paths, with nesting
// preserved. A path that is missing (or that traverses into a non-object) is
// omitted rather than emitted as null, so requesting a superset is safe. A JSON
// array is projected element-wise; a bare scalar has nothing to project and is
// returned unchanged. paths must be non-empty; callers skip the call when no
// projection was requested.
func projectValue(v any, paths [][]string) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	projected, ok := projectBytes(b, paths)
	if !ok {
		return v
	}
	return projected
}

// projectBytes decodes data and projects it to paths. It decodes numbers as
// json.Number (not float64) so large integer IDs and epoch values keep their
// exact value through the round-trip. Returns ok=false only when data isn't
// valid JSON.
func projectBytes(data []byte, paths [][]string) (any, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	// Require exactly one JSON value. If data holds trailing/concatenated values
	// (NDJSON, multiple responses), Decode would silently keep only the first;
	// report not-ok so the caller falls back to emitting the raw bytes.
	if dec.More() {
		return nil, false
	}
	return projectDecoded(v, paths), true
}

// projectDecoded returns v keeping only paths. Objects are trimmed to the
// requested keys (nesting preserved); arrays are projected element-wise; scalars
// pass through unchanged. The output uses the source's own key spelling, not the
// requested one — --fields selects keys, it never renames them.
func projectDecoded(v any, paths [][]string) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for _, p := range paths {
			if keys, val, ok := lookupPath(t, p); ok {
				setPath(out, keys, val)
			}
		}
		return out
	case []any:
		arr := make([]any, len(t))
		for i, el := range t {
			arr[i] = projectDecoded(el, paths)
		}
		return arr
	default:
		return v
	}
}

// lookupPath walks m following path, returning the value at the leaf and the
// sequence of *source* keys actually matched (which may differ in casing/style
// from path — see resolveKey). It only descends through nested objects; hitting
// a non-object before the leaf (or a missing key) reports ok=false.
func lookupPath(m map[string]any, path []string) ([]string, any, bool) {
	cur := m
	matched := make([]string, 0, len(path))
	for i, seg := range path {
		key, val, ok := resolveKey(cur, seg)
		if !ok {
			return nil, nil, false
		}
		matched = append(matched, key)
		if i == len(path)-1 {
			return matched, val, true
		}
		next, ok := val.(map[string]any)
		if !ok {
			return nil, nil, false
		}
		cur = next
	}
	return nil, nil, false
}

// resolveKey finds seg among m's keys, returning the matching source key.
// It tries an exact match first (unchanged behavior), then falls back to a
// case- and separator-insensitive match so a --fields value can use camelCase
// against snake_case output and vice versa — the CLI emits list rows in
// snake_case but single-object reads in camelCase, and an agent shouldn't have
// to know which. The fallback only runs when the exact lookup misses, so it can
// never change an already-matching projection.
func resolveKey(m map[string]any, seg string) (string, any, bool) {
	if val, ok := m[seg]; ok {
		return seg, val, true
	}
	want := normalizeKey(seg)
	for k, val := range m {
		if normalizeKey(k) == want {
			return k, val, true
		}
	}
	return "", nil, false
}

// normalizeKey lowercases s and strips '_'/'-' so "display_name", "displayName",
// and "DisplayName" all compare equal.
func normalizeKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '_' || r == '-' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// setPath writes val into out at path, creating intermediate objects as needed.
func setPath(out map[string]any, path []string, val any) {
	cur := out
	for i, seg := range path {
		if i == len(path)-1 {
			cur[seg] = val
			return
		}
		next, ok := cur[seg].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
}

// emitter writes NDJSON rows, applying --fields projection when set. List
// commands use it in place of a bare json.Encoder so field selection is
// consistent across every list surface.
type emitter struct {
	enc   *json.Encoder
	paths [][]string
}

// newEmitter builds an emitter writing to w, reading the projection from the
// global --fields flag.
func newEmitter(w io.Writer) *emitter {
	return &emitter{enc: json.NewEncoder(w), paths: fieldPaths()}
}

// Encode writes v as one NDJSON line, projected to --fields when set. The name
// and signature match json.Encoder.Encode so an emitter is a drop-in
// replacement for the bare encoder list commands used before.
func (e *emitter) Encode(v any) error {
	if len(e.paths) == 0 {
		return e.enc.Encode(v)
	}
	return e.enc.Encode(projectValue(v, e.paths))
}

// writeObject pretty-prints a single JSON response to stdout, applying --fields
// projection when set. Use it for read/get output. It falls back to raw
// pretty-printing when projection is off or the bytes aren't valid JSON.
func writeObject(cmd *cobra.Command, data []byte) error {
	if paths := fieldPaths(); len(paths) > 0 {
		if projected, ok := projectBytes(data, paths); ok {
			if b, err := json.MarshalIndent(projected, "", "  "); err == nil {
				_, _ = cmd.OutOrStdout().Write(append(b, '\n'))
				return nil
			}
		}
	}
	return writeRawObject(cmd, data)
}

// writeRawObject pretty-prints a single JSON response to stdout WITHOUT applying
// --fields. Use it for mutation confirmations (create/update/delete), whose
// status objects must never be trimmed away by a session-wide C1I_FIELDS — a
// projection that matched none of the confirmation's keys would silently emit
// "{}" and hide whether the change succeeded. Falls back to raw bytes if the
// data isn't valid JSON.
func writeRawObject(cmd *cobra.Command, data []byte) error {
	out := cmd.OutOrStdout()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		_, _ = out.Write(data)
		return nil
	}
	pretty.WriteByte('\n')
	_, _ = out.Write(pretty.Bytes())
	return nil
}
