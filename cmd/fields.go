package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
				continue
			}
			// Single-object reads pass the API response through as-is, which
			// wraps the resource under the endpoint's own top-level key
			// (userView.user, function, app, ...). A path that doesn't resolve
			// from the root falls back to a depth-insensitive search: try the
			// same path starting from every nested object, so `--fields id`
			// finds `userView.user.id` without the caller needing to know the
			// wrapper key. See lookupPathAnyDepth for the ambiguity rule.
			if keys, val, ok := lookupPathAnyDepth(t, p); ok {
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

// lookupPathAnyDepth searches m's descendant objects (not m itself — callers
// only reach here after a root-anchored lookupPath(m, path) already failed)
// for the first place path resolves, one nesting level at a time. This is the
// depth-insensitive half of field matching: a single-object read wraps its
// payload under the endpoint's own key (userView.user, function, app, ...),
// so an unqualified `--fields id` (or a dot-path shorter than the real
// nesting) would otherwise match nothing and silently project to {}.
//
// Shallower matches win: the search stops at the first level where at least
// one match exists, without descending further, so `--fields id` prefers an
// outer "id" over one buried deeper. Two matches at the *same* depth are a
// genuine ambiguity (e.g. sibling objects that both have an "id"); it is
// resolved the same way resolveKey resolves a casing tie — deterministically —
// rather than by map-iteration order (random) or by erroring (which would make
// the depth-insensitive fallback unpredictable to rely on). This never
// overrides an exact/root match: lookupPath is always tried first by the
// caller. Arrays are not searched into; only nested objects are — the
// observed wrapper shapes are all objects, and picking an array index to
// descend into would be its own ambiguity.
//
// The tie-break compares the candidates' full []string path segments
// element-wise (lessPath), NOT a "."-joined string. Joining first is a trap:
// a JSON key can itself legally contain a literal dot (e.g. a sibling
// structure {"a":{"b.c":{"id":..}}} vs {"a.b":{"c":{"id":..}}}), and both
// paths join to the identical string "a.b.c.id" despite being genuinely
// different locations. A string comparator then reports neither candidate as
// less than the other, so sort.Slice (which is not a stable sort) falls back
// to whatever order the candidates happened to arrive in — which comes from
// randomized Go map iteration. That turns a deterministic input into
// nondeterministic output: the same `--fields id` against the same response
// could return a different value from run to run. Comparing the segment
// slices directly can't collide this way, because two different locations in
// a JSON tree always differ in at least one *segment*, even when their
// dotted-string joins coincide.
func lookupPathAnyDepth(m map[string]any, path []string) ([]string, any, bool) {
	matches := matchesPathAnyDepth(m, path)
	if len(matches) == 0 {
		return nil, nil, false
	}
	return matches[0].path, matches[0].val, true
}

// pathMatch is one place a depth-insensitive search resolved a path: the full
// segment path from the searched root, and the value there.
type pathMatch struct {
	path []string
	val  any
}

// matchesPathAnyDepth is the search lookupPathAnyDepth describes, returning
// EVERY match at the shallowest depth that has one, sorted by lessPath.
// lookupPathAnyDepth takes the first; unwrapEnvelope (cmd/unwrap.go) instead
// refuses a set with more than one, since a same-depth tie there would promote
// the wrong object to the top level.
func matchesPathAnyDepth(m map[string]any, path []string) []pathMatch {
	type node struct {
		prefix []string
		obj    map[string]any
	}
	prefixed := func(prefix []string, seg string) []string {
		next := make([]string, len(prefix)+1)
		copy(next, prefix)
		next[len(prefix)] = seg
		return next
	}

	var level []node
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			level = append(level, node{[]string{k}, sub})
		}
	}
	for len(level) > 0 {
		var matches []pathMatch
		var next []node
		for _, n := range level {
			if matched, val, ok := lookupPath(n.obj, path); ok {
				full := append(append([]string{}, n.prefix...), matched...)
				matches = append(matches, pathMatch{full, val})
			}
			for k, v := range n.obj {
				if sub, ok := v.(map[string]any); ok {
					next = append(next, node{prefixed(n.prefix, k), sub})
				}
			}
		}
		if len(matches) > 0 {
			sort.Slice(matches, func(i, j int) bool {
				return lessPath(matches[i].path, matches[j].path)
			})
			return matches
		}
		level = next
	}
	return nil
}

