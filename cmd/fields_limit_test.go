package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// This file covers a regression the --fields empty-row-skip fix
// (cmd/fields.go's emitter.Encode) introduced into --limit: every list
// command's pagination loop compared --limit against a local counter
// incremented every time enc.Encode was CALLED, not every time it actually
// WROTE a line. Before the empty-row-skip fix those were the same thing by
// construction; after it, a row whose --fields projection is empty is
// scanned (Encode is called, incrementing the old counter) but not written
// — so a sparse --fields spec combined with --limit could stop pagination
// having written fewer than --limit lines, or before a later page's real
// matches were ever reached. This violates addLimitFlag's documented
// contract (cmd/flags.go): "the command stops emitting rows after `limit`
// items have been written."
//
// The fix drives limitReached/effectivePageSize off emitter.Written()
// (rows actually written) instead of each command's own scanned-row
// counter, across every list command built on newEmitter+addLimitFlag.
//
// TestTasksListLimitWithSparseFieldsContinuesPastFilteredPage exercises one
// real command end to end (tasks list); cmd/api_test.go's
// TestAPIPaginateLimitWithSparseFieldsContinuesPastFilteredPage covers `api
// --paginate` the same way.

// taskPage builds one /api/v1/search/tasks response page: certifyOutcomes[i]
// is the certify outcome for task i on this page ("" omits taskRow's
// "outcome" key entirely — see finalOutcome/taskRow in cmd/tasks.go).
func taskPage(certifyOutcomes []string, nextToken string) string {
	items := make([]string, 0, len(certifyOutcomes))
	for i, o := range certifyOutcomes {
		outcome, _ := json.Marshal(o)
		items = append(items, fmt.Sprintf(
			`{"task":{"id":"t%d","type":{"certify":{"outcome":%s}}}}`, i, outcome))
	}
	next, _ := json.Marshal(nextToken)
	list := "["
	for i, it := range items {
		if i > 0 {
			list += ","
		}
		list += it
	}
	list += "]"
	return fmt.Sprintf(`{"list":%s,"nextPageToken":%s}`, list, next)
}

// TestTasksListLimitWithSparseFieldsContinuesPastFilteredPage: page 1 has
// two tasks with no terminal outcome (taskRow omits "outcome" for both, so
// --fields outcome projects each to "{}" and the emitter skips writing
// them); page 2 has one task with a real outcome. With --limit 1, the
// command must still fetch page 2 and write exactly that one line — not
// stop after page 1's two scanned-but-unwritten rows already "reached" the
// old (buggy) per-scan counter's limit of 1.
func TestTasksListLimitWithSparseFieldsContinuesPastFilteredPage(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var body struct {
			PageToken string `json:"pageToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		w.Header().Set("Content-Type", "application/json")
		if body.PageToken == "tok1" {
			_, _ = fmt.Fprint(w, taskPage([]string{"TASK_OUTCOME_APPROVED"}, ""))
			return
		}
		_, _ = fmt.Fprint(w, taskPage([]string{"", ""}, "tok1"))
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksListCmd)
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	viper.Set("fields", "outcome")
	t.Cleanup(func() { viper.Set("fields", "") })
	if err := tasksListCmd.Flags().Set("limit", "1"); err != nil {
		t.Fatalf("setting --limit: %v", err)
	}

	var out bytes.Buffer
	tasksListCmd.SetOut(&out)
	tasksListCmd.SetContext(context.Background())
	err := tasksListCmd.RunE(tasksListCmd, []string{})
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if requestCount != 2 {
		t.Fatalf("server received %d requests, want 2 (page 1's two filtered-out rows must not stop pagination before page 2 is fetched)", requestCount)
	}
	rows := decodeNDJSONRows(t, out.String())
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want exactly 1 WRITTEN row (--limit 1), not 1 scanned row: %v", len(rows), rows)
	}
	want := map[string]any{"outcome": "TASK_OUTCOME_APPROVED"}
	if !reflect.DeepEqual(rows[0], want) {
		t.Errorf("row = %v, want %v", rows[0], want)
	}
}

// TestTasksListPageSizeStaysFullDuringSparseFieldsScan pins the request-size
// half of the fix: effectivePageSize must NOT be fed enc.Written() while
// --fields is active, because "remaining = limit - written" stays pinned
// near `limit` for as long as matches stay sparse, collapsing every page
// request down to `limit` (or less) instead of the real requested page
// size. Two pages of --page-size worth of non-matching rows, then a third
// page with enough matches to satisfy --limit: every request's "pageSize"
// body field must equal the full requested page size throughout, never
// shrink toward --limit.
func TestTasksListPageSizeStaysFullDuringSparseFieldsScan(t *testing.T) {
	const requestedPageSize = 10
	var gotPageSizes []float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeJSONBody(t, r)
		if ps, ok := body["pageSize"].(float64); ok {
			gotPageSizes = append(gotPageSizes, ps)
		}
		tok, _ := body["pageToken"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, taskPage(make([]string, requestedPageSize), "tok1"))
		case "tok1":
			_, _ = fmt.Fprint(w, taskPage(make([]string, requestedPageSize), "tok2"))
		case "tok2":
			_, _ = fmt.Fprint(w, taskPage([]string{
				"TASK_OUTCOME_APPROVED", "TASK_OUTCOME_APPROVED", "TASK_OUTCOME_APPROVED",
			}, ""))
		default:
			t.Errorf("unexpected pageToken: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksListCmd)
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	viper.Set("fields", "outcome")
	t.Cleanup(func() { viper.Set("fields", "") })
	if err := tasksListCmd.Flags().Set("page-size", fmt.Sprint(requestedPageSize)); err != nil {
		t.Fatalf("setting --page-size: %v", err)
	}
	if err := tasksListCmd.Flags().Set("limit", "3"); err != nil {
		t.Fatalf("setting --limit: %v", err)
	}

	var out bytes.Buffer
	tasksListCmd.SetOut(&out)
	tasksListCmd.SetContext(context.Background())
	if err := tasksListCmd.RunE(tasksListCmd, []string{}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if len(gotPageSizes) != 3 {
		t.Fatalf("server received %d requests, want 3", len(gotPageSizes))
	}
	for i, ps := range gotPageSizes {
		if ps != requestedPageSize {
			t.Errorf("request %d: pageSize = %v, want %v (must stay at the requested page size, not collapse toward --limit while --fields is filtering out rows)", i, ps, requestedPageSize)
		}
	}

	rows := decodeNDJSONRows(t, out.String())
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want exactly 3 (--limit 3)", len(rows))
	}
}
