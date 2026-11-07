package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// A preview names the tenant it would hit, so every dry-run path resolves the
// URL first and fails without one, like a real send.
func TestDryRunCommandsNeedURL(t *testing.T) {
	ent := []string{"--app-id", "app1", "--entitlement-id", "ent1"}
	for _, argv := range [][]string{
		{"mcp", "classifiers", "delete", "cl1"},
		{"access-profiles", "bundle-automation", "resume", "ap1"},
		{"access-profiles", "bundle-automation", "delete", "ap1"},
		append([]string{"access-profiles", "requestable-entitlements", "add", "ap1"}, ent...),
		append([]string{"access-profiles", "requestable-entitlements", "remove", "ap1"}, ent...),
		append([]string{"access-profiles", "visibility-entitlements", "add", "ap1"}, ent...),
		append([]string{"access-profiles", "visibility-entitlements", "remove", "ap1"}, ent...),
	} {
		t.Run(strings.Join(argv[:3], " "), func(t *testing.T) {
			withDryRun(t)
			// Control: with a URL the same argv previews, so the refusal
			// below is about the URL, not the arguments.
			t.Setenv("C1I_URL", mutTestURL)
			if r := runMutationCLI(t, nil, 0, nil, argv...); r.exit != exitOK || !strings.Contains(r.stdout, `"dryRun": true`) {
				t.Fatalf("with a URL: exit %d stdout %q stderr %q; want a preview", r.exit, r.stdout, r.stderr)
			}
			t.Setenv("C1I_URL", "")
			orig := viper.GetString("url")
			viper.Set("url", "")
			t.Cleanup(func() { viper.Set("url", orig) })
			r := runMutationCLI(t, nil, 0, nil, argv...)
			if r.exit != exitUsage || r.stdout != "" || !strings.Contains(r.stderr, "url is required") {
				t.Errorf("exit %d stdout %q stderr %q; want exit 2, no preview, the URL error", r.exit, r.stdout, r.stderr)
			}
		})
	}
}
