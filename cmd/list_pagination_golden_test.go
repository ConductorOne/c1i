package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// goldenListPath pins, for every list command in listPaginationCases and each
// scenario below, the exact request sequence, stdout, stderr and exit code.
// Regenerate deliberately with UPDATE_LIST_GOLDEN=1; a diff after a change to
// the pagination loop is a behavior change.
const goldenListPath = "testdata/list_pagination_golden.json"

type goldenRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Params any    `json:"params"`
}

type goldenRun struct {
	Requests []goldenRequest `json:"requests"`
	Stdout   string          `json:"stdout"`
	Stderr   string          `json:"stderr"`
	Exit     int             `json:"exit"`
}

type goldenScenario struct {
	name  string
	flags map[string]string
	// body returns the response for the n-th request (1-based) carrying
	// token, built from the case's page() so each command's envelope is used.
	body func(tc listPaginationCase, n int, token string) string
}

func threePages(tc listPaginationCase, _ int, token string) string {
	switch token {
	case "":
		return tc.page([]string{"a1", "a2", "a3"}, "t1")
	case "t1":
		return tc.page([]string{"b1", "b2", "b3"}, "t2")
	default:
		return tc.page([]string{"c1", "c2", "c3"}, "")
	}
}

// Rows carrying none of the fields any command reads, under every list key.
const unrecognizedRows = `{"list":[{"x":1},{"x":2}],"tools":[{"x":1}],"bindings":[{"x":1}],"profiles":[{"x":1}],"classifiers":[{"x":1}],"templates":[{"x":1}],"nextPageToken":"t1"}`

func goldenScenarios() []goldenScenario {
	var stuckIDs []string
	for i := 0; i < stuckStaleRows; i++ {
		stuckIDs = append(stuckIDs, fmt.Sprintf("s%d", i))
	}
	return []goldenScenario{
		{name: "default", body: threePages},
		{name: "limit-within-page", flags: map[string]string{"limit": "2"}, body: threePages},
		{name: "limit-spans-pages", flags: map[string]string{"limit": "4"}, body: threePages},
		{name: "limit-ends-on-page", flags: map[string]string{"limit": "6"}, body: threePages},
		{name: "page-size", flags: map[string]string{"page-size": "7"}, body: threePages},
		{name: "page-size-clamped", flags: map[string]string{"page-size": "100000"}, body: threePages},
		{name: "page-size-and-limit", flags: map[string]string{"page-size": "5", "limit": "7"}, body: threePages},
		{name: "page-token", flags: map[string]string{"page-token": "t1"}, body: threePages},
		{name: "stuck-repeated-token", body: func(tc listPaginationCase, _ int, _ string) string {
			return tc.page(stuckIDs, "same")
		}},
		{name: "stuck-fresh-tokens", body: func(tc listPaginationCase, n int, _ string) string {
			return tc.page(stuckIDs, fmt.Sprintf("fresh%d", n))
		}},
		{name: "empty-pages-then-rows", body: func(tc listPaginationCase, _ int, token string) string {
			switch token {
			case "":
				return tc.page(nil, "t1")
			case "t1":
				return tc.page(nil, "t2")
			default:
				return tc.page([]string{"z1"}, "")
			}
		}},
		{name: "unrecognized-rows", body: func(_ listPaginationCase, _ int, _ string) string { return unrecognizedRows }},
		{name: "list-wrong-type", body: func(_ listPaginationCase, _ int, _ string) string {
			return `{"list":5,"tools":5,"bindings":5,"profiles":5,"classifiers":5,"templates":5}`
		}},
		{name: "not-json", body: func(_ listPaginationCase, _ int, _ string) string { return `<html>nope</html>` }},
		{name: "empty-object", body: func(_ listPaginationCase, _ int, _ string) string { return `{}` }},
		{name: "second-page-bad-shape", body: func(tc listPaginationCase, _ int, token string) string {
			if token == "" {
				return tc.page([]string{"a1"}, "t1")
			}
			return strings.Replace(unrecognizedRows, `"t1"`, `"t2"`, 1)
		}},
	}
}

