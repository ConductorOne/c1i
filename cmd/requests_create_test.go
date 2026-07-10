package cmd

import (
	"encoding/json"
	"testing"
)

// TestBuildGrantTaskBodyNoWrapper pins the fix for the 400 "unknown field
// \"task\"" bug: POST /api/v1/task/grant (CreateGrantTaskRequest) expects its
// fields at the top level, never nested under a "task" key. It also pins the
// proto field names — identityUserId (not userId) and grantDuration (not
// duration) — which would otherwise be rejected as unknown fields too.
func TestBuildGrantTaskBodyNoWrapper(t *testing.T) {
	body := buildGrantTaskBody("app1", "ent1", "user1", "24h", "test", true)

	if _, wrapped := body["task"]; wrapped {
		t.Fatalf("body must not wrap fields under a \"task\" key: %v", body)
	}

	// Re-marshal to inspect the exact wire shape the server sees.
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	want := map[string]any{
		"appId":            "app1",
		"appEntitlementId": "ent1",
		"identityUserId":   "user1",
		"grantDuration":    "24h",
		"description":      "test",
		"emergencyAccess":  true,
	}
	for k, v := range want {
		if wire[k] != v {
			t.Errorf("wire[%q] = %v, want %v", k, wire[k], v)
		}
	}
	// No stray/legacy field names should leak through.
	for _, bad := range []string{"task", "userId", "duration"} {
		if _, ok := wire[bad]; ok {
			t.Errorf("unexpected field %q in wire body: %v", bad, wire)
		}
	}
	// Pin the exact wire shape: only the fields in `want`, nothing else.
	if len(wire) != len(want) {
		t.Errorf("wire body has %d fields, want exactly %d: %v", len(wire), len(want), wire)
	}
}

// TestBuildGrantTaskBodyOmitsEmpty confirms optional fields are omitted when
// unset, so the request carries only appId/appEntitlementId (self-request,
// default duration, no description, non-emergency).
func TestBuildGrantTaskBodyOmitsEmpty(t *testing.T) {
	body := buildGrantTaskBody("app1", "ent1", "", "", "", false)
	if len(body) != 2 {
		t.Fatalf("expected only appId and appEntitlementId, got %v", body)
	}
	if body["appId"] != "app1" || body["appEntitlementId"] != "ent1" {
		t.Errorf("unexpected body: %v", body)
	}
}

// TestBuildRevokeTaskBodyNoWrapper pins the same flat-body contract for
// POST /api/v1/task/revoke (CreateRevokeTaskRequest).
func TestBuildRevokeTaskBodyNoWrapper(t *testing.T) {
	body := buildRevokeTaskBody("app1", "ent1", "user1", "test")

	if _, wrapped := body["task"]; wrapped {
		t.Fatalf("body must not wrap fields under a \"task\" key: %v", body)
	}

	want := map[string]any{
		"appId":            "app1",
		"appEntitlementId": "ent1",
		"identityUserId":   "user1",
		"description":      "test",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, body[k], v)
		}
	}
	if _, ok := body["userId"]; ok {
		t.Errorf("unexpected legacy field \"userId\" in body: %v", body)
	}
	// Pin the exact wire shape: only the fields in `want`, nothing else.
	if len(body) != len(want) {
		t.Errorf("body has %d fields, want exactly %d: %v", len(body), len(want), body)
	}
}

// TestBuildRevokeTaskBodyOmitsEmpty mirrors the grant omit-empty case: with no
// user or description, the body carries only appId/appEntitlementId.
func TestBuildRevokeTaskBodyOmitsEmpty(t *testing.T) {
	body := buildRevokeTaskBody("app1", "ent1", "", "")
	if len(body) != 2 {
		t.Fatalf("expected only appId and appEntitlementId, got %v", body)
	}
	if body["appId"] != "app1" || body["appEntitlementId"] != "ent1" {
		t.Errorf("unexpected body: %v", body)
	}
}