// lessPath reports whether path a sorts before path b, comparing segment
// count first (fewer segments first) and then each segment in order, plain
// string comparison. See lookupPathAnyDepth's comment for why this must NOT
// be done by joining segments with "." first and comparing the resulting
// strings: two structurally different paths can join to the identical
// string when a segment itself contains a literal dot, which would make
// sort.Slice's ordering depend on randomized map-iteration order instead of
// the paths' actual content. Comparing segments directly can't have that
// collision — two different locations in a decoded JSON tree always differ
// in at least one segment.
func lessPath(a, b []string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
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
	// If several keys normalize to the same value (e.g. a source with both
	// "displayName" and "display_name" and a spec of "display-name"), map
	// iteration order is random — so pick the lexicographically smallest match
	// for a stable, deterministic result instead of a flaky one.
	best := ""
	found := false
	for k := range m {
		if normalizeKey(k) == want && (!found || k < best) {
			best, found = k, true
		}
	}
	if found {
		return best, m[best], true
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

// fieldsMatchState tracks, across every row emitted by a single list-command
// invocation, whether --fields matched anything anywhere. A
// list command projects each row independently as it streams (--paginate
// commands fetch potentially-unbounded results and must not buffer them just
// to answer this question), so no single Encode call can know whether the
// requested paths are bogus versus simply absent on this particular row.
// Instead every emitter sharing one invocation accumulates into the same
// state, and checkFieldsMatchedAnyRow inspects it once, after the command
// finishes successfully.
//
// This is intentionally NOT a package-level variable. It is threaded through
// cmd.Context() (withFieldsMatchState / fieldsMatchStateFromContext) so each
// command invocation gets its own, independent instance: many tests in this
// package drive rootCmd.ExecuteContext repeatedly in the same test binary,
// and a shared global would leak match state between them (and would be the
// same class of hidden, easy-to-forget coupling a repeated per-call-site
// defer would have been).
type fieldsMatchState struct {
	sawRow  bool // at least one row was emitted (the result wasn't empty)
	matched bool // at least one row's projection kept at least one field
}

// fieldsMatchStateKey is the context key fieldsMatchState is stored under.
// Unexported struct type per Go convention for context keys: it can't
// collide with any key from another package.
type fieldsMatchStateKey struct{}

// withFieldsMatchState attaches a fresh *fieldsMatchState to ctx. Called
// once, from rootCmd's PersistentPreRunE (cmd/root.go), before any command's
// RunE runs — so every newEmitter created during that invocation shares the
// same tracker via the command's context.
func withFieldsMatchState(ctx context.Context) context.Context {
	return context.WithValue(ctx, fieldsMatchStateKey{}, &fieldsMatchState{})
}

// fieldsMatchStateFromContext returns the tracker withFieldsMatchState
// attached, or nil when none was attached — e.g. ctx is nil, or a unit test
// builds a bare *cobra.Command and calls newEmitter directly without driving
// it through rootCmd's PersistentPreRunE. A nil result just means Encode
// skips the bookkeeping; projection itself is unaffected either way.
func fieldsMatchStateFromContext(ctx context.Context) *fieldsMatchState {
	if ctx == nil {
		return nil
	}
	st, _ := ctx.Value(fieldsMatchStateKey{}).(*fieldsMatchState)
	return st
}

// checkFieldsMatchedAnyRow is rootCmd's PersistentPostRunE (cmd/root.go) —
// the single central hook for the list-side half of the --fields zero-match
// fix (see writeObject/projectionMatchedNothing above for the
// single-object half, which this mirrors). cobra only calls
// PersistentPostRunE after a command's RunE has already returned nil, so a
// pre-existing failure (a 404, a usage error, ...) is returned exactly as-is
// and is never reachable here — this check can only ever run on an
// otherwise-successful command, and can only ever make a successful exit
// stricter, never mask a failure.
//
// Do NOT add a PersistentPostRunE to any subcommand: cobra runs only the
// NEAREST ancestor's PersistentPostRunE (the same rule it uses for
// PersistentPreRunE), so an override would silently stop this check from
// running for that subcommand and everything under it — the exact
// silent-miss failure mode this whole design exists to avoid.
// TestNoSubcommandDefinesOwnPersistentPostRunE (cmd/root_test.go) walks the
// whole command tree and fails CI if that ever happens.
func checkFieldsMatchedAnyRow(cmd *cobra.Command, _ []string) error {
	st := fieldsMatchStateFromContext(cmd.Context())
	if st == nil || !st.sawRow || st.matched {
		return nil
	}
	return &usageError{fmt.Errorf("--fields %q matched no keys in any row of the response", viper.GetString("fields"))}
}

// emitter writes NDJSON rows, applying --fields projection when set. List
// commands use it in place of a bare json.Encoder so field selection is
// consistent across every list surface.
type emitter struct {
	enc     *json.Encoder
	paths   [][]string
	state   *fieldsMatchState
	written int
}

// newEmitter builds an emitter writing to cmd's stdout, reading the
// projection from the global --fields flag. When --fields is set, it also
// picks up the invocation's *fieldsMatchState from cmd.Context() (attached by
// rootCmd's PersistentPreRunE) so every row Encode writes gets folded into
// the one check checkFieldsMatchedAnyRow runs after the command returns.
func newEmitter(cmd *cobra.Command) *emitter {
	return &emitter{
		enc:   json.NewEncoder(cmd.OutOrStdout()),
		paths: fieldPaths(),
		state: fieldsMatchStateFromContext(cmd.Context()),
	}
}

// Encode writes v as one NDJSON line, projected to --fields when set. The
// name and signature match json.Encoder.Encode so an emitter is a drop-in
// replacement for the bare encoder list commands used before.
//
// A row whose projection is empty ("{}") carries no information, so it is
// skipped rather than written — this is decided per row, with no buffering,
// so it's safe even under --paginate's unbounded result sets. Whether the
// whole invocation ever matched anything is still tracked in e.state and
// judged once at the end by checkFieldsMatchedAnyRow, which is what turns an
// all-rows-empty result into the zero-match usage error; skipping the empty
// row here only changes what gets printed, never that verdict.
func (e *emitter) Encode(v any) error {
	if len(e.paths) == 0 {
		e.written++
		return e.enc.Encode(v)
	}
	projected := projectValue(v, e.paths)
	empty := projectionMatchedNothing(projected)
	if e.state != nil {
		e.state.sawRow = true
		if !empty {
			e.state.matched = true
		}
	}
	if empty {
		return nil
	}
	e.written++
	return e.enc.Encode(projected)
}

// Written reports how many rows Encode has actually written to stdout so
// far, as opposed to how many times it's been called. The two diverge once
// --fields is set: a call whose projection is empty is skipped (see Encode
// above), so it's scanned but not written. --limit must cap WRITTEN rows
// (addLimitFlag's documented contract), so every list command's pagination
// loop drives limitReached/effectivePageSize off this instead of a
// separately incremented local counter — a local counter tracks calls, and
// once calls and writes diverge, comparing --limit against calls can stop
// pagination before a later page's real matches are ever reached.
func (e *emitter) Written() int {
	return e.written
}

// Filtered reports whether a --fields/C1I_FIELDS projection is active, i.e.
// whether a fetched row might not become a written one (see Encode). List
// commands must check this (alongside any of their own client-side filters)
// before calling effectivePageSize — see that function's doc for why feeding
// it a progress count while rows can be dropped collapses the page size.
func (e *emitter) Filtered() bool {
	return len(e.paths) > 0
}

// writeObject pretty-prints a single JSON response to stdout, applying --fields
// projection when set. Use it for read/get output. It falls back to raw
// pretty-printing when projection is off or the bytes aren't valid JSON.
//
// A projection that matches none of the requested paths is a usage error, not
// a successful empty result: depth-insensitive matching (see
// lookupPathAnyDepth) closes the common case where a name just needed to be
// found under the response's wrapper key, but a genuine typo (or a field that
// truly doesn't exist anywhere in the response) must not be allowed to print
// "{}" and exit 0 — that reads as "the resource has no such data" when what
// actually happened is "the requested fields don't exist". Failing loudly here
// is the backstop for whatever depth-insensitive matching doesn't catch.
func writeObject(cmd *cobra.Command, data []byte) error {
	if paths := fieldPaths(); len(paths) > 0 {
		if projected, ok := projectBytes(data, paths); ok {
			if projectionMatchedNothing(projected) {
				return &usageError{fmt.Errorf("--fields %q matched no keys in the response", viper.GetString("fields"))}
			}
			if b, err := json.MarshalIndent(projected, "", "  "); err == nil {
				_, _ = cmd.OutOrStdout().Write(append(b, '\n'))
				return nil
			}
		}
	}
	return writeRawObject(cmd, data)
}

// projectionMatchedNothing reports whether a projected value has nothing in
// it: an empty object, or a non-empty array whose every element is itself
// empty. An empty array is not a miss — the projection may be exactly right,
// there's just no data — so it is left alone.
func projectionMatchedNothing(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		return len(t) == 0
	case []any:
		if len(t) == 0 {
			return false
		}
		for _, el := range t {
			if !projectionMatchedNothing(el) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// writeRawObject pretty-prints a single JSON response to stdout WITHOUT applying
// --fields. Use it for mutation confirmations (create/update/delete), whose
// status objects must never be trimmed away by a session-wide C1I_FIELDS — a
// projection that matched none of the confirmation's keys would silently emit
// "{}" and hide whether the change succeeded.
//
// An empty (or whitespace-only) body is treated as a legitimate no-content
// success — some endpoints answer 2xx with nothing — and writes nothing.
// Anything else that isn't valid JSON is a *nonJSONResponseError instead of a
// silent verbatim dump: a 200 with an HTML/text body (e.g. a path that
// escaped the API prefix) must not read as success to a downstream parser.
func writeRawObject(cmd *cobra.Command, data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	out := cmd.OutOrStdout()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		return &nonJSONResponseError{fmt.Errorf("response was not JSON: %w", err)}
	}
	pretty.WriteByte('\n')
	_, _ = out.Write(pretty.Bytes())
	return nil
}
