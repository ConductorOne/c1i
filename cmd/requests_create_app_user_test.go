package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// appUserServer answers the app-user pre-check, the create, and (optionally)
// grants searches. Each request is recorded by the shared mutServer harness.
func appUserServer(t *testing.T, found bool, emptyPolls int) *mutServer {
	t.Helper()
	reads := 0
	return newMutServer(t, func(r *http.Request, _ int) mutReply {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/search/app_users":
			if !found {
				return mutReply{status: http.StatusOK, body: `{"list":[]}`}
			}
			return mutReply{status: http.StatusOK, body: `{"list":[{"appUser":{"id":"au1","appId":"a1"}}]}`}
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
			return mutReply{status: http.StatusOK, body: `{}`}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/introspect":
			return mutReply{status: http.StatusOK, body: `{"userId":"self1"}`}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/task/grant":
			return mutReply{status: http.StatusOK, body: `{"task":{"id":"t1","state":"TASK_STATE_OPEN"}}`}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/task/revoke":
			return mutReply{status: http.StatusOK, body: `{"task":{"id":"t2","state":"TASK_STATE_OPEN"}}`}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/search/grants":
			reads++
			if reads <= emptyPolls {
				return mutReply{status: http.StatusOK, body: `{"list":[]}`}
			}
			return mutReply{status: http.StatusOK, body: `{"list":[{"appEntitlementUserBinding":{"appId":"a1","appEntitlementId":"e1","appUserId":"au1","appUser":{"appUser":{"id":"au1","identityUserId":"u1"}}},"entitlement":{"appEntitlement":{"id":"e1","appId":"a1"}}}],"nextPageToken":""}`}
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		return mutReply{status: http.StatusNotFound, body: `{}`}
	})
}

func findRequest(reqs []entOwnerRequest, method, path string) *entOwnerRequest {
	for i := range reqs {
		if reqs[i].method == method && reqs[i].rawPath == path {
			return &reqs[i]
		}
	}
	return nil
}

// An app account alone names the recipient: appUserId is sent, identityUserId
// is not, and the caller is never resolved.
func TestRequestsCreateGrantByAppUserID(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := appUserServer(t, true, 0)
	res := runMutationCLI(t, srv.srv, 0, nil,
		"requests", "create", "grant", "--app-id", "a1", "--entitlement-id", "e1", "--app-user-id", "au1")
	if res.exit != 0 {
		t.Fatalf("exit %d err %v stderr %q", res.exit, res.err, res.stderr)
	}
	reqs := srv.requests()
	if findRequest(reqs, http.MethodGet, "/api/v1/auth/introspect") != nil {
		t.Error("introspect was called; --app-user-id must not resolve the caller")
	}
	post := findRequest(reqs, http.MethodPost, "/api/v1/task/grant")
	if post == nil {
		t.Fatalf("no grant request: %+v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(post.body), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["appUserId"] != "au1" {
		t.Errorf("appUserId = %v, want au1", body["appUserId"])
	}
	if _, ok := body["identityUserId"]; ok {
		t.Errorf("identityUserId must be absent when only --app-user-id is given: %v", body)
	}
}

// Both flags are sent together; the server enforces that the account belongs
// to the user.
func TestRequestsCreateGrantByUserAndAppUser(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := appUserServer(t, true, 0)
	res := runMutationCLI(t, srv.srv, 0, nil,
		"requests", "create", "grant", "--app-id", "a1", "--entitlement-id", "e1", "--user-id", "u1", "--app-user-id", "au1")
	if res.exit != 0 {
		t.Fatalf("exit %d err %v stderr %q", res.exit, res.err, res.stderr)
	}
	post := findRequest(srv.requests(), http.MethodPost, "/api/v1/task/grant")
	if post == nil {
		t.Fatal("no grant request")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(post.body), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["identityUserId"] != "u1" || body["appUserId"] != "au1" {
		t.Errorf("body = %v, want both recipient ids", body)
	}
}

// An unknown app account is refused before the write, with the not-found exit.
func TestRequestsCreateGrantUnknownAppUser(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := appUserServer(t, false, 0)
	res := runMutationCLI(t, srv.srv, 0, nil,
		"requests", "create", "grant", "--app-id", "a1", "--entitlement-id", "e1", "--app-user-id", "auX")
	if res.exit != 4 {
		t.Fatalf("exit %d err %v, want 4", res.exit, res.err)
	}
	if res.err == nil || !strings.Contains(res.err.Error(), `--app-user-id "auX": no such app user`) {
		t.Errorf("err = %v, want the not-found wording", res.err)
	}
	if findRequest(srv.requests(), http.MethodPost, "/api/v1/task/grant") != nil {
		t.Error("a grant request was sent after the app-user pre-check failed")
	}
}

// Revoke takes the same recipient pair.
func TestRequestsCreateRevokeByAppUserID(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := appUserServer(t, true, 0)
	res := runMutationCLI(t, srv.srv, 0, nil,
		"requests", "create", "revoke", "--app-id", "a1", "--entitlement-id", "e1", "--app-user-id", "au1")
	if res.exit != 0 {
		t.Fatalf("exit %d err %v stderr %q", res.exit, res.err, res.stderr)
	}
	post := findRequest(srv.requests(), http.MethodPost, "/api/v1/task/revoke")
	if post == nil {
		t.Fatal("no revoke request")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(post.body), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["appUserId"] != "au1" {
		t.Errorf("appUserId = %v, want au1", body["appUserId"])
	}
	if _, ok := body["identityUserId"]; ok {
		t.Errorf("identityUserId must be absent: %v", body)
	}
}

// --wait filters the grant poll to the named app account, not the caller.
func TestRequestsCreateGrantWaitByAppUserID(t *testing.T) {
	old := grantsWaitPollInterval
	grantsWaitPollInterval = time.Millisecond
	t.Cleanup(func() { grantsWaitPollInterval = old })
	t.Setenv("C1I_URL", mutTestURL)
	srv := appUserServer(t, true, 1)
	res := runMutationCLI(t, srv.srv, 0, nil,
		"requests", "create", "grant", "--app-id", "a1", "--entitlement-id", "e1", "--app-user-id", "au1",
		"--wait", "--wait-stable", "2", "--wait-timeout", "2s")
	if res.exit != 0 {
		t.Fatalf("exit %d err %v stderr %q", res.exit, res.err, res.stderr)
	}
	search := findRequest(srv.requests(), http.MethodPost, "/api/v1/search/grants")
	if search == nil {
		t.Fatal("no grants search")
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(search.body), &body); err != nil {
		t.Fatalf("search body: %v", err)
	}
	ids, _ := body["appUserIds"].([]any)
	if len(ids) != 1 || ids[0] != "au1" {
		t.Errorf("appUserIds = %v, want [au1]", body["appUserIds"])
	}
	if _, ok := body["userId"]; ok {
		t.Errorf("userId must be absent when the recipient is an app account: %v", body)
	}
}
