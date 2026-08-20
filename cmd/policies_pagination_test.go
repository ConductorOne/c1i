package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// resetPoliciesListCmdFlags restores policiesListCmd's own flags to their
// zero values and clears pflag's per-flag Changed bit, so tests sharing the
// package-level singleton command can't leak flag state into each other.
// Mirrors resetPoliciesUpdateCmdFlags in cmd/policies_update_test.go.
func resetPoliciesListCmdFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		f := policiesListCmd.Flags().Lookup("page-size")
		_ = f.Value.Set("50")
		f.Changed = false
		f = policiesListCmd.Flags().Lookup("page-token")
		_ = f.Value.Set("")
		f.Changed = false
		f = policiesListCmd.Flags().Lookup("limit")
		_ = f.Value.Set("0")
		f.Changed = false
	}
	reset()
	t.Cleanup(reset)
}

// resetPoliciesSearchCmdFlags is the search-command counterpart. StringSlice
// flags (policy-type, exclude-policy-id) track their own internal "changed"
// bit independent of pflag.Flag.Changed, so a plain Value.Set("") after the
// flag has ever been set once would APPEND instead of clearing it; Replace(nil)
// via the SliceValue interface clears them unconditionally.
func resetPoliciesSearchCmdFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		for _, name := range []string{"query", "display-name", "page-token"} {
			f := policiesSearchCmd.Flags().Lookup(name)
			_ = f.Value.Set("")
			f.Changed = false
		}
		f := policiesSearchCmd.Flags().Lookup("include-deleted")
		_ = f.Value.Set("false")
		f.Changed = false
		for _, name := range []string{"policy-type", "exclude-policy-id"} {
			f := policiesSearchCmd.Flags().Lookup(name)
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			}
			f.Changed = false
		}
		f = policiesSearchCmd.Flags().Lookup("page-size")
		_ = f.Value.Set("25")
		f.Changed = false
		f = policiesSearchCmd.Flags().Lookup("limit")
		_ = f.Value.Set("0")
		f.Changed = false
	}
	reset()
	t.Cleanup(reset)
}

// decodeNDJSONRows splits an emitter's NDJSON output back into rows, in
// emission order, so tests can assert both count and order.
func decodeNDJSONRows(t *testing.T, out string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			t.Fatalf("decoding NDJSON row: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}

// runPoliciesListCmd drives policiesListCmd.RunE directly (the DI seam from
// cmd/policies_update_test.go) and returns its NDJSON output.
func runPoliciesListCmd(t *testing.T, srv *httptest.Server) (string, error) {
	t.Helper()
	stubPoliciesClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")
	var out bytes.Buffer
	policiesListCmd.SetOut(&out)
	policiesListCmd.SetContext(context.Background())
	err := policiesListCmd.RunE(policiesListCmd, []string{})
	return out.String(), err
}

func runPoliciesSearchCmd(t *testing.T, srv *httptest.Server) (string, error) {
	t.Helper()
	stubPoliciesClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")
	var out bytes.Buffer
	policiesSearchCmd.SetOut(&out)
	policiesSearchCmd.SetContext(context.Background())
	err := policiesSearchCmd.RunE(policiesSearchCmd, []string{})
	return out.String(), err
}

// ---- policies list (GET, page token in the "page_token" query param) ----

// TestPoliciesListPaginatesAcrossTwoPages is the load-bearing proof that
// "list" follows nextPageToken instead of stopping after the first page. It
// also pins that the second request actually carries the token the first
// response returned, in the "page_token" query param (not a body field).
func TestPoliciesListPaginatesAcrossTwoPages(t *testing.T) {
	var gotTokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/policies" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tok := r.URL.Query().Get("page_token")
		gotTokens = append(gotTokens, tok)
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"page1-item"}],"nextPageToken":"tok1"}`)
		case "tok1":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"page2-item"}],"nextPageToken":""}`)
		default:
			t.Errorf("unexpected page_token: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetPoliciesListCmdFlags(t)
	out, err := runPoliciesListCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (a break after page 1 would silently emit only 1)", len(rows))
	}
	if rows[0]["id"] != "page1-item" || rows[1]["id"] != "page2-item" {
		t.Errorf("rows out of order or wrong content: %v", rows)
	}
	if len(gotTokens) != 2 {
		t.Fatalf("server received %d requests, want 2", len(gotTokens))
	}
	if gotTokens[0] != "" {
		t.Errorf("first request page_token = %q, want empty", gotTokens[0])
	}
	if gotTokens[1] != "tok1" {
		t.Errorf("second request did not carry the page token from page 1: got %q, want %q", gotTokens[1], "tok1")
	}
}

