package cmd

import (
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestOwnerOldShapesExitUsage pins that the previous owner shapes (user as
// the positional, --app-id/--entitlement-id as the address) fail with exit 2
// and send nothing, rather than addressing the wrong id.
func TestOwnerOldShapesExitUsage(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"apps add-owner old", []string{"apps", "add-owner", "u1", "--app-id", "app-1"}},
		{"apps remove-owner old", []string{"apps", "remove-owner", "u1", "--app-id", "app-1"}},
		{"entitlements add-owner old", []string{"entitlements", "add-owner", "u1", "--app-id", "app-1", "--entitlement-id", "e-1"}},
		{"entitlements remove-owner old", []string{"entitlements", "remove-owner", "u1", "--app-id", "app-1", "--entitlement-id", "e-1"}},
		{"apps add-owner no --user-id", []string{"apps", "add-owner", "app-1"}},
		{"apps remove-owner no --user-id", []string{"apps", "remove-owner", "app-1"}},
		{"entitlements add-owner no --user-id", []string{"entitlements", "add-owner", "e-1", "--app-id", "app-1"}},
		{"entitlements remove-owner no --user-id", []string{"entitlements", "remove-owner", "e-1", "--app-id", "app-1"}},
		{"apps add-owner blank --user-id", []string{"apps", "add-owner", "app-1", "--user-id", ""}},
		{"apps remove-owner blank --user-id", []string{"apps", "remove-owner", "app-1", "--user-id", ""}},
		{"entitlements add-owner blank --user-id", []string{"entitlements", "add-owner", "e-1", "--app-id", "app-1", "--user-id", ""}},
		{"entitlements remove-owner blank --user-id", []string{"entitlements", "remove-owner", "e-1", "--app-id", "app-1", "--user-id", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, func(*http.Request, int) mutReply { return mutReply{body: `{}`} })
			r := runMutationCLI(t, s.srv, 0, nil, append(tc.args, "--error-format", "json")...)
			if r.exit != exitUsage {
				t.Errorf("exit = %d, want %d (%v)", r.exit, exitUsage, r.err)
			}
			if n := len(s.requests()); n != 0 {
				t.Errorf("sent %d request(s); a usage error must send nothing", n)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want nothing", r.stdout)
			}
		})
	}
}

// TestOwnerCommandsSharePositional enforces CLAUDE.md's membership-verb rule:
// in every group with an `owners` command, every *owner/*owners verb takes
// exactly one positional, the same one as `owners`, and the user as --user-id.
func TestOwnerCommandsSharePositional(t *testing.T) {
	checked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		var owners *cobra.Command
		for _, sub := range c.Commands() {
			if sub.Name() == "owners" {
				owners = sub
			}
		}
		if owners != nil {
			want := positionalPlaceholders(owners.Use)
			if len(want) != 1 || strings.Contains(want[0], "user") {
				t.Errorf("%s: owners Use %q must take the collection owner's id as its one positional", c.CommandPath(), owners.Use)
			}
			for _, sub := range c.Commands() {
				name := sub.Name()
				if name == "owners" || (!strings.HasSuffix(name, "owner") && !strings.HasSuffix(name, "owners")) {
					continue
				}
				checked++
				got := positionalPlaceholders(sub.Use)
				if strings.Join(got, " ") != strings.Join(want, " ") {
					t.Errorf("%s: Use %q has positional %v, want %v (same as owners)", sub.CommandPath(), sub.Use, got, want)
				}
				if sub.Args == nil || sub.Args(sub, []string{"x"}) != nil || sub.Args(sub, []string{"x", "y"}) == nil {
					t.Errorf("%s: Args must accept exactly one positional", sub.CommandPath())
				}
				if sub.Flags().Lookup("user-id") == nil {
					t.Errorf("%s: the member must be a --user-id flag", sub.CommandPath())
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	if checked < 6 {
		t.Errorf("checked %d owner verbs, want at least 6 (apps and entitlements)", checked)
	}
}

// positionalPlaceholders returns the <x>/[<x>] tokens of a Use string.
func positionalPlaceholders(use string) []string {
	var out []string
	for _, f := range strings.Fields(use)[1:] {
		if strings.HasPrefix(f, "<") || strings.HasPrefix(f, "[<") {
			out = append(out, f)
		}
	}
	return out
}
