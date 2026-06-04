package cmd

import (
	"encoding/json"
	"testing"
)

// TestExtractListAndTokenList pins the canonical case: endpoints that wrap
// items under "list" (users, apps, entitlements, functions, automations) keep
// working without --list-key.
func TestExtractListAndTokenList(t *testing.T) {
	data := []byte(`{"list":[{"id":"1"},{"id":"2"}],"nextPageToken":"abc"}`)
	items, token, err := extractListAndToken(data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if token != "abc" {
		t.Errorf("token = %q, want abc", token)
	}
}

// TestExtractListAndTokenTypedKey pins the bug fix: endpoints like
// /api/v1/automation_executions use a typed key ("automationExecutions") for
// the array. Before this fix, c1i looked only for "list", got 0 items, and
// looped forever on the nextPageToken.
func TestExtractListAndTokenTypedKey(t *testing.T) {
	cases := map[string]string{
		"automationExecutions": `{"automationExecutions":[{"id":"e1"},{"id":"e2"}],"nextPageToken":"t"}`,
		"automations":          `{"automations":[{"id":"a1"}],"nextPageToken":""}`,
		"items":                `{"items":[{"id":"i1"},{"id":"i2"},{"id":"i3"}]}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			items, _, err := extractListAndToken([]byte(payload), "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(items) == 0 {
				t.Fatalf("expected non-empty list, got 0 items")
			}
		})
	}
}

// TestExtractListAndTokenForceKey pins the --list-key override. If the user
// specifies a key explicitly, we use that field even when other arrays exist.
func TestExtractListAndTokenForceKey(t *testing.T) {
	data := []byte(`{"list":[{"id":"primary"}],"extra":[{"id":"other"}],"nextPageToken":""}`)
	items, _, err := extractListAndToken(data, "extra")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	var item map[string]string
	_ = json.Unmarshal(items[0], &item)
	if item["id"] != "other" {
		t.Errorf("got id=%q, want 'other'", item["id"])
	}
}

// TestExtractListAndTokenForceKeyMissing returns an empty list (not an error)
// when --list-key points at a field that doesn't exist. The page itself is
// well-formed; the caller can decide whether absence means "stop" or "skip".
func TestExtractListAndTokenForceKeyMissing(t *testing.T) {
	data := []byte(`{"list":[{"id":"a"}],"nextPageToken":""}`)
	items, _, err := extractListAndToken(data, "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty list, got %d items", len(items))
	}
}

// TestExtractListAndTokenForceKeyNotArray errors when the forced key exists
// but isn't an array — that's a user mistake worth surfacing rather than
// silently treating as empty.
func TestExtractListAndTokenForceKeyNotArray(t *testing.T) {
	data := []byte(`{"foo":"bar","nextPageToken":""}`)
	_, _, err := extractListAndToken(data, "foo")
	if err == nil {
		t.Fatal("expected error for non-array forced key, got nil")
	}
}

// TestExtractListAndTokenNoArray returns no items and no error when the
// response is a single-object endpoint (e.g. get-by-id). The api command
// shouldn't be paginating these in the first place, but the helper should
// not panic.
func TestExtractListAndTokenNoArray(t *testing.T) {
	data := []byte(`{"id":"123","displayName":"x"}`)
	items, token, err := extractListAndToken(data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

// TestExtractListAndTokenNullToken handles the "nextPageToken": null case
// some endpoints emit instead of omitting the field.
func TestExtractListAndTokenNullToken(t *testing.T) {
	data := []byte(`{"list":[{"id":"1"}],"nextPageToken":null}`)
	_, token, err := extractListAndToken(data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

// TestExtractListAndTokenMultiArrayDeterministic pins that when a response
// carries more than one array-valued field (and no "list"), the chosen array
// is the first by sorted key name — not a randomized map-iteration pick. The
// loop below runs the extraction many times; a non-deterministic walk would
// eventually select "zebra".
func TestExtractListAndTokenMultiArrayDeterministic(t *testing.T) {
	data := []byte(`{"alpha":[{"id":"a"}],"zebra":[{"id":"z"}],"nextPageToken":"t"}`)
	for i := 0; i < 50; i++ {
		items, token, err := extractListAndToken(data, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "t" {
			t.Errorf("expected token %q, got %q", "t", token)
		}
		if len(items) != 1 || string(items[0]) != `{"id":"a"}` {
			t.Fatalf("expected the sorted-first array (alpha), got %v", items)
		}
	}
}
