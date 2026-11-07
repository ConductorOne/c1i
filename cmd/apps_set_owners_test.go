package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestBuildSetOwnersBody pins the exact wire JSON for one and many owners: the
// key is "userIds" (not "user_ids"), unwrapped, order preserved.
func TestBuildSetOwnersBody(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"single", []string{"user-1111111111111111111111"}, `{"userIds":["user-1111111111111111111111"]}`},
		{"multiple", []string{"a", "b"}, `{"userIds":["a","b"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(buildSetOwnersBody(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.want {
				t.Errorf("body = %s, want %s", b, tc.want)
			}
		})
	}
}

// TestAppsSetOwnersPrintsJSON pins the output contract every owner write
// shares: stdout is exactly the API's JSON response, and --wait's progress and
// success lines go to stderr so a caller can pipe stdout to jq.
func TestAppsSetOwnersPrintsJSON(t *testing.T) {
	t.Run("without --wait", func(t *testing.T) {
		_, seen := entOwnerServer(t, `{"userIds":[]}`)
		out, errOut, err := runEntOwnerCmd(t, appsSetOwnersCmd,
			map[string][]string{"user-id": {"u1", "u2"}}, "app/1")
		if err != nil {
			t.Fatalf("RunE: %v", err)
		}
		if out != "{}\n" {
			t.Errorf("stdout = %q, want exactly the API's JSON response", out)
		}
		if errOut != "" {
			t.Errorf("stderr = %q, want nothing", errOut)
		}
		w := withoutUserLookups(seen())
		want := entOwnerRequest{"PUT", "/api/v1/apps/app%2F1/owners", `{"userIds":["u1","u2"]}`}
		if len(w) != 1 || w[0] != want {
			t.Errorf("writes = %+v, want [%+v]", w, want)
		}
	})
	t.Run("with --wait", func(t *testing.T) {
		_, seen := entOwnerServer(t, `{"userIds":["u1"]}`)
		out, errOut, err := runEntOwnerCmd(t, appsSetOwnersCmd,
			map[string][]string{"user-id": {"u1"}, "wait": {"true"}, "wait-timeout": {"2s"}}, "app1")
		if err != nil {
			t.Fatalf("RunE: %v", err)
		}
		if out != "{}\n" {
			t.Errorf("stdout = %q, want only the write's JSON response", out)
		}
		if !strings.HasPrefix(errOut, "Owners provisioned on app app1 after ") {
			t.Errorf("stderr missing the success line:\n%s", errOut)
		}
		w := withoutUserLookups(seen())
		if len(w) != 2 || w[1].method != "GET" || w[1].rawPath != "/api/v1/apps/app1/ownerids" {
			t.Errorf("requests = %+v, want the PUT then GET .../apps/app1/ownerids", w)
		}
	})
}

// TestAppsSetOwnersFailsOnOutputWriteError: the owners were set, but a lost
// response must not exit 0.
func TestAppsSetOwnersFailsOnOutputWriteError(t *testing.T) {
	entOwnerServer(t, `{"userIds":["u1"]}`)
	resetCmdFlags(t, appsSetOwnersCmd)
	if err := appsSetOwnersCmd.Flags().Set("user-id", "u1"); err != nil {
		t.Fatal(err)
	}
	appsSetOwnersCmd.SetOut(failingWriter{})
	appsSetOwnersCmd.SetErr(io.Discard)
	appsSetOwnersCmd.SetContext(context.Background())
	assertMutationWriteFailure(t, appsSetOwnersCmd.RunE(appsSetOwnersCmd, []string{"app1"}), "PUT /api/v1/apps/app1/owners")
}

// TestAppsSetOwnersChecksUsersExist: the apps owners PUT accepts an unknown
// user id and stores it as an owner no user matches. Ids are deduped like
// entitlements set-owners', whose PUT 500s on a repeat.
func TestAppsSetOwnersChecksUsersExist(t *testing.T) {
	t.Run("unknown user is exit 2 naming it, and no PUT is sent", func(t *testing.T) {
		_, seen := entOwnerServerUsers(t, []string{`{"userIds":[]}`}, map[string]int{"ghost": http.StatusNotFound})
		_, _, err := runEntOwnerCmd(t, appsSetOwnersCmd,
			map[string][]string{"user-id": {"u1", "ghost"}}, "app1")
		var ue *usageError
		if !errors.As(err, &ue) {
			t.Fatalf("error = %v, want a *usageError (exit 2)", err)
		}
		if !strings.Contains(err.Error(), `"ghost"`) {
			t.Errorf("error %q does not name the unknown id", err)
		}
		if w := withoutUserLookups(seen()); len(w) != 0 {
			t.Errorf("sent %+v after a failed lookup; want nothing", w)
		}
	})
	t.Run("a repeated id is looked up and sent once", func(t *testing.T) {
		_, seen := entOwnerServer(t, `{"userIds":[]}`)
		if _, _, err := runEntOwnerCmd(t, appsSetOwnersCmd,
			map[string][]string{"user-id": {"u1", "u2", "u1"}}, "app1"); err != nil {
			t.Fatalf("RunE: %v", err)
		}
		if got := userLookups(seen()); strings.Join(got, ",") != "u1,u2" {
			t.Errorf("looked up %v, want [u1 u2]", got)
		}
		w := withoutUserLookups(seen())
		if len(w) != 1 || w[0].body != `{"userIds":["u1","u2"]}` {
			t.Errorf("writes = %+v, want one PUT of [u1 u2]", w)
		}
	})
	t.Run("dry-run sends a repeated id once", func(t *testing.T) {
		withDryRun(t)
		entOwnerServer(t, `{"userIds":[]}`)
		out, _, err := runEntOwnerCmd(t, appsSetOwnersCmd,
			map[string][]string{"user-id": {"u1", "u2", "u1"}}, "app1")
		if err != nil {
			t.Fatalf("RunE: %v", err)
		}
		if want := `"body": {
        "userIds": [
          "u1",
          "u2"
        ]
      }`; !strings.Contains(out, want) {
			t.Errorf("dry-run preview =\n%s\nwant\n%s", out, want)
		}
	})
	t.Run("dry-run does no lookups", func(t *testing.T) {
		withDryRun(t)
		_, seen := entOwnerServerUsers(t, []string{`{"userIds":[]}`}, map[string]int{"ghost": http.StatusNotFound})
		if _, _, err := runEntOwnerCmd(t, appsSetOwnersCmd,
			map[string][]string{"user-id": {"ghost"}}, "app1"); err != nil {
			t.Fatalf("RunE: %v", err)
		}
		if n := len(seen()); n != 0 {
			t.Errorf("dry-run sent %d request(s)", n)
		}
	})
}

// TestAppsSetOwnersWaitsForEquality: set-owners replaces the list, so a
// removal-only set must keep polling while the removed id still shows.
func TestAppsSetOwnersWaitsForEquality(t *testing.T) {
	orig := ownerWaitInterval
	ownerWaitInterval = 5 * time.Millisecond
	t.Cleanup(func() { ownerWaitInterval = orig })

	_, seen := entOwnerServerUsers(t,
		[]string{`{"userIds":["u1","u2"]}`, `{"userIds":["u2","u1"]}`, `{"userIds":["u1"]}`}, nil)
	if _, _, err := runEntOwnerCmd(t, appsSetOwnersCmd,
		map[string][]string{"user-id": {"u1"}, "wait": {"true"}, "wait-timeout": {"5s"}}, "app1"); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	polls := 0
	for _, r := range seen() {
		if strings.HasSuffix(r.rawPath, "/ownerids") {
			polls++
		}
	}
	if polls != 3 {
		t.Errorf("polled ownerids %d time(s), want 3", polls)
	}
}
