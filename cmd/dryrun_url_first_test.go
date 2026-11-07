package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// A preview names the tenant it would hit, so every dry-run path resolves the
// URL first and fails without one, like a real send.
func TestDryRunHelpersNeedURL(t *testing.T) {
	withDryRun(t)
	t.Setenv("C1I_URL", "")
	orig := viper.GetString("url")
	viper.Set("url", "")
	t.Cleanup(func() { viper.Set("url", orig) })
	resetCmdFlags(t, rootCmd)

	body := map[string]any{}
	helpers := map[string]func(*cobra.Command) error{
		"postClassifierMutation":          func(c *cobra.Command) error { return postClassifierMutation(c, "/api/v1/x", body, byID("id")) },
		"postBundleAutomation":            func(c *cobra.Command) error { return postBundleAutomation(c, "ap", "/run", body) },
		"postAccessProfileEntitlements":   func(c *cobra.Command) error { return postAccessProfileEntitlements(c, "/api/v1/x", body) },
		"deleteAccessProfileEntitlements": func(c *cobra.Command) error { return deleteAccessProfileEntitlements(c, "/api/v1/x", body) },
		"bundle-automation delete": func(c *cobra.Command) error {
			return accessProfilesBundleAutomationDeleteCmd.RunE(c, []string{"ap"})
		},
	}
	for name, run := range helpers {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			c := &cobra.Command{}
			c.SetOut(&out)
			c.SetContext(context.Background())
			err := run(c)
			if exitCode(err) != exitUsage || out.Len() != 0 {
				t.Errorf("err %v (exit %d), stdout %q; want exit 2 and no preview", err, exitCode(err), out.String())
			}
		})
	}
}
