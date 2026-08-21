package cmd

import (
	"encoding/json"
	"testing"
)

func TestGrantRow(t *testing.T) {
	raw := `{
		"appEntitlementUserBinding": {
			"appEntitlementUserBindingCreatedAt": "2026-01-02T03:04:05Z",
			"appEntitlementUserBindingDeprovisionAt": "2026-06-01T00:00:00Z",
			"appUser": { "appUser": {
				"id": "au1", "appId": "appA", "displayName": "Ada",
				"email": "ada@x.com", "username": "ada", "identityUserId": "u1",
				"appUserType": "APP_USER_TYPE_USER"
			}},
			"grantSources": [{"appId":"appA","id":"grp1"},{"appId":"appA","id":"grp2"}]
		},
		"entitlement": { "appEntitlement": {
			"id": "ent1", "appId": "appA", "displayName": "Admin", "slug": "admin"
		}}
	}`
	var item grantListItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := grantRow(item)
	want := map[string]any{
		"app_id":                   "appA",
		"entitlement_id":           "ent1",
		"entitlement_display_name": "Admin",
		"entitlement_slug":         "admin",
		"app_user_id":              "au1",
		"app_user_display_name":    "Ada",
		"email":                    "ada@x.com",
		"username":                 "ada",
		"identity_user_id":         "u1",
		"app_user_type":            "APP_USER_TYPE_USER",
		"created_at":               "2026-01-02T03:04:05Z",
		"deprovision_at":           "2026-06-01T00:00:00Z",
	}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("row[%q] = %v, want %q", k, row[k], v)
		}
	}
	// grant_source_count must be a real JSON number, not the string "2" —
	// otherwise a `jq '.grant_source_count > 0'` pipeline does a string
	// comparison instead of a numeric one.
	if row["grant_source_count"] != 2 {
		t.Errorf("grant_source_count = %v (%T), want int 2", row["grant_source_count"], row["grant_source_count"])
	}
}

// TestGrantRowAppIDFallback pins that when the entitlement view carries no
// appId, the row falls back to the account's appId, and that an empty
// grantSources list reports as a direct grant (count 0).
func TestGrantRowAppIDFallback(t *testing.T) {
	raw := `{"appEntitlementUserBinding":{"appUser":{"appUser":{"id":"au1","appId":"appB"}}},"entitlement":{"appEntitlement":{"id":"ent1"}}}`
	var item grantListItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := grantRow(item)
	if row["app_id"] != "appB" {
		t.Errorf("app_id = %q, want appB (fallback to account appId)", row["app_id"])
	}
	if row["grant_source_count"] != 0 {
		t.Errorf("grant_source_count = %v (%T), want int 0 for a direct grant", row["grant_source_count"], row["grant_source_count"])
	}
}

// TestGrantRowDeprovisionAtIsNullNotEmptyString pins that deprovision_at is
// untyped nil, not "", when a grant has no scheduled deprovision — "" is
// truthy in jq, so `jq 'select(.deprovision_at)'` would otherwise match
// every row regardless of whether one is actually scheduled.
func TestGrantRowDeprovisionAtIsNullNotEmptyString(t *testing.T) {
	raw := `{"appEntitlementUserBinding":{"appUser":{"appUser":{"id":"au1","appId":"appA"}}},"entitlement":{"appEntitlement":{"id":"ent1"}}}`
	var item grantListItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := grantRow(item)
	if got := row["deprovision_at"]; got != nil {
		t.Errorf("deprovision_at = %#v, want untyped nil for an unset value", got)
	}
}

// TestGrantRowNestedDeletedAt pins that a grant to a soft-deleted
// entitlement or a soft-deleted account is still an active binding
// server-side, so the row must surface the nested deletedAt of each — the
// grant itself is not what's deleted here.
func TestGrantRowNestedDeletedAt(t *testing.T) {
	raw := `{
		"appEntitlementUserBinding": {
			"appUser": { "appUser": {
				"id": "au1", "appId": "appA", "deletedAt": "2026-03-01T00:00:00Z"
			}}
		},
		"entitlement": { "appEntitlement": {
			"id": "ent1", "appId": "appA", "deletedAt": "2026-02-01T00:00:00Z"
		}}
	}`
	var item grantListItem
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := grantRow(item)
	if row["entitlement_deleted_at"] != "2026-02-01T00:00:00Z" {
		t.Errorf("entitlement_deleted_at = %v, want 2026-02-01T00:00:00Z", row["entitlement_deleted_at"])
	}
	if row["app_user_deleted_at"] != "2026-03-01T00:00:00Z" {
		t.Errorf("app_user_deleted_at = %v, want 2026-03-01T00:00:00Z", row["app_user_deleted_at"])
	}

	// A live grant's nested deletedAt fields must be nil, not "".
	liveRaw := `{"appEntitlementUserBinding":{"appUser":{"appUser":{"id":"au1","appId":"appA"}}},"entitlement":{"appEntitlement":{"id":"ent1"}}}`
	var liveItem grantListItem
	if err := json.Unmarshal([]byte(liveRaw), &liveItem); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	liveRow := grantRow(liveItem)
	if liveRow["entitlement_deleted_at"] != nil {
		t.Errorf("entitlement_deleted_at = %#v, want untyped nil for a live entitlement", liveRow["entitlement_deleted_at"])
	}
	if liveRow["app_user_deleted_at"] != nil {
		t.Errorf("app_user_deleted_at = %#v, want untyped nil for a live account", liveRow["app_user_deleted_at"])
	}
}
