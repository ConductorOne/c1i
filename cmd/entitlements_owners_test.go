package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// entOwnerRequest is one request an entitlement-owner fake server saw.
type entOwnerRequest struct {
	method  string
	rawPath string // escaped form, so a test can pin %2F rather than a decoded "/"
	body    string
}

// entOwnerServer records every request and answers ownerids with ownerIDs,
// every user lookup with a user, and every write with `{}`.
func entOwnerServer(t *testing.T, ownerIDs string) (*httptest.Server, func() []entOwnerRequest) {
	t.Helper()
	return entOwnerServerUsers(t, []string{ownerIDs}, nil)
}

// entOwnerServerUsers is entOwnerServer with GET /api/v1/users/{id} answering
// userStatus[id] (200 when absent). The i-th ownerids poll gets ownerIDs[i],
// repeating the last one thereafter.
func entOwnerServerUsers(t *testing.T, ownerIDs []string, userStatus map[string]int) (*httptest.Server, func() []entOwnerRequest) {
	t.Helper()
	var mu sync.Mutex
	polls := 0
	var seen []entOwnerRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, entOwnerRequest{r.Method, r.URL.EscapedPath(), string(b)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if id, ok := strings.CutPrefix(r.URL.Path, "/api/v1/users/"); ok && r.Method == http.MethodGet {
			if code, set := userStatus[id]; set && code != http.StatusOK {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"code":5,"message":"not found"}`))
				return
			}
			_, _ = w.Write([]byte(`{"user":{"id":"` + id + `"}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/ownerids") {
			mu.Lock()
			i := min(polls, len(ownerIDs)-1)
			polls++
			mu.Unlock()
			_, _ = w.Write([]byte(ownerIDs[i]))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	orig := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")
	return srv, func() []entOwnerRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]entOwnerRequest(nil), seen...)
	}
}

// withoutUserLookups drops the set-owners user-existence GETs, leaving the
// write and any wait polls.
func withoutUserLookups(reqs []entOwnerRequest) []entOwnerRequest {
	var out []entOwnerRequest
	for _, r := range reqs {
		if !strings.HasPrefix(r.rawPath, "/api/v1/users/") {
			out = append(out, r)
		}
	}
	return out
}

func userLookups(reqs []entOwnerRequest) []string {
	var out []string
	for _, r := range reqs {
		if id, ok := strings.CutPrefix(r.rawPath, "/api/v1/users/"); ok {
			out = append(out, id)
		}
	}
	return out
}

func runEntOwnerCmd(t *testing.T, cmd *cobra.Command, flags map[string][]string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	resetCmdFlags(t, cmd)
	for name, vals := range flags {
		for _, v := range vals {
			if err := cmd.Flags().Set(name, v); err != nil {
				t.Fatalf("--%s=%q: %v", name, v, err)
			}
		}
	}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())
	err = cmd.RunE(cmd, args)
	return out.String(), errOut.String(), err
}

