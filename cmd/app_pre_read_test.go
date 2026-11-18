package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// The app pre-read shared by these commands refuses an unknown app (4) and a
// soft-deleted one (2) before any write, dry-run included.
func TestAppPreReadCommands(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"mcp servers register", []string{"mcp", "servers", "register", "--app-id", "app-1", "--type", "external", "--display-name", "z", "--server-url", "https://m.example", "--auth", "none"}},
		{"connectors create", []string{"connectors", "create", "--app-id", "app-1", "--catalog-id", "cat-1"}},
		{"apps set-owners", []string{"apps", "set-owners", "app-1", "--user-id", "u1"}},
		{"apps add-owner", []string{"apps", "add-owner", "app-1", "--user-id", "u1"}},
		{"apps remove-owner", []string{"apps", "remove-owner", "app-1", "--user-id", "u1"}},
		{"apps resources update", []string{"apps", "resources", "update", "r-1", "--app-id", "app-1", "--resource-type-id", "rt-1", "--description", "d"}},
		{"apps resources set-owners", []string{"apps", "resources", "set-owners", "r-1", "--app-id", "app-1", "--resource-type-id", "rt-1", "--user-id", "u1"}},
		{"apps resources add-owner", []string{"apps", "resources", "add-owner", "r-1", "--app-id", "app-1", "--resource-type-id", "rt-1", "--user-id", "u1"}},
		{"apps resources remove-owner", []string{"apps", "resources", "remove-owner", "r-1", "--app-id", "app-1", "--resource-type-id", "rt-1", "--user-id", "u1"}},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			for _, state := range []string{"deleted", "unknown"} {
				t.Run(tc.name+" "+state, func(t *testing.T) {
					t.Setenv("C1I_URL", mutTestURL)
					clearViperOverrides(t, "dry_run")
					if dry {
						withDryRun(t)
					}
					s := newMutServer(t, func(r *http.Request, _ int) mutReply {
						switch {
						case r.Method == http.MethodGet && r.URL.Path == "/api/v1/apps/app-1":
							if state == "unknown" {
								return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
							}
							return mutReply{body: `{"app":{"id":"app-1","deletedAt":"2026-01-01T00:00:00Z"}}`}
						case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
							return mutReply{body: `{"user":{"id":"u1"}}`}
						}
						return mutReply{body: `{}`}
					})
					args := append([]string{}, tc.args...)
					if dry {
						args = append(args, "--dry-run")
					}
					r := runMutationCLI(t, s.srv, 0, nil, args...)
					want := exitUsage
					if state == "unknown" {
						want = exitNotFound
					}
					if r.exit != want {
						t.Fatalf("exit %d (want %d): %s", r.exit, want, r.stderr)
					}
					if len(writes(s)) != 0 {
						t.Errorf("sent %v", writes(s))
					}
				})
			}
		}
	}
}
