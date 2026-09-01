package cmd

import (
	"encoding/json"
	"fmt"
	"testing"
)

// catalogViewJSON renders one `catalogs list` row as the API sends it: the
// catalog nested under "requestCatalog", memberCount a sibling encoded as a
// string.
func catalogViewJSON(published, visibleToEveryone, requestBundle bool) string {
	return fmt.Sprintf(`{
		"requestCatalog": {
			"id": "cat1",
			"displayName": "Engineering",
			"description": "eng access",
			"deletedAt": null,
			"published": %t,
			"visibleToEveryone": %t,
			"requestBundle": %t
		},
		"memberCount": "0"
	}`, published, visibleToEveryone, requestBundle)
}

// TestCatalogRowKeepsRealJSONTypes pins that each boolean stays a bool AND
// lands under the right key. Stringifying one breaks NDJSON consumers
// silently: every non-empty string is truthy, so `jq 'select(.published)'`
// would match a "false".
//
// The cases are one-hot — exactly one field true per case — because three
// booleans cannot all differ. An all-true fixture would let any two of the
// three be cross-wired and still pass; this way every pairwise swap flips an
// assertion in at least one case.
func TestCatalogRowKeepsRealJSONTypes(t *testing.T) {
	tests := []struct {
		name                                    string
		published, visibleToEveryone, reqBundle bool
	}{
		{name: "published only", published: true},
		{name: "visible to everyone only", visibleToEveryone: true},
		{name: "request bundle only", reqBundle: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var item catalogListItem
			body := catalogViewJSON(tt.published, tt.visibleToEveryone, tt.reqBundle)
			if err := json.Unmarshal([]byte(body), &item); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			row := catalogRow(item)

			for key, want := range map[string]bool{
				"published":           tt.published,
				"visible_to_everyone": tt.visibleToEveryone,
				"request_bundle":      tt.reqBundle,
			} {
				v, ok := row[key]
				if !ok {
					t.Fatalf("row has no %s key", key)
				}
				b, ok := v.(bool)
				if !ok {
					t.Fatalf("%s has type %T, want bool", key, v)
				}
				if b != want {
					t.Errorf("%s = %v, want %v", key, b, want)
				}
			}

			if row["id"] != "cat1" || row["display_name"] != "Engineering" {
				t.Errorf("row = %#v, want the nested requestCatalog fields hoisted", row)
			}
		})
	}
}

// TestCatalogRowOmitsMemberCount pins that the view's memberCount does not
// reach the row. The list endpoint reports it as "0" for every catalog, so a
// member_count key here would read as "no members" for all of them and
// `jq 'select(.member_count > 0)'` would never match.
func TestCatalogRowOmitsMemberCount(t *testing.T) {
	var item catalogListItem
	if err := json.Unmarshal([]byte(catalogViewJSON(true, true, true)), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := catalogRow(item)["member_count"]; ok {
		t.Errorf("row has member_count = %#v; the list endpoint does not populate it, so it must be omitted", v)
	}
}

// TestCatalogRowDeletedAtIsNullNotEmptyString pins that deleted_at is untyped
// nil, not "", on a live catalog — "" is truthy in jq and would make
// `jq 'select(.deleted_at)'` match every row.
func TestCatalogRowDeletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name      string
		deletedAt string
		want      any
	}{
		{name: "live catalog", deletedAt: "", want: nil},
		{name: "deleted catalog", deletedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var item catalogListItem
			item.RequestCatalog.ID = "cat1"
			item.RequestCatalog.DeletedAt = tt.deletedAt

			got, ok := catalogRow(item)["deleted_at"]
			if !ok {
				t.Fatal("row has no deleted_at key")
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("deleted_at = %#v, want untyped nil", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("deleted_at = %v, want %v", got, tt.want)
			}
		})
	}
}
