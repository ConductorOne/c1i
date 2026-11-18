package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// isDeletedAt: present and not null means deleted; null, "" and absent are live.
func TestIsDeletedAt(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`"2026-01-01T00:00:00Z"`, true},
		{` "x" `, true},
		{`{"seconds":1}`, true},
		{`null`, false},
		{` null `, false},
		{`""`, false},
		{``, false},
	}
	for _, tc := range cases {
		if got := isDeletedAt(json.RawMessage(tc.raw)); got != tc.want {
			t.Errorf("isDeletedAt(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// deletedAtPath reads only the named object: a deletedAt elsewhere in the
// response is not that object's.
func TestDeletedAtPath(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		path    []string
		ok      bool
		deleted bool
	}{
		{"live", `{"app":{"id":"a","deletedAt":null}}`, []string{"app"}, true, false},
		{"deleted", `{"app":{"id":"a","deletedAt":"2026-01-01T00:00:00Z"}}`, []string{"app"}, true, true},
		{"empty deletedAt", `{"app":{"id":"a","deletedAt":""}}`, []string{"app"}, true, false},
		{"nested view", `{"appEntitlementView":{"appEntitlement":{"deletedAt":"x"}}}`, []string{"appEntitlementView", "appEntitlement"}, true, true},
		{"sibling deletedAt is not the object's", `{"app":{"id":"a"},"expanded":[{"deletedAt":"x"}],"other":{"deletedAt":"x"}}`, []string{"app"}, true, false},
		{"envelope deletedAt is not the object's", `{"appResourceTypeView":{"deletedAt":"x","appResourceType":{"id":"rt"}}}`, []string{"appResourceTypeView", "appResourceType"}, true, false},
		{"missing envelope", `{}`, []string{"app"}, false, false},
		{"null envelope", `{"app":null}`, []string{"app"}, false, false},
		{"non-object envelope", `{"app":"x"}`, []string{"app"}, false, false},
		{"not JSON", `<html>`, []string{"app"}, false, false},
		{"null body", `null`, nil, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deleted, at, ok := deletedAtPath([]byte(tc.raw), tc.path...)
			if ok != tc.ok || deleted != tc.deleted {
				t.Fatalf("deleted, ok = %v, %v; want %v, %v", deleted, ok, tc.deleted, tc.ok)
			}
			if deleted && at == "" {
				t.Error("a deleted object reported no deletedAt text")
			}
		})
	}
}

// A referenced-object lookup reads the object it asked for: "" is live, a
// 200 without that object exits 6, and nothing is written either way.
func TestRefLookupReadsItsOwnObject(t *testing.T) {
	args := []string{"access-reviews", "templates", "entitlements", "add", "tpl-1", "--app-id", "app-1", "--entitlement-id", "ent-1"}
	for _, tc := range []struct {
		name, get string
		exit      int
	}{
		{"empty deletedAt", `{"appEntitlementView":{"appEntitlement":{"id":"ent-1","deletedAt":""}}}`, 0},
		{"no entitlement object", `{}`, exitServer},
		{"another object's deletedAt", `{"appEntitlementView":{"appEntitlement":{"id":"ent-1"}},"other":{"deletedAt":"x"}}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			clearViperOverrides(t, "dry_run")
			withDryRun(t)
			s := newMutServer(t, func(r *http.Request, _ int) mutReply {
				if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/entitlements/") {
					return mutReply{body: tc.get}
				}
				return mutReply{body: `{}`}
			})
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.exit {
				t.Errorf("exit %d, want %d: %s", r.exit, tc.exit, r.stderr)
			}
			if len(writes(s)) != 0 {
				t.Errorf("sent %v", writes(s))
			}
		})
	}
}
