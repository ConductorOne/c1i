package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// A --*-file object key that the command itself sets must be absent (exit 2),
// rather than silently overridden, duplicated or sent mid-stream. The one
// exception: a key set from a POSITIONAL id may be present when its value is
// that positional exactly, so a saved `get` round-trips into `update`. Keys are
// declared once per flag with ownBodyKeys and enforced for every command in
// readJSONFile; the flag's usage text states the rule.
const ownedBodyFieldsAnnotation = "c1i_owned_body_fields"

// ownedKey is a body key (one spelling) and what owns it. A positional owner
// (arg != "") lets the key equal args[pos]; a flag owner never does.
type ownedKey struct {
	key, owner string
	arg        string
	pos        int
}

// pagingBodyKeys are the paging keys, in both spellings the API accepts, that
// --page-size and --page-token own on a body-driven list; c1i sets them per
// page, so there is no equality exception.
var pagingBodyKeys = []ownedKey{
	{key: "pageSize", owner: "--page-size"}, {key: "page_size", owner: "--page-size"},
	{key: "pageToken", owner: "--page-token"}, {key: "page_token", owner: "--page-token"},
}

// argBodyKeys returns keys (every spelling) set from positional number pos
// (0-based), named arg such as "<finding-id>".
func argBodyKeys(pos int, arg string, keys ...string) []ownedKey {
	out := make([]ownedKey, len(keys))
	for i, k := range keys {
		out[i] = ownedKey{key: k, owner: "the " + arg + " argument", arg: arg, pos: pos}
	}
	return out
}

// ownBodyKeys declares the keys the command owns on file flag `flag` and
// states the rule in its usage.
func ownBodyKeys(cmd *cobra.Command, flag string, groups ...[]ownedKey) {
	f := cmd.Flags().Lookup(flag)
	if f == nil {
		panic("ownBodyKeys: no --" + flag + " flag on " + cmd.CommandPath())
	}
	var entries, items []string
	var absent []string
	eq := map[string][]string{}
	var eqOrder []string
	for _, g := range groups {
		for _, o := range g {
			entries = append(entries, fmt.Sprintf("%s\x1f%s\x1f%s\x1f%d", o.key, o.owner, o.arg, o.pos))
			if o.arg == "" {
				absent = append(absent, o.key)
				continue
			}
			if _, seen := eq[o.arg]; !seen {
				eqOrder = append(eqOrder, o.arg)
			}
			eq[o.arg] = append(eq[o.arg], o.key)
		}
	}
	if len(absent) > 0 {
		items = append(items, strings.Join(absent, ", "))
	}
	for _, arg := range eqOrder {
		items = append(items, strings.Join(eq[arg], ", ")+" (or must equal "+arg+")")
	}
	if err := cmd.Flags().SetAnnotation(flag, ownedBodyFieldsAnnotation, entries); err != nil {
		panic(err)
	}
	clause := "; must not contain " + strings.Join(items, ", ") + "; exit 2"
	const required = " (required)"
	if base, ok := strings.CutSuffix(f.Usage, required); ok {
		f.Usage = base + clause + required
	} else {
		f.Usage += clause
	}
}

// refuseCLIOwnedBodyKeys fails with a usage error naming every key of body
// that the command owns on `flag` and may not carry: any value of a flag-owned
// key (null included), and any value of a positional-owned key other than
// that positional as a string. A key matches in either spelling the server
// accepts (fieldSpellings), so declaring one covers both; a body carrying
// both is refused.
func refuseCLIOwnedBodyKeys(cmd *cobra.Command, flag string, body map[string]any) error {
	f := cmd.Flags().Lookup(flag)
	if f == nil {
		return nil
	}
	positionals := cmd.Flags().Args()
	var keys, owners, mismatches []string
	// The server reads a field's camel and snake spellings as one field and
	// 400s on both, so spellings equal to the positional collapse to the
	// first-declared one.
	canonical := map[int]string{}
	equal := map[int][]string{}
	claimed := map[string]bool{}
	for _, e := range f.Annotations[ownedBodyFieldsAnnotation] {
		p := strings.Split(e, "\x1f")
		declared, owner, arg := p[0], p[1], p[2]
		var pos int
		if arg != "" {
			pos, _ = strconv.Atoi(p[3])
			if _, seen := canonical[pos]; !seen {
				canonical[pos] = declared
			}
		}
		for _, key := range bodyKeys(body, declared) {
			if claimed[key] {
				continue
			}
			claimed[key] = true
			if arg == "" {
				keys = append(keys, key)
				owners = appendUnique(owners, owner)
				continue
			}
			value := body[key]
			if pos < len(positionals) {
				if s, isString := value.(string); isString && s == positionals[pos] {
					equal[pos] = append(equal[pos], key)
					continue
				}
			}
			given, _ := json.Marshal(value)
			want := "(unavailable)"
			if pos < len(positionals) {
				want = strconv.Quote(positionals[pos])
			}
			mismatches = append(mismatches, fmt.Sprintf("--%s %s is %s but %s is %s; remove it or set it to the argument", flag, key, given, owner, want))
		}
	}
	var msgs []string
	if len(keys) > 0 {
		pronoun := "it"
		if len(keys) > 1 {
			pronoun = "them"
		}
		msgs = append(msgs, fmt.Sprintf("--%s must not contain %s; remove %s and use %s", flag, strings.Join(keys, ", "), pronoun, joinAnd(owners)))
	}
	msgs = append(msgs, mismatches...)
	if len(msgs) > 0 {
		return &usageError{errors.New(strings.Join(msgs, "; "))}
	}
	for _, present := range equal {
		if len(present) > 1 { // both spellings, each equal to the positional
			_, _, err := bodyField(body, present[0])
			return err
		}
	}
	for pos, present := range equal {
		for _, k := range present {
			delete(body, k)
		}
		body[canonical[pos]] = positionals[pos]
	}
	return nil
}

// joinAnd renders "a", "a and b", "a, b and c".
func joinAnd(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}
