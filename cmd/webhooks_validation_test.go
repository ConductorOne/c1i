package cmd

import (
	"strings"
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

// Display-name length is 3-100 runes on both create and update; the server
// enforces it only on create.
func TestWebhooksDisplayNameLength(t *testing.T) {
	withCatalogDryRun(t, false)
	cases := []struct {
		name string
		val  string
		ok   bool
	}{
		{"two runes", "ab", false},
		{"three runes", "abc", true},
		{"100 runes", strings.Repeat("a", 100), true},
		{"101 runes", strings.Repeat("a", 101), false},
		{"100 multibyte runes (300 bytes)", strings.Repeat("日", 100), true},
		{"two multibyte runes (6 bytes)", "日本", false},
	}
	for _, c := range cases {
		for _, tc := range createAndUpdate("display-name", c.val) {
			t.Run(tc.name+" "+c.name, func(t *testing.T) {
				srv, reqs := webhooksServer(t, webhookEnvelope)
				_, err := runWebhookCmd(t, srv, tc.cmd, tc.args, tc.flags)
				if c.ok {
					if err != nil || len(*reqs) != 1 {
						t.Errorf("err = %v, requests = %d; want success and 1 request", err, len(*reqs))
					}
					return
				}
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
