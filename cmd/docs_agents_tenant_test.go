package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// TestAgentsDocTenantSectionMatchesCode pins the runtime claims in agents.md's
// "Tenant and auth" section to the code's own constants, so a rewording or a
// changed exit code fails here instead of shipping a false claim.
func TestAgentsDocTenantSectionMatchesCode(t *testing.T) {
	doc := strings.Join(strings.Fields(agentsTemplate), " ") // line wraps are not content
	_, section, ok := strings.Cut(doc, "## Tenant and auth")
	if !ok {
		t.Fatal("agents.md has no \"## Tenant and auth\" section")
	}
	section, _, _ = strings.Cut(section, "## Global flags")

	usage := rootCmd.PersistentFlags().Lookup("url").Usage
	configLabel := urlSourceLabel(URLSourceConfig)
	claims := map[string]string{
		"config-file warning text": fmt.Sprintf(urlDefaultWarningFmt, "<url>", configLabel),
		"resolution order":         fmt.Sprintf("`--url`, `%s`, then `url:` in `%s`", urlEnvVar, configLabel),
		"whoami tenantSource":      fmt.Sprintf("(`%s`, `%s`, or `%s`", urlSourceToken(URLSourceFlag), urlSourceToken(URLSourceEnv), urlSourceToken(URLSourceConfig)),
		"bad tenant URL exit":      fmt.Sprintf("usage errors (exit `%d`)", exitUsage),
		"unknown scoped role exit": fmt.Sprintf("exits %d and lists the valid names; an unknown ID exits %d", exitUsage, exitNotFound),
		"403 exit":                 fmt.Sprintf("`403` (exit %d)", exitAuth),
		"temporary credential":     fmt.Sprintf("\"temporary credential\" (exit %d)", exitCode(&helperLeftError{})),
	}
	for name, want := range claims {
		if !strings.Contains(section, want) {
			t.Errorf("agents.md tenant section lacks %s: %q", name, want)
		}
	}
	// The --url help text states the same order as the doc.
	if want := fmt.Sprintf("--url, %s, then %s", urlEnvVar, configLabel); !strings.Contains(usage, want) {
		t.Errorf("--url usage %q lacks the resolution order %q", usage, want)
	}
}
