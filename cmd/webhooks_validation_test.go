package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

type webhookFlagCase struct {
	name  string
	cmd   *cobra.Command
	args  []string
	flags map[string]string
}

// createAndUpdate builds the create and update runs that pass flag=val.
func createAndUpdate(flag, val string) []webhookFlagCase {
	create := map[string]string{"display-name": "zz-webhook-a", "destination-url": "https://hooks.example.com/x"}
	create[flag] = val
	return []webhookFlagCase{
		{"create", webhooksCreateCmd, nil, create},
		{"update", webhooksUpdateCmd, []string{"wh-1"}, map[string]string{flag: val}},
	}
}

// A URL with no hostname (port only) or an IPv6 literal host is refused: the
// server stores the first as "https:///a" and strips the second's brackets.
func TestWebhooksURLRefusesHostlessAndIPv6(t *testing.T) {
	withCatalogDryRun(t, false)
	for _, u := range []string{
		"https://:443/a",
		"https://[2001:db8::1]/a",
		"https://[2001:db8::1]:8443/a",
		"https://[::1]/a",
	} {
		for _, tc := range createAndUpdate("destination-url", u) {
			t.Run(tc.name+" "+u, func(t *testing.T) {
				srv, reqs := webhooksServer(t, webhookEnvelope)
				_, err := runWebhookCmd(t, srv, tc.cmd, tc.args, tc.flags)
				if code := exitCode(err); code != exitUsage {
					t.Errorf("exit = %d, want %d (err: %v)", code, exitUsage, err)
				}
				if len(*reqs) != 0 {
					t.Errorf("sent %d request(s)", len(*reqs))
				}
			})
		}
	}
}

// IPv4 literals and ordinary hosts with a port still pass.
func TestWebhooksURLAcceptsHostWithPort(t *testing.T) {
	withCatalogDryRun(t, false)
	for _, u := range []string{"https://hooks.example.com:8443/a", "https://192.0.2.10/a"} {
		for _, tc := range createAndUpdate("destination-url", u) {
			t.Run(tc.name+" "+u, func(t *testing.T) {
				srv, reqs := webhooksServer(t, webhookEnvelope)
				if _, err := runWebhookCmd(t, srv, tc.cmd, tc.args, tc.flags); err != nil {
					t.Fatalf("RunE: %v", err)
				}
				if len(*reqs) != 1 {
					t.Errorf("requests = %d, want 1", len(*reqs))
				}
			})
		}
	}
}
