package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const grantsAddOK = `{"failedUsersErrorMap":{},"bulkActionId":"b1"}`

// grantsAddRoute answers user lookups (u-gone is unknown), the write, and the
// grants search with the next of pages per poll (the last repeats).
func grantsAddRoute(pages []string) func(*http.Request, int) mutReply {
	polls := 0
	return func(r *http.Request, _ int) mutReply {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/u-gone":
			return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
		case r.Method == http.MethodGet:
			return mutReply{body: `{"user":{"id":"x"}}`}
		case strings.HasSuffix(r.URL.Path, "/add-manual-user"):
			return mutReply{body: grantsAddOK}
		case r.URL.Path == "/api/v1/search/grants":
			body := pages[min(polls, len(pages)-1)]
			polls++
			return mutReply{body: body}
		}
		return mutReply{status: http.StatusTeapot}
	}
}

func grantsPage(users ...string) string {
	var rows []string
	for _, u := range users {
		rows = append(rows, `{"appEntitlementUserBinding":{"appUser":{"appUser":{"id":"au-`+u+`","identityUserId":"`+u+`"}}}}`)
	}
	return `{"list":[` + strings.Join(rows, ",") + `]}`
}

func countPath(s *mutServer, suffix string) int {
	n := 0
	for _, r := range s.requests() {
		if strings.HasSuffix(r.rawPath, suffix) {
			n++
		}
	}
	return n
}

func TestGrantsAddSendsOneDedupedRequest(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, grantsAddRoute(nil))
	r := runMutationCLI(t, s.srv, 0, nil, "grants", "add", "--app-id", "app-1", "--entitlement-id", "ent-1",
		"--user-id", "u1", "--user-id", "u2", "--user-id", "u1")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	var writes []entOwnerRequest
	for _, q := range s.requests() {
		if q.method == http.MethodPost {
			writes = append(writes, q)
		}
	}
	if len(writes) != 1 || writes[0].rawPath != "/api/v1/apps/app-1/entitlements/ent-1/add-manual-user" {
		t.Fatalf("writes = %+v, want one POST add-manual-user", writes)
	}
	var body struct {
		UserIDs []string `json:"userIds"`
	}
	if err := json.Unmarshal([]byte(writes[0].body), &body); err != nil || strings.Join(body.UserIDs, ",") != "u1,u2" {
		t.Errorf("body = %s, want userIds [u1 u2]", writes[0].body)
	}
}

// An unknown user id exits 4 with no write, on a real run and a preview alike:
// the server answers such an id with 200 and does nothing.
func TestGrantsAddUnknownUserExits4NothingSent(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(map[bool]string{false: "real", true: "dry-run"}[dry], func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			if dry {
				withDryRun(t)
			}
			s := newMutServer(t, grantsAddRoute(nil))
			r := runMutationCLI(t, s.srv, 0, nil, "grants", "add", "--app-id", "app-1", "--entitlement-id", "ent-1",
				"--user-id", "u1", "--user-id", "u-gone")
			if r.exit != exitNotFound {
				t.Fatalf("exit = %d (%v), want %d", r.exit, r.err, exitNotFound)
			}
			if n := countPath(s, "/add-manual-user"); n != 0 {
				t.Errorf("%d add-manual-user request(s) sent, want none", n)
			}
		})
	}
}

// --wait holds until every requested user shows as a grant holder, not just
// any grant.
func TestGrantsAddWaitsForEveryUser(t *testing.T) {
	orig := grantsWaitPollInterval
	grantsWaitPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { grantsWaitPollInterval = orig })
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, grantsAddRoute([]string{grantsPage(), grantsPage("u1"), grantsPage("u2", "u1")}))
	r := runMutationCLI(t, s.srv, 0, nil, "grants", "add", "--app-id", "app-1", "--entitlement-id", "ent-1",
		"--user-id", "u1", "--user-id", "u2", "--wait", "--wait-timeout", "5s")
	if r.err != nil {
		t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
	}
	if n := countPath(s, "/search/grants"); n != 3 {
		t.Errorf("polled grants %d time(s), want 3", n)
	}
	if strings.Contains(r.stdout, "Grants appeared") || !strings.Contains(r.stderr, "Grants appeared") {
		t.Errorf("progress must be on stderr only: stdout=%q stderr=%q", r.stdout, r.stderr)
	}
}

func TestGrantsAddWaitTimeoutExits1(t *testing.T) {
	orig := grantsWaitPollInterval
	grantsWaitPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { grantsWaitPollInterval = orig })
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, grantsAddRoute([]string{grantsPage("u1")}))
	r := runMutationCLI(t, s.srv, 0, nil, "grants", "add", "--app-id", "app-1", "--entitlement-id", "ent-1",
		"--user-id", "u1", "--user-id", "u2", "--wait", "--wait-timeout", "50ms")
	if r.exit != 1 || r.err == nil || !strings.Contains(r.err.Error(), "was applied") {
		t.Fatalf("exit = %d, err = %v; want exit 1 naming the applied write", r.exit, r.err)
	}
}
