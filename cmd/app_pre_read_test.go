package cmd

import (
	"net/http"
	"sort"
	"strings"
	"testing"
)

// appPreReadCommands are the mutationTable commands that read their app
// first (requireLiveApp via liveAppPreRead).
var appPreReadCommands = []string{
	"apps add-owner", "apps remove-owner", "apps set-owners", "apps update",
	"apps resources add-owner", "apps resources remove-owner", "apps resources set-owners", "apps resources update",
	"apps routing-rules create", "apps routing-rules reorder", "apps routing-rules update",
	"connectors create", "mcp servers register",
	"xaa access-profiles create", "xaa resource-servers create", "xaa scopes create",
}

// isAppGET reports a GET of one app, /api/v1/apps/<id>.
func isAppGET(r *http.Request) bool {
	id, ok := strings.CutPrefix(r.URL.EscapedPath(), "/api/v1/apps/")
	return ok && r.Method == http.MethodGet && id != "" && !strings.Contains(id, "/")
}

// appStateRoute answers the app GET for state and everything else as
// tableRoute(body) does.
func appStateRoute(state, body string) func(*http.Request, int) mutReply {
	next := tableRoute(body)
	return func(r *http.Request, n int) mutReply {
		if !isAppGET(r) {
			return next(r, n)
		}
		switch state {
		case "unknown":
			return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
		case "deleted":
			return mutReply{body: `{"app":{"id":"app-1","deletedAt":"2026-01-01T00:00:00Z"}}`}
		case "no app object":
			return mutReply{body: `{}`}
		case "empty deletedAt":
			return mutReply{body: `{"app":{"id":"app-1","deletedAt":""}}`}
		}
		return mutReply{body: `{"app":{"id":"app-1","deletedAt":null}}`}
	}
}

// Every mutationTable command that refuses a deleted app is listed, and no
// listed command lost the read.
func TestAppPreReadCommandsAreListed(t *testing.T) {
	got := map[string]bool{}
	for _, row := range mutationTable {
		t.Run(row.cmd, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, appStateRoute("deleted", row.body))
			r := runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
			if r.exit == exitUsage && strings.Contains(r.stderr, "the app is deleted") {
				got[row.cmd] = true
			}
		})
	}
	var have []string
	for c := range got {
		have = append(have, c)
	}
	sort.Strings(have)
	want := append([]string{}, appPreReadCommands...)
	sort.Strings(want)
	if strings.Join(have, "|") != strings.Join(want, "|") {
		t.Errorf("commands refusing a deleted app:\n got %q\nwant %q", have, want)
	}
}

// The app pre-read: live and "deletedAt":"" pass, unknown exits 4, deleted
// 2, a 200 without an app object 6, before any write, dry-run included.
func TestAppPreReadCommands(t *testing.T) {
	type tcase struct {
		name string
		args []string
		body string
	}
	var cases []tcase
	listed := map[string]bool{}
	for _, c := range appPreReadCommands {
		listed[c] = true
	}
	for _, row := range mutationTable {
		if listed[row.cmd] {
			cases = append(cases, tcase{row.cmd, row.argv(t), row.body})
		}
	}
	ent := []string{"--owner-entitlement-id", "e-9"}
	cases = append(cases,
		tcase{"apps add-owner entitlement", cat([]string{"apps", "add-owner", "app-1"}, ent), `{}`},
		tcase{"apps remove-owner entitlement", cat([]string{"apps", "remove-owner", "app-1"}, ent), `{}`},
		tcase{"apps set-owners entitlement", cat([]string{"apps", "set-owners", "app-1", "--no-user-owners"}, ent), `{}`},
		tcase{"apps resources add-owner entitlement", cat([]string{"apps", "resources", "add-owner", "r-1"}, resScope, ent), `{}`},
		tcase{"apps resources remove-owner entitlement", cat([]string{"apps", "resources", "remove-owner", "r-1"}, resScope, ent), `{}`},
		tcase{"apps resources set-owners entitlement", cat([]string{"apps", "resources", "set-owners", "r-1", "--no-user-owners"}, resScope, ent), `{}`},
	)
	want := map[string]int{"live": 0, "empty deletedAt": 0, "unknown": exitNotFound, "deleted": exitUsage, "no app object": exitServer}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			for state, exit := range want {
				t.Run(tc.name+" "+state+map[bool]string{true: " dry-run"}[dry], func(t *testing.T) {
					t.Setenv("C1I_URL", mutTestURL)
					clearViperOverrides(t, "dry_run")
					if dry {
						withDryRun(t)
					}
					s := newMutServer(t, appStateRoute(state, tc.body))
					r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
					if r.exit != exit {
						t.Fatalf("exit %d (want %d): %s", r.exit, exit, r.stderr)
					}
					sent := writes(s)
					if exit != 0 || dry {
						if len(sent) != 0 {
							t.Errorf("sent %v", sent)
						}
					} else if len(sent) == 0 {
						t.Error("a live app sent no write")
					}
				})
			}
		}
	}
}

// apps usage-controls get reads the app with the same helper.
func TestUsageControlsGetAppPreRead(t *testing.T) {
	want := map[string]int{"live": 0, "empty deletedAt": 0, "unknown": exitNotFound, "deleted": exitUsage, "no app object": exitServer}
	for state, exit := range want {
		t.Run(state, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, appStateRoute(state, `{}`))
			r := runMutationCLI(t, s.srv, 0, nil, "apps", "usage-controls", "get", "app-1")
			if r.exit != exit {
				t.Fatalf("exit %d (want %d): %s", r.exit, exit, r.stderr)
			}
		})
	}
}
