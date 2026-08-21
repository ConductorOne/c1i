package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// functions usage filters automations client-side by callFunction.functionId,
// so its emitted-row count is not 1:1 with automations fetched. Pin that the
// request page_size stays fixed at --page-size regardless of --limit, since
// effectivePageSize's remaining-headroom shrink (built for 1:1 list commands)
// would otherwise collapse it to 1 once a small --limit is set and no match
// has been found yet — turning a handful of batched pages into one request
// per automation scanned.
func TestFunctionsUsagePageSizeStaysFixedUnderLimit(t *testing.T) {
	const functionID = "func1"
	// Matches only show up on the last page, so the loop must scan all 5
	// pages before --limit 1 is satisfied.
	pages := []struct{ id, next string }{
		{"a1", "tok1"},
		{"a2", "tok2"},
		{"a3", "tok3"},
		{"a4", "tok4"},
		{"a5", ""},
	}
	var gotPageSizes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPageSizes = append(gotPageSizes, r.URL.Query().Get("page_size"))
		tok := r.URL.Query().Get("page_token")
		var idx int
		for i := range pages {
			if (i == 0 && tok == "") || (i > 0 && tok == pages[i-1].next) {
				idx = i
				break
			}
		}
		p := pages[idx]
		matched := ""
		if idx == len(pages)-1 {
			matched = fmt.Sprintf(`,"automationSteps":[{"stepName":"s1","callFunction":{"functionId":%s}}]`, jstr(functionID))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":[{"id":%s%s}],"nextPageToken":%s}`, jstr(p.id), matched, jstr(p.next))
	}))
	defer srv.Close()

	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	resetCmdFlags(t, functionsUsageCmd)
	if err := functionsUsageCmd.Flags().Set("page-size", "20"); err != nil {
		t.Fatalf("setting --page-size: %v", err)
	}
	if err := functionsUsageCmd.Flags().Set("limit", "1"); err != nil {
		t.Fatalf("setting --limit: %v", err)
	}

	var out strings.Builder
	functionsUsageCmd.SetOut(&out)
	functionsUsageCmd.SetContext(context.Background())
	if err := functionsUsageCmd.RunE(functionsUsageCmd, []string{functionID}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if len(gotPageSizes) != len(pages) {
		t.Fatalf("server received %d requests, want %d (one per page scanned before the match)", len(gotPageSizes), len(pages))
	}
	for i, ps := range gotPageSizes {
		if ps != "20" {
			t.Errorf("request %d: page_size = %q, want %q (a --limit-driven shrink would send %q here)", i, ps, "20", "1")
		}
	}

	rows := decodeNDJSONRows(t, out.String())
	if len(rows) != 1 || rows[0]["automation_id"] != "a5" {
		t.Errorf("got rows %v, want exactly one row for automation a5", rows)
	}
}
