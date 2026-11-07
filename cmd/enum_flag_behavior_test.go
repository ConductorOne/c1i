package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// TestEnumFlagRefusalsExitUsage drives each enum value the shared table
// refuses through the real command. Each row exited 0 before: a write sent
// the value (here, printed it under --dry-run) and a read got an empty page
// back from the stub. Only symbols that predate enumFlag are used, so this
// file also runs against the old mappers.
func TestEnumFlagRefusalsExitUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"list":[]}`))
	}))
	t.Cleanup(srv.Close)
	stub := func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	origList, origPolicies := newListClient, newPoliciesClient
	newListClient, newPoliciesClient = stub, stub
	t.Cleanup(func() { newListClient, newPoliciesClient = origList, origPolicies })

	register := []string{"mcp", "servers", "register", "--dry-run", "--app-id", "a", "--display-name", "d"}
	external := append(append([]string{}, register...), "--type", "external", "--server-url", "https://mcp.example")
	hosted := append(append([]string{}, register...), "--type", "hosted", "--catalog-id", "cat")
	profile := []string{"access-profiles", "create", "--dry-run", "--display-name", "d"}

	cases := []struct {
		name string
		args []string
		cmd  *cobra.Command
	}{
		{"register --token-sharing unknown", append(external, "--token-sharing", "global"), mcpServersRegisterCmd},
		{"register --token-sharing blank", append(external, "--token-sharing", "  "), mcpServersRegisterCmd},
		{"register --transport unknown", append(external, "--transport", "stdio"), mcpServersRegisterCmd},
		{"register --data-sensitivity unknown", append(hosted, "--data-sensitivity", "secret"), mcpServersRegisterCmd},
		{"servers update --data-sensitivity unknown", []string{"mcp", "servers", "update", "conn-1", "--app-id", "a", "--dry-run", "--data-sensitivity", "secret"}, mcpServersUpdateCmd},
		{"access-profiles --enrollment-behavior unknown", append(profile, "--enrollment-behavior", "maybe"), accessProfilesCreateCmd},
		{"access-profiles --unenrollment-behavior unknown", append(profile, "--unenrollment-behavior", "keep"), accessProfilesCreateCmd},
		{"access-profiles --unenrollment-entitlement-behavior blank", append(profile, "--unenrollment-entitlement-behavior", " "), accessProfilesCreateCmd},
		{"tools approve --state lookalike", []string{"mcp", "tools", "approve", "tool-1", "--app-id", "a", "--connector-id", "c", "--dry-run", "--state", "pendıng"}, mcpToolsApproveCmd},
		{"tools search --state unknown", []string{"mcp", "tools", "search", "--app-id", "a", "--connector-id", "c", "--state", "bogus"}, mcpToolsSearchCmd},
		{"servers search --tool-state unknown", []string{"mcp", "servers", "search", "--app-id", "a", "--tool-state", "bogus"}, mcpServersSearchCmd},
		{"policies search --policy-type unknown", []string{"policies", "search", "--policy-type", "bogus"}, policiesSearchCmd},
		{"accounts list --status unknown", []string{"accounts", "list", "--app-id", "a", "--status", "bogus"}, accountsListCmd},
		{"accounts list --type blank", []string{"accounts", "list", "--app-id", "a", "--type", " "}, accountsListCmd},
		{"executions list --state unknown", []string{"automations", "executions", "list", "--state", "bogus"}, automationsExecutionsListCmd},
		{"users list --status lookalike", []string{"users", "list", "--status", "enabıed"}, usersListCmd},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCmds(t, rootCmd, tc.cmd)
			t.Setenv("C1I_URL", "https://example.invalid")
			err := runRootWithArgs(t, tc.args)
			if got := exitCode(err); got != exitUsage {
				t.Errorf("exit %d (%v), want %d", got, err, exitUsage)
			}
		})
	}
}