// TestPoliciesListPaginatesAcrossThreePages guards against a loop
// accidentally hardcoded to exactly two iterations.
func TestPoliciesListPaginatesAcrossThreePages(t *testing.T) {
	var gotTokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("page_token")
		gotTokens = append(gotTokens, tok)
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"item1"}],"nextPageToken":"tok1"}`)
		case "tok1":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"item2"}],"nextPageToken":"tok2"}`)
		case "tok2":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"item3"}],"nextPageToken":""}`)
		default:
			t.Errorf("unexpected page_token: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetPoliciesListCmdFlags(t)
	out, err := runPoliciesListCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	want := []string{"item1", "item2", "item3"}
	for i, id := range want {
		if rows[i]["id"] != id {
			t.Errorf("row %d id = %v, want %q", i, rows[i]["id"], id)
		}
	}
	if len(gotTokens) != 3 {
		t.Fatalf("server received %d requests, want 3", len(gotTokens))
	}
}

// TestPoliciesListEmptyPageWithTokenDoesNotStopEarly pins the comment in
// cmd/policies_list.go: a page can come back with zero rows while
// nextPageToken is still set, and the loop must keep going on the token.
func TestPoliciesListEmptyPageWithTokenDoesNotStopEarly(t *testing.T) {
	var gotTokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("page_token")
		gotTokens = append(gotTokens, tok)
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[],"nextPageToken":"tok1"}`)
		case "tok1":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"page2-item"}],"nextPageToken":""}`)
		default:
			t.Errorf("unexpected page_token: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetPoliciesListCmdFlags(t)
	out, err := runPoliciesListCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if len(gotTokens) != 2 {
		t.Fatalf("server received %d requests, want 2 (an empty page must not stop pagination)", len(gotTokens))
	}
	rows := decodeNDJSONRows(t, out)
	if len(rows) != 1 || rows[0]["id"] != "page2-item" {
		t.Errorf("expected exactly the one row from page 2, got: %v", rows)
	}
}

// TestPoliciesListExplicitPageTokenDisablesAutoPagination pins that a
// caller-supplied --page-token performs exactly one request, even though the
// server reports more pages are available.
func TestPoliciesListExplicitPageTokenDisablesAutoPagination(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		tok := r.URL.Query().Get("page_token")
		if tok != "manual-tok" {
			t.Errorf("page_token = %q, want %q", tok, "manual-tok")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"list":[{"id":"only-item"}],"nextPageToken":"more-available"}`)
	}))
	defer srv.Close()

	resetPoliciesListCmdFlags(t)
	_ = policiesListCmd.Flags().Set("page-token", "manual-tok")
	out, err := runPoliciesListCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if requestCount != 1 {
		t.Fatalf("server received %d requests, want exactly 1 (--page-token should disable auto-pagination)", requestCount)
	}
	rows := decodeNDJSONRows(t, out)
	if len(rows) != 1 || rows[0]["id"] != "only-item" {
		t.Errorf("expected exactly the one page's row, got: %v", rows)
	}
}

// TestPoliciesListLimitStopsEarly pins that --limit caps total emitted rows
// and breaks the pagination loop early, so no extra pages are fetched.
func TestPoliciesListLimitStopsEarly(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		tok := r.URL.Query().Get("page_token")
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"a"},{"id":"b"}],"nextPageToken":"tok1"}`)
		case "tok1":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"c"},{"id":"d"}],"nextPageToken":"tok2"}`)
		default:
			t.Errorf("--limit should have stopped pagination before a third request, got page_token=%q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetPoliciesListCmdFlags(t)
	_ = policiesListCmd.Flags().Set("limit", "3")
	out, err := runPoliciesListCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want exactly 3 (--limit 3)", len(rows))
	}
	if requestCount != 2 {
		t.Errorf("server received %d requests, want 2 (limit reached partway through page 2)", requestCount)
	}
}

// ---- policies search (POST, page token in the "pageToken" body field) ----

// searchRequest captures one request the search-command mock server saw.
type searchRequest struct {
	body map[string]any
}

func decodeSearchRequestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading request body: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("unmarshaling request body: %v (body: %s)", err, b)
	}
	return body
}

// TestPoliciesSearchPaginatesAcrossTwoPages is "search"'s counterpart to the
// list test above: it pins that the loop follows nextPageToken, and that the
// second request carries the token from page 1 as the BODY field
// "pageToken" (camelCase) — not a query param, and not snake_case.
func TestPoliciesSearchPaginatesAcrossTwoPages(t *testing.T) {
	var reqs []searchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/search/policies" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body := decodeSearchRequestBody(t, r)
		reqs = append(reqs, searchRequest{body: body})
		tok, _ := body["pageToken"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"page1-item"}],"nextPageToken":"tok1"}`)
		case "tok1":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"page2-item"}],"nextPageToken":""}`)
		default:
			t.Errorf("unexpected pageToken: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetPoliciesSearchCmdFlags(t)
	out, err := runPoliciesSearchCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (a break after page 1 would silently emit only 1)", len(rows))
	}
	if rows[0]["id"] != "page1-item" || rows[1]["id"] != "page2-item" {
		t.Errorf("rows out of order or wrong content: %v", rows)
	}
	if len(reqs) != 2 {
		t.Fatalf("server received %d requests, want 2", len(reqs))
	}
	if _, present := reqs[0].body["pageToken"]; present {
		t.Errorf("first request should not send a pageToken field at all, got body: %v", reqs[0].body)
	}
	if reqs[1].body["pageToken"] != "tok1" {
		t.Errorf("second request did not carry the page token from page 1: got %v, want %q", reqs[1].body["pageToken"], "tok1")
	}
}

