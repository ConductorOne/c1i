package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUnwrapEnvelopeHoistsPayload covers the two live envelope families: a
// depth-1 wrapper, and a *View wrapper whose own keys (plus a top-level
// expanded) must come along as siblings.
func TestUnwrapEnvelopeHoistsPayload(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		idKey string
		want  string
	}{
		{
			name:  "depth 1",
			in:    `{"app":{"id":"app-1","displayName":"Okta"}}`,
			idKey: "id",
			want:  `{"id":"app-1","displayName":"Okta"}`,
		},
		{
			name:  "depth 1 keyed by connectorId",
			in:    `{"mcpServer":{"connectorId":"conn-1","appId":"app-1"}}`,
			idKey: "connectorId",
			want:  `{"connectorId":"conn-1","appId":"app-1"}`,
		},
		{
			name:  "userView keeps its siblings and expanded",
			in:    `{"userView":{"user":{"id":"u1"},"userId":"u1x","rolesPath":""},"expanded":[{"id":"r1"}]}`,
			idKey: "id",
			want:  `{"id":"u1","expanded":[{"id":"r1"}],"rolesPath":"","userId":"u1x"}`,
		},
		{
			name:  "taskView keeps its siblings and expanded",
			in:    `{"taskView":{"task":{"id":"t1","state":"TASK_STATE_OPEN"},"userPath":"","objectPermissions":null},"expanded":[{"id":"e1"}]}`,
			idKey: "id",
			want:  `{"id":"t1","state":"TASK_STATE_OPEN","expanded":[{"id":"e1"}],"objectPermissions":null,"userPath":""}`,
		},
		{
			name:  "appEntitlementView keeps its siblings and expanded",
			in:    `{"appEntitlementView":{"appEntitlement":{"id":"ae1"},"appPath":"$.x","objectPermissions":{"read":true}},"expanded":[{"id":"a1"}]}`,
			idKey: "id",
			want:  `{"id":"ae1","expanded":[{"id":"a1"}],"appPath":"$.x","objectPermissions":{"read":true}}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := string(unwrapEnvelope([]byte(c.in), c.idKey))
			if got != c.want {
				t.Errorf("unwrapEnvelope() = %s\nwant %s", got, c.want)
			}
		})
	}
}

// TestUnwrapEnvelopeIgnoresExpandedWhenLocating: `expanded` holds referenced
// objects with ids of their own, so it must not be searched — otherwise its id
// can outrank the payload's at the same depth ("expanded" sorts before
// "userView") and the wrong object gets promoted.
func TestUnwrapEnvelopeIgnoresExpandedWhenLocating(t *testing.T) {
	in := `{"userView":{"user":{"id":"u1"}},"expanded":{"role":{"id":"r1"}}}`
	got := map[string]any{}
	if err := json.Unmarshal(unwrapEnvelope([]byte(in), "id"), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["id"] != "u1" {
		t.Errorf("id = %v, want the payload's u1", got["id"])
	}
}

// TestUnwrapEnvelopePayloadWithOwnExpandedIsUntouched: merging would drop one of
// the two "expanded" values, so the whole response is left alone instead.
func TestUnwrapEnvelopePayloadWithOwnExpandedIsUntouched(t *testing.T) {
	in := `{"userView":{"user":{"id":"u1","expanded":{"mine":true}}},"expanded":[{"id":"r1"}]}`
	if got := string(unwrapEnvelope([]byte(in), "id")); got != in {
		t.Errorf("unwrapEnvelope() = %s\nwant it unchanged", got)
	}
}

// TestUnwrapEnvelopeLeavesUnrecognizedShapes: every shape the unwrap cannot
// prove lossless comes back byte-identical.
func TestUnwrapEnvelopeLeavesUnrecognizedShapes(t *testing.T) {
	cases := map[string]string{
		"not JSON":              `not json at all`,
		"not an object":         `[{"id":"a"}]`,
		"scalar":                `"hello"`,
		"already flat":          `{"id":"app-1","displayName":"Okta"}`,
		"no id anywhere":        `{"app":{"displayName":"Okta"}}`,
		"id only in expanded":   `{"expanded":{"role":{"id":"r1"}}}`,
		"ambiguous same depth":  `{"a":{"id":"1"},"b":{"id":"2"}}`,
		"sibling name collides": `{"appView":{"app":{"id":"a1","appPath":"x"},"appPath":"y"}}`,
		"concatenated values":   `{"app":{"id":"a"}}{"app":{"id":"b"}}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if got := string(unwrapEnvelope([]byte(in), "id")); got != in {
				t.Errorf("unwrapEnvelope() = %s\nwant it unchanged", got)
			}
		})
	}
}

// TestUnwrapEnvelopePreservesRawBytes: the payload is re-extracted from the
// original bytes, never re-marshalled, so a large integer keeps every digit and
// a string keeps its own escaping and key order.
func TestUnwrapEnvelopePreservesRawBytes(t *testing.T) {
	in := `{"function":{"numericId":9007199254740993,"name":"a & b <c>","id":"fn-1"}}`
	got := string(unwrapEnvelope([]byte(in), "id"))
	want := `{"numericId":9007199254740993,"name":"a & b <c>","id":"fn-1"}`
	if got != want {
		t.Errorf("unwrapEnvelope() = %s\nwant %s", got, want)
	}
	if strings.Contains(got, `\u0026`) {
		t.Error("output HTML-escaped an ampersand, so the payload was re-marshalled rather than re-extracted")
	}
}