func recordGoldenRequest(t *testing.T, r *http.Request) goldenRequest {
	t.Helper()
	req := goldenRequest{Method: r.Method, Path: r.URL.Path}
	// Leave the body readable for extractPageToken.
	raw, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if r.Method == http.MethodGet {
		q := r.URL.Query()
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		params := map[string]string{}
		for _, k := range keys {
			params[k] = strings.Join(q[k], ",")
		}
		req.Params = params
	} else {
		req.Params = decodeJSONBody(t, r)
		r.Body = io.NopCloser(bytes.NewReader(raw))
	}
	return req
}

func runGoldenCase(t *testing.T, tc listPaginationCase, sc goldenScenario) goldenRun {
	t.Helper()
	var run goldenRun
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		run.Requests = append(run.Requests, recordGoldenRequest(t, r))
		if len(run.Requests) > 6 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tok := extractPageToken(t, tc.method, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, sc.body(tc, len(run.Requests), tok))
	}))
	defer srv.Close()

	tc.extraFlags = mergeFlags(tc.extraFlags, sc.flags)
	out, errOut, err := runListPaginationCase(t, tc, srv)
	run.Stdout, run.Stderr = digestLong(out), digestLong(errOut)
	if err != nil {
		run.Exit = exitCode(err)
		msg := err.Error()
		// Go's decoder wording names the command's private struct types.
		if i := strings.Index(msg, "failed to parse response"); i >= 0 {
			msg = msg[:i+len("failed to parse response")]
		}
		run.Stderr += "ERR: " + msg + "\n"
	}
	// The test server listens on a random port; keep it out of the transcript.
	run.Stderr = strings.ReplaceAll(run.Stderr, srv.URL, "SERVER")
	return run
}

func mergeFlags(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestListCommandsPaginationGolden(t *testing.T) {
	got := map[string]goldenRun{}
	for _, tc := range listPaginationCases() {
		for _, sc := range goldenScenarios() {
			key := tc.name + " / " + sc.name
			t.Run(key, func(t *testing.T) { got[key] = runGoldenCase(t, tc, sc) })
		}
	}
	if t.Failed() {
		return
	}
	if os.Getenv("UPDATE_LIST_GOLDEN") != "" {
		// One transcript per line keeps regenerations reviewable.
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		sb.WriteString("{\n")
		for i, k := range keys {
			kj, _ := json.Marshal(k)
			vj, err := json.Marshal(got[k])
			if err != nil {
				t.Fatal(err)
			}
			sb.WriteString(" " + string(kj) + ": " + string(vj))
			if i < len(keys)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("}")
		b := []byte(sb.String())
		if err := os.MkdirAll(filepath.Dir(goldenListPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenListPath, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(goldenListPath)
	if err != nil {
		t.Fatalf("reading %s: %v (regenerate with UPDATE_LIST_GOLDEN=1)", goldenListPath, err)
	}
	var want map[string]goldenRun
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for key, w := range want {
		g, ok := got[key]
		if !ok {
			t.Errorf("%s: in golden file, not run", key)
			continue
		}
		wj, _ := json.Marshal(w)
		gj, _ := json.Marshal(g)
		if string(wj) != string(gj) {
			t.Errorf("%s: transcript changed\n want %s\n  got %s", key, wj, gj)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("%s: run, not in golden file", key)
		}
	}
}

// digestLong keeps small transcripts readable and reduces a large one (the
// stuck-cursor runs print dozens of rows) to its line count and SHA-256.
func digestLong(s string) string {
	if len(s) <= 2048 {
		return s
	}
	return fmt.Sprintf("sha256:%x lines:%d", sha256.Sum256([]byte(s)), strings.Count(s, "\n"))
}