// TestPoliciesSearchPaginatesAcrossThreePages guards against a loop
// accidentally hardcoded to exactly two iterations.
func TestPoliciesSearchPaginatesAcrossThreePages(t *testing.T) {
	var reqs []searchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeSearchRequestBody(t, r)
		reqs = append(reqs, searchRequest{body: body})
		tok, _ := body["pageToken"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"item1"}],"nextPageToken":"tok1"}`)
		case "tok1":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"item2"}],"nextPageToken":"tok2"}`)
		case "tok2":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"item3"}],"nextPageToken":""}`)
		default:
			t.Errorf("unexpected pageToken: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetPoliciesSearchCmdFlags(t)
	out, err := runPoliciesSearchCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	want := []string{"item1", "item2", "item3"}
	for i, id := range want {
		if rows[i]["id"] != id {
			t.Errorf("row %d id = %v, want %q", i, rows[i]["id"], id)
		}
	}
	if len(reqs) != 3 {
		t.Fatalf("server received %d requests, want 3", len(reqs))
	}
}

// TestPoliciesSearchEmptyPageWithTokenDoesNotStopEarly pins the shared
// short/empty-page-continues behavior for "search" too.
func TestPoliciesSearchEmptyPageWithTokenDoesNotStopEarly(t *testing.T) {
	var reqs []searchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeSearchRequestBody(t, r)
		reqs = append(reqs, searchRequest{body: body})
		tok, _ := body["pageToken"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[],"nextPageToken":"tok1"}`)
		case "tok1":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"page2-item"}],"nextPageToken":""}`)
		default:
			t.Errorf("unexpected pageToken: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetPoliciesSearchCmdFlags(t)
	out, err := runPoliciesSearchCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if len(reqs) != 2 {
		t.Fatalf("server received %d requests, want 2 (an empty page must not stop pagination)", len(reqs))
	}
	rows := decodeNDJSONRows(t, out)
	if len(rows) != 1 || rows[0]["id"] != "page2-item" {
		t.Errorf("expected exactly the one row from page 2, got: %v", rows)
	}
}

// TestPoliciesSearchExplicitPageTokenDisablesAutoPagination pins that a
// caller-supplied --page-token performs exactly one request.
func TestPoliciesSearchExplicitPageTokenDisablesAutoPagination(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body := decodeSearchRequestBody(t, r)
		if body["pageToken"] != "manual-tok" {
			t.Errorf("pageToken = %v, want %q", body["pageToken"], "manual-tok")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"list":[{"id":"only-item"}],"nextPageToken":"more-available"}`)
	}))
	defer srv.Close()

	resetPoliciesSearchCmdFlags(t)
	_ = policiesSearchCmd.Flags().Set("page-token", "manual-tok")
	out, err := runPoliciesSearchCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if requestCount != 1 {
		t.Fatalf("server received %d requests, want exactly 1 (--page-token should disable auto-pagination)", requestCount)
	}
	rows := decodeNDJSONRows(t, out)
	if len(rows) != 1 || rows[0]["id"] != "only-item" {
		t.Errorf("expected exactly the one page's row, got: %v", rows)
	}
}

// TestPoliciesSearchLimitStopsEarly pins that --limit caps total emitted
// rows and breaks the pagination loop early for "search" too.
func TestPoliciesSearchLimitStopsEarly(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body := decodeSearchRequestBody(t, r)
		tok, _ := body["pageToken"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"a"},{"id":"b"}],"nextPageToken":"tok1"}`)
		case "tok1":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"c"},{"id":"d"}],"nextPageToken":"tok2"}`)
		default:
			t.Errorf("--limit should have stopped pagination before a third request, got pageToken=%q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetPoliciesSearchCmdFlags(t)
	_ = policiesSearchCmd.Flags().Set("limit", "3")
	out, err := runPoliciesSearchCmd(t, srv)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want exactly 3 (--limit 3)", len(rows))
	}
	if requestCount != 2 {
		t.Errorf("server received %d requests, want 2 (limit reached partway through page 2)", requestCount)
	}
}
