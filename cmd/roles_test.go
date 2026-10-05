package cmd

import (
	"reflect"
	"testing"
)

// TestRoleRowKeepsJSONTypes pins the row conventions: proto3 omits false bools
// and unset timestamps, which must come out as false and null, never "".
func TestRoleRowKeepsJSONTypes(t *testing.T) {
	got := roleRow(map[string]any{"id": "r-1", "name": "system:user", "displayName": "Basic User"})
	want := map[string]any{
		"id":              "r-1",
		"name":            "system:user",
		"display_name":    "Basic User",
		"system_builtin":  false,
		"system_api_only": false,
		"created_at":      nil,
		"updated_at":      nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sparse row = %#v, want %#v", got, want)
	}

	got = roleRow(map[string]any{"id": "r-2", "systemBuiltin": true, "systemApiOnly": true, "createdAt": "2021-01-01T00:00:00Z"})
	if got["system_builtin"] != true || got["system_api_only"] != true || got["created_at"] != "2021-01-01T00:00:00Z" {
		t.Errorf("full row = %#v", got)
	}
}
