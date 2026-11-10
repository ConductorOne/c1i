package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// TestAppRowUserCountIsNumeric pins that user_count keeps its real numeric
// type. The API sends it as a JSON string ("103"); emitting that string
// unchanged makes every string sort above every number in jq, so
// `jq 'select(.user_count > 60)'` would match every row regardless of value.
func TestAppRowUserCountIsNumeric(t *testing.T) {
	var item appListItem
	if err := json.Unmarshal([]byte(`{"id":"a1","userCount":"103"}`), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := appRow(item)

	v, ok := row["user_count"].(int64)
	if !ok {
		t.Fatalf("user_count has type %T, want int64", row["user_count"])
	}
	if v != 103 {
		t.Errorf("user_count = %d, want 103", v)
	}
}

// `apps list` omits deleted apps, so its rows carry no deleted_at (it was
// always null); apps get shows deletedAt.
func TestAppsListRowsHaveNoDeletedAt(t *testing.T) {
	if _, ok := appRow(appListItem{ID: "a1"})["deleted_at"]; ok {
		t.Error("row still has a deleted_at key")
	}
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"list":[{"id":"a1","displayName":"A","userCount":"3"}]}`))
	stub := func() {
		orig := newListClient
		newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
			return client.NewForTesting(s.srv.URL, s.srv.Client()), nil
		}
		t.Cleanup(func() { newListClient = orig })
	}
	r := runCLI(t, stub, nil, "apps", "list")
	if r.err != nil || strings.Contains(r.stdout, "deleted_at") {
		t.Errorf("exit %d (%v), stdout %q", r.exit, r.err, r.stdout)
	}
	r = runCLI(t, stub, nil, "apps", "list", "--fields", "deleted_at")
	if r.exit != exitUsage {
		t.Errorf("--fields deleted_at: exit %d (%v), want %d (matched no keys)", r.exit, r.err, exitUsage)
	}
}
