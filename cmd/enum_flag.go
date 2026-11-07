package cmd

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// enumUnknown is what enumFlag.parse does with a value outside its table.
type enumUnknown int

const (
	// enumReject: a closed set; anything else is a usage error.
	enumReject enumUnknown = iota
	// enumPassPrefixed: an unknown value carrying the API prefix is sent
	// upper-cased, so an enum newer than c1i still reaches the server. Any
	// other unknown is a usage error — the gateway 400s on it anyway.
	enumPassPrefixed
	// enumPassAny: any unknown is sent trimmed. Only for an enum with no
	// prefix, where a newer value can't be told apart from a typo.
	enumPassAny
)

// enumValue is one accepted value. Its short name, the API name, and the API
// name minus the flag's prefix are all accepted; aliases add more.
type enumValue struct {
	name    string
	api     string
	aliases []string
}

// enumFlag maps a user-facing enum flag to its API enum. Matching ignores
// case and surrounding whitespace, and treats '-' and '_' as equal.
type enumFlag struct {
	flag    string
	prefix  string
	values  []enumValue
	unknown enumUnknown
	// keywords are listed in usage and returned as their short name for the
	// caller to expand (e.g. --origin all).
	keywords []string
}

// as returns e reporting errors under another flag name, for a table shared
// by differently named flags.
func (e enumFlag) as(flag string) enumFlag {
	e.flag = flag
	return e
}

// enumKey normalises s for matching. Callers refuse non-ASCII first:
// strings.ToUpper folds lookalikes (ſ→S, ı→I) onto real values.
func enumKey(s string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), "-", "_"))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// inputs lists the normalised keys that select v: its short name, aliases,
// API name, and API name minus prefix. Prefix+short name is deliberately not
// matched: a future server value of that shape must not be remapped.
func (e enumFlag) inputs(v enumValue) []string {
	keys := []string{enumKey(v.api), enumKey(strings.TrimPrefix(v.api, e.prefix))}
	for _, n := range append([]string{v.name}, v.aliases...) {
		keys = append(keys, enumKey(n))
	}
	return keys
}

// parse returns the API enum for raw. An empty raw returns "" for a flag
// that passes unknowns, as the per-flag mappers did; a blank one is refused.
func (e enumFlag) parse(raw string) (string, error) {
	if raw == "" && e.unknown != enumReject {
		return "", nil
	}
	key := enumKey(raw)
	if key != "" && isASCII(raw) {
		for _, kw := range e.keywords {
			if key == enumKey(kw) {
				return kw, nil
			}
		}
		for _, v := range e.values {
			for _, k := range e.inputs(v) {
				if key == k {
					return v.api, nil
				}
			}
		}
		switch e.unknown {
		case enumPassAny:
			return strings.TrimSpace(raw), nil
		case enumPassPrefixed:
			if e.prefix != "" && strings.HasPrefix(key, e.prefix) && len(key) > len(e.prefix) {
				return key, nil
			}
		}
	}
	return "", &usageError{fmt.Errorf("invalid --%s %q: want %s", e.flag, raw, e.usage())}
}

// without returns e minus the named values, for help that omits a value the
// command refuses with its own message.
func (e enumFlag) without(names ...string) enumFlag {
	var kept []enumValue
	for _, v := range e.values {
		if !slices.Contains(names, v.name) {
			kept = append(kept, v)
		}
	}
	e.values = kept
	return e
}

// usage lists the accepted values for help text and errors, so neither can
// drift from the table.
func (e enumFlag) usage() string {
	names := make([]string, 0, len(e.values)+len(e.keywords))
	for _, v := range e.values {
		names = append(names, v.name)
	}
	names = append(names, e.keywords...)
	s := strings.Join(names, ", ")
	switch {
	case e.prefix == "" && len(names) > 1:
		return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
	case e.prefix == "":
		return s
	case e.unknown == enumPassPrefixed:
		return s + ", or any " + e.prefix + "* value"
	default:
		return s + ", or the full " + e.prefix + "* name of one"
	}
}

// parseRepeatableEnum reads a repeatable flag named e.flag through e.
func parseRepeatableEnum(cmd *cobra.Command, e enumFlag) ([]string, error) {
	raws, err := repeatableStringFlag(cmd, e.flag)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(raws))
	for i, raw := range raws {
		if out[i], err = e.parse(raw); err != nil {
			return nil, err
		}
	}
	return out, nil
}
