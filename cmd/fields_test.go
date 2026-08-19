package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestFieldPathsAndEmitterReadViper(t *testing.T) {
	viper.Set("fields", "id,user.email")
	t.Cleanup(func() { viper.Set("fields", "") })

	if got, want := fieldPaths(), [][]string{{"id"}, {"user", "email"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fieldPaths() = %v, want %v", got, want)
	}

	var buf bytes.Buffer
	if err := newEmitter(&buf).Encode(map[string]any{
		"id": "1", "user": map[string]any{"email": "x@y.z", "name": "n"}, "drop": "gone",
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := string(bytes.TrimSpace(buf.Bytes()))
	want := `{"id":"1","user":{"email":"x@y.z"}}`
	if got != want {
		t.Errorf("newEmitter honoring --fields = %s, want %s", got, want)
	}
}

func TestParseFieldPaths(t *testing.T) {
	cases := map[string][][]string{
		"":              nil,
		"   ":           nil,
		",":             nil,
		"id":            {{"id"}},
		"id,email":      {{"id"}, {"email"}},
		" id , email ":  {{"id"}, {"email"}},
		"user.email":    {{"user", "email"}},
		"id,user.email": {{"id"}, {"user", "email"}},
		"a..b":          nil, // malformed dot-path is dropped
		"id,,email":     {{"id"}, {"email"}},
		"a.b.c,d":       {{"a", "b", "c"}, {"d"}},
	}
	for spec, want := range cases {
		if got := parseFieldPaths(spec); !reflect.DeepEqual(got, want) {
			t.Errorf("parseFieldPaths(%q) = %v, want %v", spec, got, want)
		}
	}
}

// projectJSON is a test helper: project a JSON literal and return the result as
// compact JSON for easy comparison.
func projectJSON(t *testing.T, in string, spec string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(in), &v); err != nil {
		t.Fatalf("bad input JSON: %v", err)
	}
	out := projectValue(v, parseFieldPaths(spec))
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal projected: %v", err)
	}
	return string(b)
}

func TestProjectValue(t *testing.T) {
	cases := []struct {
		name, in, spec, want string
	}{
		{"top-level subset", `{"id":"1","email":"a@b.c","dept":"x"}`, "id,email", `{"email":"a@b.c","id":"1"}`},
		{"missing field omitted", `{"id":"1"}`, "id,email", `{"id":"1"}`},
		{"nested selection preserves nesting", `{"user":{"email":"a@b.c","name":"n"},"id":"1"}`, "user.email", `{"user":{"email":"a@b.c"}}`},
		{"nested plus top-level", `{"user":{"email":"a@b.c"},"id":"1","x":9}`, "id,user.email", `{"id":"1","user":{"email":"a@b.c"}}`},
		{"path into non-object omitted", `{"id":"1"}`, "id.sub", `{}`},
		{"all missing yields empty object", `{"id":"1"}`, "nope,gone", `{}`},
		{"value preserved with type", `{"n":5,"b":true,"id":"1"}`, "n,b", `{"b":true,"n":5}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := projectJSON(t, tc.in, tc.spec); got != tc.want {
				t.Errorf("project(%s, %q) = %s, want %s", tc.in, tc.spec, got, tc.want)
			}
		})
	}
}

// TestProjectValueCaseInsensitiveFallback covers the snake_case/camelCase
// bridge: a --fields value in one style resolves keys emitted in the other
// (the CLI emits list rows in snake_case but single-object reads in camelCase).
// The output keeps the SOURCE key spelling, and an exact match always wins over
// the fallback.
func TestProjectValueCaseInsensitiveFallback(t *testing.T) {
	cases := []struct {
		name, in, spec, want string
	}{
		{"camel spec matches snake source", `{"display_name":"n","id":"1"}`, "displayName", `{"display_name":"n"}`},
		{"snake spec matches camel source", `{"displayName":"n","id":"1"}`, "display_name", `{"displayName":"n"}`},
		{"nested cross-style", `{"app_user":{"userEmail":"a@b.c"}}`, "appUser.user_email", `{"app_user":{"userEmail":"a@b.c"}}`},
		{"exact match wins over fallback", `{"displayName":"camel","display_name":"snake"}`, "displayName", `{"displayName":"camel"}`},
		{"still missing stays missing", `{"id":"1"}`, "totally_absent", `{}`},
		// Ambiguous fallback (both variants present, spec matches neither
		// exactly): the lexicographically smallest key wins, deterministically —
		// "displayName" < "display_name" ('N' 0x4E < '_' 0x5F).
		{"ambiguous fallback is deterministic", `{"displayName":"camel","display_name":"snake"}`, "display-name", `{"displayName":"camel"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := projectJSON(t, tc.in, tc.spec); got != tc.want {
				t.Errorf("project(%s, %q) = %s, want %s", tc.in, tc.spec, got, tc.want)
			}
		})
	}
}

func TestProjectValueNonObjectUnchanged(t *testing.T) {
	// A bare scalar can't be projected; it should pass through untouched.
	if got := projectJSON(t, `"hello"`, "id"); got != `"hello"` {
		t.Errorf("scalar projection = %s, want unchanged", got)
	}
}

func TestProjectBytesPreservesLargeNumbers(t *testing.T) {
	// Regression: round-tripping through float64 would corrupt integers > 2^53.
	projected, ok := projectBytes([]byte(`{"id":1234567890123456789,"big":1e21,"drop":1}`), parseFieldPaths("id,big"))
	if !ok {
		t.Fatal("projectBytes returned ok=false on valid JSON")
	}
	b, _ := json.Marshal(projected)
	if want := `{"big":1e21,"id":1234567890123456789}`; string(b) != want {
		t.Errorf("large-number projection = %s, want %s", b, want)
	}
}

func TestProjectBytesRejectsTrailingData(t *testing.T) {
	// Concatenated/NDJSON input must not be silently truncated to its first
	// value; projectBytes reports ok=false so the caller emits raw.
	if _, ok := projectBytes([]byte(`{"id":"1"}{"id":"2"}`), parseFieldPaths("id")); ok {
		t.Error("expected ok=false for multi-value input")
	}
	// A single value with trailing whitespace is still fine.
	if _, ok := projectBytes([]byte("{\"id\":\"1\"}\n  "), parseFieldPaths("id")); !ok {
		t.Error("expected ok=true for a single value with trailing whitespace")
	}
}

func TestProjectBytesArrayElementwise(t *testing.T) {
	// A top-level array is projected element by element.
	projected, ok := projectBytes([]byte(`[{"id":"1","x":9},{"id":"2","x":8}]`), parseFieldPaths("id"))
	if !ok {
		t.Fatal("projectBytes ok=false")
	}
	b, _ := json.Marshal(projected)
	if want := `[{"id":"1"},{"id":"2"}]`; string(b) != want {
		t.Errorf("array projection = %s, want %s", b, want)
	}
}

func TestEmitterProjects(t *testing.T) {
	var buf bytes.Buffer
	e := &emitter{enc: json.NewEncoder(&buf), paths: parseFieldPaths("id,user.email")}
	row := map[string]any{"id": "1", "user": map[string]any{"email": "a@b.c", "name": "n"}, "extra": "drop"}
	if err := e.Encode(row); err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := bytes.TrimSpace(buf.Bytes())
	want := `{"id":"1","user":{"email":"a@b.c"}}`
	if string(got) != want {
		t.Errorf("emitter output = %s, want %s", got, want)
	}
}

func TestEmitterRawMessagePreservesNumbers(t *testing.T) {
	big := `1234567890123456789`
	// This is the shape the api --paginate loop feeds the emitter: a raw
	// json.RawMessage list item. Numbers must survive both with and without
	// projection (no float64 round-trip).
	for _, paths := range [][][]string{nil, parseFieldPaths("id")} {
		var buf bytes.Buffer
		e := &emitter{enc: json.NewEncoder(&buf), paths: paths}
		if err := e.Encode(json.RawMessage(`{"id":` + big + `,"x":1}`)); err != nil {
			t.Fatalf("encode: %v", err)
		}
		if !bytes.Contains(buf.Bytes(), []byte(big)) {
			t.Errorf("paths=%v: large integer corrupted: %s", paths, buf.Bytes())
		}
	}
}

func TestEmitterNoProjectionEmitsAll(t *testing.T) {
	var buf bytes.Buffer
	e := &emitter{enc: json.NewEncoder(&buf), paths: nil}
	row := map[string]any{"id": "1", "extra": "keep"}
	if err := e.Encode(row); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"extra":"keep"`)) {
		t.Errorf("no-projection output dropped a field: %s", buf.Bytes())
	}
}

func TestWriteObjectProjects(t *testing.T) {
	t.Setenv("C1I_FIELDS", "")
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	// No projection: full pretty JSON.
	if err := writeObject(cmd, []byte(`{"id":"1","name":"n"}`)); err != nil {
		t.Fatalf("writeObject: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"name": "n"`)) {
		t.Errorf("expected pretty-printed full object, got %s", buf.Bytes())
	}

	// Invalid JSON falls back to raw passthrough.
	buf.Reset()
	if err := writeObject(cmd, []byte(`not json`)); err != nil {
		t.Fatalf("writeObject raw: %v", err)
	}
	if got := buf.String(); got != "not json" {
		t.Errorf("raw passthrough = %q, want %q", got, "not json")
	}

	// With --fields set, writeObject projects the single object.
	viper.Set("fields", "id")
	t.Cleanup(func() { viper.Set("fields", "") })
	buf.Reset()
	if err := writeObject(cmd, []byte(`{"id":"1","name":"n"}`)); err != nil {
		t.Fatalf("writeObject project: %v", err)
	}
	if got := buf.String(); !bytes.Contains(buf.Bytes(), []byte(`"id": "1"`)) || bytes.Contains(buf.Bytes(), []byte(`"name"`)) {
		t.Errorf("writeObject with --fields = %q, want only id", got)
	}
}

