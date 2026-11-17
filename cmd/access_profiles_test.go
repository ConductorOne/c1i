package cmd

import (
	"strings"
	"testing"
)

// The server requires conditions even to disable a bundle automation, and an
// empty entitlementRefs list is how you say "none"; refuse the opaque 400.
func TestBundleAutomationBodyRequiresEntitlements(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	body := writeTempJSON(t, "body.json", `{"enabled":false}`)
	srv := newMutServer(t, fixedReply(200, `{}`))
	res := runMutationCLI(t, srv.srv, 0, nil, "access-profiles", "bundle-automation", "set", "cat-1", "--body-file", body)
	if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), "entitlements is required") {
		t.Fatalf("exit %d err %v, want exit %d with the entitlements message", res.exit, res.err, exitUsage)
	}
	if n := len(srv.requests()); n != 0 {
		t.Errorf("%d request(s) sent, want none", n)
	}

	body = writeTempJSON(t, "body2.json", `{"enabled":false,"entitlements":{"entitlementRefs":[]}}`)
	res = runMutationCLI(t, srv.srv, 0, nil, "access-profiles", "bundle-automation", "create", "cat-1", "--body-file", body, "--dry-run")
	if res.exit != 0 {
		t.Fatalf("body with an empty refs list: exit %d err %v", res.exit, res.err)
	}
}

// An explicit {"entitlements": null} is the same no-conditions body the server
// refuses, so the presence check must not accept it.
func TestBundleAutomationNullEntitlementsRefused(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	body := writeTempJSON(t, "null.json", `{"enabled":false,"entitlements":null}`)
	srv := newMutServer(t, fixedReply(200, `{}`))
	res := runMutationCLI(t, srv.srv, 0, nil, "access-profiles", "bundle-automation", "set", "cat-1", "--body-file", body, "--dry-run")
	if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), "entitlements is required") {
		t.Fatalf("exit %d err %v, want exit %d with the entitlements message", res.exit, res.err, exitUsage)
	}
	if n := len(srv.requests()); n != 0 {
		t.Errorf("%d request(s) sent, want none", n)
	}
}