// TestEntitlementOwnerWritesSendExactRequests pins method, escaped path and
// body for each write. The user id goes in add-owner's body (unlike "apps
// add-owner", which puts it in the path) but in remove-owner's path.
func TestEntitlementOwnerWritesSendExactRequests(t *testing.T) {
	scope := map[string][]string{"app-id": {"app/1"}, "entitlement-id": {"ent 1"}}
	const base = "/api/v1/apps/app%2F1/entitlements/ent%201/owners"
	cases := []struct {
		name  string
		cmd   *cobra.Command
		flags map[string][]string
		args  []string
		want  entOwnerRequest
	}{
		{
			name:  "add-owner",
			cmd:   entitlementsAddOwnerCmd,
			flags: scope,
			args:  []string{"user/1"},
			want:  entOwnerRequest{"POST", base, `{"userId":"user/1"}`},
		},
		{
			name:  "remove-owner",
			cmd:   entitlementsRemoveOwnerCmd,
			flags: scope,
			args:  []string{"user/1"},
			want:  entOwnerRequest{"DELETE", base + "/user%2F1", ""},
		},
		{
			name:  "set-owners",
			cmd:   entitlementsSetOwnersCmd,
			flags: map[string][]string{"app-id": {"app/1"}, "user-id": {"u1", "u2"}},
			args:  []string{"ent 1"},
			want:  entOwnerRequest{"PUT", base, `{"userIds":["u1","u2"]}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, seen := entOwnerServer(t, `{"userIds":[]}`)
			out, _, err := runEntOwnerCmd(t, tc.cmd, tc.flags, tc.args...)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if strings.TrimSpace(out) != "{}" {
				t.Errorf("stdout = %q, want the API's JSON response {}", out)
			}
			got := withoutUserLookups(seen())
			if len(got) != 1 {
				t.Fatalf("sent %d requests, want 1: %+v", len(got), got)
			}
			if got[0] != tc.want {
				t.Errorf("request = %+v, want %+v", got[0], tc.want)
			}
		})
	}
}

// TestEntitlementOwnerWritesRequireScopeFlags proves an empty parent id is a
// usage error before anything is sent: entOwnerServer would record a request.
func TestEntitlementOwnerWritesRequireScopeFlags(t *testing.T) {
	cases := []struct {
		name    string
		cmd     *cobra.Command
		flags   map[string][]string
		args    []string
		mention string
	}{
		{"owners: empty --app-id", entitlementsOwnersCmd, map[string][]string{"app-id": {""}}, []string{"ent1"}, "--app-id"},
		{"add-owner: empty --app-id", entitlementsAddOwnerCmd, map[string][]string{"app-id": {""}, "entitlement-id": {"ent1"}}, []string{"u1"}, "--app-id"},
		{"add-owner: empty --entitlement-id", entitlementsAddOwnerCmd, map[string][]string{"app-id": {"app1"}, "entitlement-id": {""}}, []string{"u1"}, "--entitlement-id"},
		{"remove-owner: empty --app-id", entitlementsRemoveOwnerCmd, map[string][]string{"app-id": {""}, "entitlement-id": {"ent1"}}, []string{"u1"}, "--app-id"},
		{"remove-owner: empty --entitlement-id", entitlementsRemoveOwnerCmd, map[string][]string{"app-id": {"app1"}, "entitlement-id": {""}}, []string{"u1"}, "--entitlement-id"},
		{"set-owners: empty --app-id", entitlementsSetOwnersCmd, map[string][]string{"app-id": {""}, "user-id": {"u1"}}, []string{"ent1"}, "--app-id"},
		{"set-owners: no --user-id", entitlementsSetOwnersCmd, map[string][]string{"app-id": {"app1"}}, []string{"ent1"}, "--user-id"},
		{"set-owners: empty --user-id beside a real one", entitlementsSetOwnersCmd, map[string][]string{"app-id": {"app1"}, "user-id": {"", "u1"}}, []string{"ent1"}, "--user-id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, seen := entOwnerServer(t, `{"userIds":[]}`)
			_, _, err := runEntOwnerCmd(t, tc.cmd, tc.flags, tc.args...)
			var ue *usageError
			if !errors.As(err, &ue) {
				t.Fatalf("error = %v, want a *usageError (exit 2)", err)
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q does not name %s", err, tc.mention)
			}
			if n := len(seen()); n != 0 {
				t.Errorf("sent %d request(s); a usage error must send nothing", n)
			}
		})
	}
}

// TestEntitlementOwnerWritesDryRun pins the preview for each write and that
// --wait never polls under --dry-run.
func TestEntitlementOwnerWritesDryRun(t *testing.T) {
	withDryRun(t)
	cases := []struct {
		name  string
		cmd   *cobra.Command
		flags map[string][]string
		args  []string
		want  []string
		not   string
	}{
		{"add-owner", entitlementsAddOwnerCmd, map[string][]string{"app-id": {"app1"}, "entitlement-id": {"ent1"}, "wait": {"true"}, "wait-timeout": {"2s"}}, []string{"u1"},
			[]string{"[dry-run] POST /api/v1/apps/app1/entitlements/ent1/owners\n", `"userId": "u1"`}, ""},
		{"remove-owner", entitlementsRemoveOwnerCmd, map[string][]string{"app-id": {"app1"}, "entitlement-id": {"ent1"}}, []string{"u1"},
			[]string{"[dry-run] DELETE /api/v1/apps/app1/entitlements/ent1/owners/u1\n"}, "{"},
		{"set-owners", entitlementsSetOwnersCmd, map[string][]string{"app-id": {"app1"}, "user-id": {"u1"}, "wait": {"true"}, "wait-timeout": {"2s"}}, []string{"ent1"},
			[]string{"[dry-run] PUT /api/v1/apps/app1/entitlements/ent1/owners\n", `"userIds": [`}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, seen := entOwnerServer(t, `{"userIds":[]}`)
			out, _, err := runEntOwnerCmd(t, tc.cmd, tc.flags, tc.args...)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("dry-run output missing %q:\n%s", w, out)
				}
			}
			if tc.not != "" && strings.Contains(out, tc.not) {
				t.Errorf("dry-run output contains %q, want none:\n%s", tc.not, out)
			}
			if n := len(seen()); n != 0 {
				t.Errorf("dry-run sent %d request(s)", n)
			}
		})
	}
}

// TestEntitlementOwnerWritesWaitPollsEntitlementOwnerIDs drives --wait end to
// end: after the write, the command polls the entitlement's ownerids (not the
// app's) and reports convergence on stderr, leaving stdout pure JSON.
func TestEntitlementOwnerWritesWaitPollsEntitlementOwnerIDs(t *testing.T) {
	cases := []struct {
		name  string
		cmd   *cobra.Command
		flags map[string][]string
		args  []string
	}{
		{"add-owner", entitlementsAddOwnerCmd, map[string][]string{"app-id": {"app1"}, "entitlement-id": {"ent1"}, "wait": {"true"}, "wait-timeout": {"2s"}}, []string{"u1"}},
		{"set-owners", entitlementsSetOwnersCmd, map[string][]string{"app-id": {"app1"}, "user-id": {"u1"}, "wait": {"true"}, "wait-timeout": {"2s"}}, []string{"ent1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, seen := entOwnerServer(t, `{"userIds":["u1"]}`)
			out, errOut, err := runEntOwnerCmd(t, tc.cmd, tc.flags, tc.args...)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if strings.TrimSpace(out) != "{}" {
				t.Errorf("stdout = %q, want only the write's JSON response", out)
			}
			got := withoutUserLookups(seen())
			if len(got) != 2 || got[1].method != "GET" || got[1].rawPath != "/api/v1/apps/app1/entitlements/ent1/ownerids" {
				t.Fatalf("requests = %+v, want the write then GET .../entitlements/ent1/ownerids", got)
			}
			if !strings.Contains(errOut, "Owners provisioned on entitlement ent1 of app app1 after ") {
				t.Errorf("stderr missing the success line:\n%s", errOut)
			}
		})
	}
}

// TestEntitlementOwnersWaitTimesOut pins the recheck command a timeout hands
// the operator.
func TestEntitlementOwnersWaitTimesOut(t *testing.T) {
	srv := ownerIDsServer(t, "/api/v1/apps/app1/entitlements/ent1/ownerids", []string{`{"userIds":[]}`})
	c := client.NewForTesting(srv.URL, srv.Client())
	op := entitlementOwnersWaitOp(c, "app1", "ent1", untilPresent([]string{"u1"}), 30*time.Millisecond)
	op.Interval = 5 * time.Millisecond
	cmd, _ := newWaitTestCmd(context.Background())
	_, err := runWait(cmd, op)
	if err == nil {
		t.Fatal("wait returned nil, want a timeout error")
	}
	if !strings.HasSuffix(err.Error(), "check again later with: c1i entitlements owners ent1 --app-id app1") {
		t.Errorf("timeout error = %q, want it to suggest entitlements owners", err)
	}
}

// TestEntitlementsSetOwnersChecksUsersExist covers the client-side existence
// check: the API accepts and silently drops an unknown user id, which can
// leave the entitlement with no owners, so set-owners looks each distinct id
// up first.
func TestEntitlementsSetOwnersChecksUsersExist(t *testing.T) {
	t.Run("unknown user is exit 2 naming it, and no PUT is sent", func(t *testing.T) {
		_, seen := entOwnerServerUsers(t, []string{`{"userIds":[]}`}, map[string]int{"ghost": http.StatusNotFound})
		_, _, err := runEntOwnerCmd(t, entitlementsSetOwnersCmd,
			map[string][]string{"app-id": {"app1"}, "user-id": {"u1", "ghost"}}, "ent1")
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
	t.Run("each distinct id is looked up once", func(t *testing.T) {
		_, seen := entOwnerServer(t, `{"userIds":[]}`)
		if _, _, err := runEntOwnerCmd(t, entitlementsSetOwnersCmd,
			map[string][]string{"app-id": {"app1"}, "user-id": {"u1", "u2", "u1"}}, "ent1"); err != nil {
			t.Fatalf("RunE: %v", err)
		}
		got := userLookups(seen())
		if strings.Join(got, ",") != "u1,u2" {
			t.Errorf("looked up %v, want [u1 u2]", got)
		}
		// The API 500s on a repeated id in the PUT body, so it is deduped too.
		w := withoutUserLookups(seen())
		if len(w) != 1 || w[0].body != `{"userIds":["u1","u2"]}` {
			t.Errorf("writes = %+v, want one PUT of [u1 u2]", w)
		}
	})
	t.Run("lookup ids are path-escaped", func(t *testing.T) {
		_, seen := entOwnerServer(t, `{"userIds":[]}`)
		if _, _, err := runEntOwnerCmd(t, entitlementsSetOwnersCmd,
			map[string][]string{"app-id": {"app1"}, "user-id": {"u/1"}}, "ent1"); err != nil {
			t.Fatalf("RunE: %v", err)
		}
		if got := userLookups(seen()); len(got) != 1 || got[0] != "u%2F1" {
			t.Errorf("looked up %v, want [u%%2F1]", got)
		}
	})
	t.Run("dry-run does no lookups", func(t *testing.T) {
		withDryRun(t)
		_, seen := entOwnerServerUsers(t, []string{`{"userIds":[]}`}, map[string]int{"ghost": http.StatusNotFound})
		if _, _, err := runEntOwnerCmd(t, entitlementsSetOwnersCmd,
			map[string][]string{"app-id": {"app1"}, "user-id": {"ghost"}}, "ent1"); err != nil {
			t.Fatalf("RunE: %v", err)
		}
		if n := len(seen()); n != 0 {
			t.Errorf("dry-run sent %d request(s)", n)
		}
	})
	t.Run("a 403 lookup keeps its auth classification", func(t *testing.T) {
		_, seen := entOwnerServerUsers(t, []string{`{"userIds":[]}`}, map[string]int{"u1": http.StatusForbidden})
		_, _, err := runEntOwnerCmd(t, entitlementsSetOwnersCmd,
			map[string][]string{"app-id": {"app1"}, "user-id": {"u1"}}, "ent1")
		if code := exitCode(err); code != exitAuth {
			t.Errorf("exit code = %d, want %d (auth); err = %v", code, exitAuth, err)
		}
		if w := withoutUserLookups(seen()); len(w) != 0 {
			t.Errorf("sent %+v after a failed lookup; want nothing", w)
		}
	})
}

// TestEntitlementOwnersWaitPredicates drives --wait end to end against an
// ownerids sequence. set-owners replaces the list, so it must wait until the
// owner set EQUALS the request -- a removal-only set must keep polling while
// the removed id still shows. add-owner only waits for presence.
func TestEntitlementOwnersWaitPredicates(t *testing.T) {
	orig := entitlementOwnerWaitInterval
	entitlementOwnerWaitInterval = 5 * time.Millisecond
	t.Cleanup(func() { entitlementOwnerWaitInterval = orig })

	seq := []string{`{"userIds":["u1","u2"]}`, `{"userIds":["u2","u1"]}`, `{"userIds":["u1"]}`}
	cases := []struct {
		name      string
		cmd       *cobra.Command
		flags     map[string][]string
		args      []string
		wantPolls int
	}{
		{"set-owners removal-only waits for the removal", entitlementsSetOwnersCmd,
			map[string][]string{"app-id": {"app1"}, "user-id": {"u1"}, "wait": {"true"}, "wait-timeout": {"5s"}}, []string{"ent1"}, 3},
		{"add-owner waits only for presence", entitlementsAddOwnerCmd,
			map[string][]string{"app-id": {"app1"}, "entitlement-id": {"ent1"}, "wait": {"true"}, "wait-timeout": {"5s"}}, []string{"u1"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, seen := entOwnerServerUsers(t, seq, nil)
			if _, _, err := runEntOwnerCmd(t, tc.cmd, tc.flags, tc.args...); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			polls := 0
			for _, r := range seen() {
				if strings.HasSuffix(r.rawPath, "/ownerids") {
					polls++
				}
			}
			if polls != tc.wantPolls {
				t.Errorf("polled ownerids %d time(s), want %d", polls, tc.wantPolls)
			}
		})
	}
}