// TestProjectValueDepthInsensitive covers the wrapper-key bug: single-object
// reads pass the API response through as-is, wrapped under the endpoint's own
// key (userView.user, function, app, ...), so an unqualified --fields id used
// to match nothing and silently project to {}. A path that doesn't resolve
// from the root now falls back to searching nested objects.
func TestProjectValueDepthInsensitive(t *testing.T) {
	cases := []struct {
		name, in, spec, want string
	}{
		{
			"unqualified name finds doubly-nested wrapper",
			`{"userView":{"user":{"id":"1","displayName":"n"},"other":"x"}}`,
			"id,displayName",
			`{"userView":{"user":{"displayName":"n","id":"1"}}}`,
		},
		{
			"unqualified name finds singly-nested wrapper",
			`{"function":{"id":"1","publishedCommitId":"c1"}}`,
			"id",
			`{"function":{"id":"1"}}`,
		},
		{
			"shorter dot-path than the real nesting still resolves",
			`{"userView":{"user":{"id":"1"}}}`,
			"user.id",
			`{"userView":{"user":{"id":"1"}}}`,
		},
		{
			"exact root match still wins over a deeper one",
			`{"id":"top","wrap":{"id":"nested"}}`,
			"id",
			`{"id":"top"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := projectJSON(t, tc.in, tc.spec); got != tc.want {
				t.Errorf("project(%s, %q) = %s, want %s", tc.in, tc.spec, got, tc.want)
			}
		})
	}
}

// TestProjectValueDepthInsensitiveAmbiguityIsDeterministic covers the case
// where the same leaf name exists under two different sibling wrappers at the
// same depth: the lexicographically smallest full dotted path wins,
// deterministically, mirroring how resolveKey breaks a casing tie.
func TestProjectValueDepthInsensitiveAmbiguityIsDeterministic(t *testing.T) {
	in := `{"b":{"id":"from-b"},"a":{"id":"from-a"}}`
	want := `{"a":{"id":"from-a"}}`
	for i := 0; i < 20; i++ { // map iteration order is random; run repeatedly
		if got := projectJSON(t, in, "id"); got != want {
			t.Fatalf("project(%s, %q) = %s, want %s (run %d)", in, "id", got, want, i)
		}
	}
}

// TestProjectValueDepthInsensitiveDoesNotDescendIntoArrays documents the
// scoping decision: only nested objects are searched, not array elements.
func TestProjectValueDepthInsensitiveDoesNotDescendIntoArrays(t *testing.T) {
	in := `{"items":[{"id":"1"}]}`
	if got, want := projectJSON(t, in, "id"), `{}`; got != want {
		t.Errorf("project(%s, %q) = %s, want %s (arrays should not be searched)", in, "id", got, want)
	}
}

// TestWriteObjectFailsLoudlyOnNoMatch is the backstop half of the fix: a
// --fields spec that matches nothing anywhere in the response (a genuine typo,
// or a field that truly doesn't exist) must not silently print "{}" with exit
// 0 — it must return a *usageError (exit 2) instead.
func TestWriteObjectFailsLoudlyOnNoMatch(t *testing.T) {
	viper.Set("fields", "totally_bogus_field")
	t.Cleanup(func() { viper.Set("fields", "") })

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := writeObject(cmd, []byte(`{"userView":{"user":{"id":"1","displayName":"n"}}}`))
	if err == nil {
		t.Fatalf("writeObject with a fully-unmatched --fields returned nil error; output: %s", buf.String())
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("writeObject error = %v (%T), want a *usageError (exit code 2)", err, err)
	}
	if buf.Len() != 0 {
		t.Errorf("writeObject wrote output %q despite returning an error; must not print anything on a no-match", buf.String())
	}
}

// TestWriteObjectPartialMatchStillSucceeds ensures the no-match backstop only
// fires when every requested field misses; a spec with one real field and one
// typo still succeeds with just the real field.
func TestWriteObjectPartialMatchStillSucceeds(t *testing.T) {
	viper.Set("fields", "id,totally_bogus_field")
	t.Cleanup(func() { viper.Set("fields", "") })

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := writeObject(cmd, []byte(`{"function":{"id":"1","name":"n"}}`)); err != nil {
		t.Fatalf("writeObject: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"id": "1"`)) {
		t.Errorf("writeObject partial match = %s, want id present", buf.Bytes())
	}
}

func TestWriteRawObjectIgnoresFields(t *testing.T) {
	// Mutation confirmations must NOT be trimmed by a session-wide C1I_FIELDS,
	// or a delete's status object would silently collapse to {}.
	viper.Set("fields", "id")
	t.Cleanup(func() { viper.Set("fields", "") })

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := writeRawObject(cmd, []byte(`{"deleted":3,"toolset_id":"t1"}`)); err != nil {
		t.Fatalf("writeRawObject: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"deleted": 3`)) {
		t.Errorf("writeRawObject dropped confirmation fields under --fields: %s", buf.Bytes())
	}
}
