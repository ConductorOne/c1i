package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// TestExecutionRowCompletedAtIsNullNotEmptyString pins that completed_at is
// untyped nil, not "", for a non-terminal execution (pending, creating,
// waiting) — "" is truthy in jq and would make `jq 'select(.completed_at)'`
// match every row, including ones that never finished.
func TestExecutionRowCompletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name        string
		completedAt string
		want        any
	}{
		{name: "pending execution", completedAt: "", want: nil},
		{name: "completed execution", completedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := executionRow(executionListItem{ID: "e1", CompletedAt: tt.completedAt})
			got, ok := row["completed_at"]
			if !ok {
				t.Fatal("row has no completed_at key")
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("completed_at = %#v, want untyped nil", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("completed_at = %v, want %v", got, tt.want)
			}
		})
	}
}

// The search route wraps each item in "automationExecution"; every field must
// survive the unwrap, and --state/--template-id must still filter.
func TestAutomationsExecutionsListUnwrapsSearchItems(t *testing.T) {
	item := func(id, tmpl, state string) string {
		return fmt.Sprintf(`{"automationExecution":{"id":%q,"automationTemplateId":%q,"state":%q,`+
			`"createdAt":"2026-01-01T00:00:00Z","completedAt":"2026-01-01T00:01:00Z","duration":"60s","isDraft":true},`+
			`"automationPath":""}`, id, tmpl, state)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/search/automation_executions" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":[%s,%s,%s],"nextPageToken":"","expanded":[]}`,
			item("e1", "t1", "AUTOMATION_EXECUTION_STATE_DONE"),
			item("e2", "t2", "AUTOMATION_EXECUTION_STATE_DONE"),
			item("e3", "t1", "AUTOMATION_EXECUTION_STATE_ERROR"))
	}))
	defer srv.Close()

	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	resetCmdFlags(t, automationsExecutionsListCmd)
	if err := automationsExecutionsListCmd.Flags().Set("state", "done"); err != nil {
		t.Fatal(err)
	}
	if err := automationsExecutionsListCmd.Flags().Set("template-id", "t1"); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	automationsExecutionsListCmd.SetOut(&out)
	automationsExecutionsListCmd.SetContext(context.Background())
	if err := automationsExecutionsListCmd.RunE(automationsExecutionsListCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	rows := decodeNDJSONRows(t, out.String())
	want := []map[string]any{{
		"id":                     "e1",
		"automation_template_id": "t1",
		"state":                  "AUTOMATION_EXECUTION_STATE_DONE",
		"created_at":             "2026-01-01T00:00:00Z",
		"completed_at":           "2026-01-01T00:01:00Z",
		"duration":               "60s",
		"is_draft":               true,
	}}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %v, want %v", rows, want)
	}
}

// A list item without its automationExecution wrapper must be skipped, not
// emitted as a row of empty strings.
func TestAutomationsExecutionsListSkipsMissingWrapper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"list":[{"automationExecution":{"id":"e1","state":"AUTOMATION_EXECUTION_STATE_DONE"}},{"automationExecution":null},{}],"nextPageToken":""}`)
	}))
	defer srv.Close()

	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	resetCmdFlags(t, automationsExecutionsListCmd)
	var out strings.Builder
	automationsExecutionsListCmd.SetOut(&out)
	automationsExecutionsListCmd.SetContext(context.Background())
	if err := automationsExecutionsListCmd.RunE(automationsExecutionsListCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	rows := decodeNDJSONRows(t, out.String())
	if len(rows) != 1 || rows[0]["id"] != "e1" {
		t.Fatalf("rows = %v, want only e1", rows)
	}
}
