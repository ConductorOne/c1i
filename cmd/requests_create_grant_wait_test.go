package cmd

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

const waitGrantRow = `{"list":[{"appEntitlementUserBinding":{"appId":"a1","appEntitlementId":"e1","appUserId":"au1","appUser":{"appUser":{"id":"au1","identityUserId":"u1"}}},"entitlement":{"appEntitlement":{"id":"e1","appId":"a1"}}}],"nextPageToken":""}`

// grantWaitServer answers the pre-read, the create, and grants searches; the
// first emptyPolls grant reads are empty, so the grant appears afterwards.
func grantWaitServer(t *testing.T, emptyPolls int) (*mutServer, *int) {
	t.Helper()
	reads := 0
	srv := newMutServer(t, func(r *http.Request, _ int) mutReply {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
			return mutReply{status: http.StatusOK, body: `{}`}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/task/grant":
			return mutReply{status: http.StatusOK, body: `{"task":{"id":"t1","state":"TASK_STATE_OPEN"}}`}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/search/grants":
			reads++
			if reads <= emptyPolls {
				return mutReply{status: http.StatusOK, body: `{"list":[]}`}
			}
			return mutReply{status: http.StatusOK, body: waitGrantRow}
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		return mutReply{status: http.StatusNotFound, body: `{}`}
	})
	return srv, &reads
}

func setFastGrantWait(t *testing.T) {
	t.Helper()
	old := grantsWaitPollInterval
	grantsWaitPollInterval = time.Millisecond
	t.Cleanup(func() { grantsWaitPollInterval = old })
}

// --wait blocks until the created grant shows up and the set settles; the task
// is still printed on stdout.
func TestRequestsCreateGrantWaitBlocksUntilGrantAppears(t *testing.T) {
	setFastGrantWait(t)
	t.Setenv("C1I_URL", mutTestURL)
	srv, reads := grantWaitServer(t, 2)
	res := runMutationCLI(t, srv.srv, 0, nil,
		"requests", "create", "grant", "--app-id", "a1", "--entitlement-id", "e1", "--user-id", "u1",
		"--wait", "--wait-min", "1", "--wait-stable", "2", "--wait-timeout", "2s")
	if res.exit != 0 {
		t.Fatalf("exit %d err %v stderr %q", res.exit, res.err, res.stderr)
	}
	if !strings.Contains(res.stdout, "t1") {
		t.Errorf("stdout = %q, want the created task", res.stdout)
	}
	if *reads < 3 {
		t.Errorf("grants reads = %d, want at least 3 (empty, empty, grant)", *reads)
	}
	if !strings.Contains(res.stderr, "Grants settled") {
		t.Errorf("stderr = %q, want the settled notice", res.stderr)
	}
}

// The default --wait-min is 1: an empty grant set must not settle successfully.
func TestRequestsCreateGrantWaitDefaultsToOneGrant(t *testing.T) {
	setFastGrantWait(t)
	t.Setenv("C1I_URL", mutTestURL)
	srv, reads := grantWaitServer(t, 1<<30)
	res := runMutationCLI(t, srv.srv, 0, nil,
		"requests", "create", "grant", "--app-id", "a1", "--entitlement-id", "e1", "--user-id", "u1",
		"--wait", "--wait-stable", "2", "--wait-timeout", "200ms")
	if res.exit != 1 {
		t.Fatalf("exit %d err %v, want 1 (timeout)", res.exit, res.err)
	}
	if res.err == nil || !strings.Contains(res.err.Error(), "timed out") ||
		!strings.Contains(res.err.Error(), "not necessarily a failure") {
		t.Errorf("err = %v, want the timeout wording", res.err)
	}
	if *reads == 0 {
		t.Error("no grants read was made")
	}
	if !strings.Contains(res.stdout, "t1") {
		t.Errorf("stdout = %q, want the created task printed before the wait", res.stdout)
	}
}

// A preview never polls.
func TestRequestsCreateGrantWaitSkipsDryRun(t *testing.T) {
	setFastGrantWait(t)
	t.Setenv("C1I_URL", mutTestURL)
	srv, reads := grantWaitServer(t, 0)
	res := runMutationCLI(t, srv.srv, 0, nil,
		"requests", "create", "grant", "--app-id", "a1", "--entitlement-id", "e1", "--user-id", "u1",
		"--wait", "--dry-run")
	if res.exit != 0 {
		t.Fatalf("exit %d err %v", res.exit, res.err)
	}
	if *reads != 0 {
		t.Errorf("grants reads = %d, want none on --dry-run", *reads)
	}
	if !strings.Contains(res.stdout, `"dryRun": true`) {
		t.Errorf("stdout = %q, want a preview", res.stdout)
	}
}

func TestRequestsCreateGrantWaitFlagValidation(t *testing.T) {
	setFastGrantWait(t)
	t.Setenv("C1I_URL", mutTestURL)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			"negative wait-min",
			[]string{"--wait", "--wait-min", "-1"},
			"--wait-min cannot be negative",
		},
		{
			"wait-stable below two",
			[]string{"--wait", "--wait-stable", "1"},
			"--wait-stable must be at least 2",
		},
		{
			"timeout cannot allow the stability reads",
			[]string{"--wait", "--wait-stable", "3", "--wait-timeout", "1ms"},
			"can never allow",
		},
		{
			"wait companion without wait",
			[]string{"--wait-min", "1"},
			"--wait-min requires --wait",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reads := grantWaitServer(t, 0)
			args := append([]string{"requests", "create", "grant", "--app-id", "a1", "--entitlement-id", "e1", "--user-id", "u1"}, tc.args...)
			res := runMutationCLI(t, srv.srv, 0, nil, args...)
			if res.exit != 2 {
				t.Fatalf("exit %d err %v, want 2", res.exit, res.err)
			}
			if res.err == nil || !strings.Contains(res.err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", res.err, tc.want)
			}
			if *reads != 0 {
				t.Errorf("grants reads = %d, want none", *reads)
			}
		})
	}
}
