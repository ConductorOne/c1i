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
