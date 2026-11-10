package cmd

import (
	"os"
	"strings"
	"testing"
)

// `apps list` omits deleted apps, so no text may send a reader there to find
// one (its deleted_at is always null).
func TestDocsDoNotClaimAppsListShowsDeletedApps(t *testing.T) {
	r := runCLI(t, nil, nil, "apps", "delete", "--help")
	if strings.Contains(r.stdout, "apps list") && strings.Contains(r.stdout, "deleted_at") {
		t.Errorf("apps delete help still ties apps list to deleted_at:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "omits deleted apps") {
		t.Errorf("apps delete help does not say apps list omits deleted apps:\n%s", r.stdout)
	}
	for _, f := range []string{"../README.md", "agents.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"`apps list` rows carry", "apps list | jq -c 'if .deleted_at"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s still contains %q", f, bad)
			}
		}
	}
}
