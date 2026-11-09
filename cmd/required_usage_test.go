package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A hand-written "(required)" carries no annotation, so validateBlankFlags
// skips the flag and a blank value slips through.
func TestRequiredUsageMatchesAnnotation(t *testing.T) {
	checked := 0
	walkCommandTree(func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			says := strings.Contains(f.Usage, "(required)")
			has := f.Annotations[requiredAnnotation] != nil
			if says || has {
				checked++
			}
			if says && !has {
				t.Errorf("%s --%s says \"(required)\" without requiredAnnotation; register it with markRequired or annotateRequired", c.CommandPath(), f.Name)
			}
			if has && !says {
				t.Errorf("%s --%s carries requiredAnnotation but its usage omits \"(required)\"", c.CommandPath(), f.Name)
			}
		})
	})
	if checked < 20 {
		t.Errorf("only %d required flags seen; the tree walk has gone vacuous", checked)
	}
}

func TestBlankUpdateMaskExitsUsage(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	body := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(body, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range [][]string{
		{"access-reviews", "update", "abc"},
		{"mcp", "classifiers", "update", "abc"},
	} {
		for _, v := range blankValues {
			srv := newMutServer(t, fixedReply(200, "{}"))
			args := append(append([]string(nil), path...), "--body-file", body, "--update-mask", v)
			res := runMutationCLI(t, srv.srv, 0, nil, args...)
			if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), "flag --update-mask requires a non-empty value") {
				t.Errorf("%v mask %q: exit = %d, err = %v, want the central blank-flag usage error", path, v, res.exit, res.err)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("%v mask %q: requests sent: %v", path, v, got)
			}
		}
	}
}
