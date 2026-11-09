package cmd

import (
	"os"
	"strings"
	"testing"
)

// TestUserCountNoteDocumented keeps the user_count caveat identical in the
// help text and both docs.
func TestUserCountNoteDocumented(t *testing.T) {
	if !strings.Contains(appsListCmd.Long, userCountNote) {
		t.Error("apps list --help lacks userCountNote")
	}
	for _, f := range []string{"../README.md", "agents.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(strings.Fields(string(b)), " "), userCountNote) {
			t.Errorf("%s lacks userCountNote", f)
		}
	}
}
