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
	want := map[string]string{
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
		"grant_source_count":       "2",
	}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("row[%q] = %q, want %q", k, row[k], v)
		}
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
	if row["grant_source_count"] != "0" {
		t.Errorf("grant_source_count = %q, want 0 for a direct grant", row["grant_source_count"])
	}
}
