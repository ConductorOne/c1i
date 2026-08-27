package cmd

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/spf13/cobra"
)

// expandedKey is the envelope sibling holding referenced objects, not the
// resource itself.
const expandedKey = "expanded"

// writeResource is writeObject for a single-resource read: it unwraps the
// envelope first, so --fields and the caller's jq see the resource's own keys
// at the top level, as list rows already do. idKey is "id" everywhere except an
// MCP server, whose only identity is "connectorId".
func writeResource(cmd *cobra.Command, data []byte, idKey string) error {
	return writeObject(cmd, unwrapEnvelope(data, idKey))
}

// unwrapEnvelope hoists a single-object response's payload to the top level,
// keeping every other envelope key as a sibling — dropping them is the data
// loss this unwrap exists to avoid. A shape it can't unwrap losslessly comes
// back untouched; a partial unwrap is never emitted. It finds the payload with
// matchesPathAnyDepth (cmd/fields.go) rather than a second search with its own
// rules, but refuses a same-depth tie, which would promote the wrong object.
// `expanded` is excluded from that search: its referenced objects carry ids of
// their own.
func unwrapEnvelope(data []byte, idKey string) []byte {
	root, ok := decodeJSONObject(data)
	if !ok {
		return data
	}
	if _, _, ok := lookupPath(root, []string{idKey}); ok {
		return data
	}

	search := make(map[string]any, len(root))
	for k, v := range root {
		if k == expandedKey {
			continue
		}
		search[k] = v
	}
	matches := matchesPathAnyDepth(search, []string{idKey})
	if len(matches) != 1 {
		return data
	}
	idPath := matches[0].path
	if len(idPath) < 2 {
		return data
	}
	payloadPath := idPath[:len(idPath)-1]

	// Re-extract from the original bytes instead of re-marshalling the decoded
	// value, which would sort keys and HTML-escape <, >, & inside strings.
	payload, siblings, ok := splitEnvelope(data, payloadPath)
	if !ok {
		return data
	}
	merged, ok := mergeSiblings(payload, siblings)
	if !ok {
		return data
	}
	return merged
}

// envelopeSibling is one key the envelope carried beside the payload.
type envelopeSibling struct {
	key string
	raw json.RawMessage
}

// splitEnvelope walks data along payloadPath, returning the payload's raw bytes
// and every key passed on the way down. Siblings are ordered outermost level
// first, lexicographic within a level, so output doesn't depend on map
// iteration order.
func splitEnvelope(data []byte, payloadPath []string) (json.RawMessage, []envelopeSibling, bool) {
	cur := json.RawMessage(data)
	var siblings []envelopeSibling
	for _, seg := range payloadPath {
		var level map[string]json.RawMessage
		if err := json.Unmarshal(cur, &level); err != nil {
			return nil, nil, false
		}
		next, ok := level[seg]
		if !ok {
			return nil, nil, false
		}
		keys := make([]string, 0, len(level))
		for k := range level {
			if k != seg {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			siblings = append(siblings, envelopeSibling{key: k, raw: level[k]})
		}
		cur = next
	}
	return cur, siblings, true
}

// mergeSiblings appends each sibling to the payload object, on raw bytes so the
// payload's key order and escaping survive; only the sibling *names* are
// re-marshalled, so a non-minimally-escaped key would come back re-serialized.
// A name already taken reports not-ok: merging it would drop a value.
func mergeSiblings(payload json.RawMessage, siblings []envelopeSibling) ([]byte, bool) {
	obj := bytes.TrimSpace(payload)
	if len(obj) < 2 || obj[0] != '{' || obj[len(obj)-1] != '}' {
		return nil, false
	}
	if len(siblings) == 0 {
		return obj, true
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(obj, &keys); err != nil {
		return nil, false
	}
	taken := make(map[string]bool, len(keys)+len(siblings))
	for k := range keys {
		taken[k] = true
	}

	var b bytes.Buffer
	b.WriteByte('{')
	inner := bytes.TrimSpace(obj[1 : len(obj)-1])
	if len(inner) > 0 {
		b.Write(inner)
	}
	for _, s := range siblings {
		if taken[s.key] {
			return nil, false
		}
		taken[s.key] = true
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		name, err := json.Marshal(s.key)
		if err != nil {
			return nil, false
		}
		b.Write(name)
		b.WriteByte(':')
		b.Write(bytes.TrimSpace(s.raw))
	}
	b.WriteByte('}')
	return b.Bytes(), true
}

// decodeJSONObject decodes data as exactly one JSON object, numbers kept as
// json.Number so nothing rounds. Concatenated values report not-ok, mirroring
// projectBytes.
func decodeJSONObject(data []byte) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if dec.More() {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}
