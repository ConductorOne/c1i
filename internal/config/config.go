package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseURL normalizes input to "https://{host}" (no trailing slash), lower-casing
// the host: DNS is case-insensitive but the keychain key built from the host
// (KeychainService) is not, so a mixed-case --url must normalize to the same key
// or credential lookup spuriously fails. Validates URL SHAPE only — never which
// tenant domain family a host belongs to, since more than one is valid
// (*.conductor.one, *.c1eu.ai) and nothing here should allowlist a suffix.
//   - Full URL: "https://acme.conductor.one/" → "https://acme.conductor.one"
//   - Protocol-relative: "//acme.conductor.one" → "https://acme.conductor.one"
//   - Raw domain: "ACME.conductor.one" → "https://acme.conductor.one"
//   - Legacy short name: "acme" → "https://acme.conductor.one" (case
//     preserved as typed -- see the dedicated comment at that branch below)
//
// It also returns human-readable warnings for anything silently altered: a
// non-https scheme (rewritten to https rather than rejected -- a typo
// shouldn't turn into a hard failure with no server to inspect) and any
// embedded userinfo. Userinfo is dropped (c1i authenticates via OAuth
// device flow or a keychain-stored client_id/client_secret, never HTTP
// Basic in the URL) and never echoed back, password included, in the
// warning -- true for a scheme-less "user:pass@host" too (an ordinary
// paste-and-forgot-the-scheme mistake), not only when "://" is present.
func ParseURL(input string) (result string, warnings []string) {
	input = strings.TrimSpace(input)

	// "//host" is a plausible typo for "https://host" but has no "://", so it
	// would otherwise miss the fast path below and fall to the raw-domain
	// branch, which prepends "https://" onto the literal leading "//" and
	// mangles it into "https:////host".
	parseable := input
	hasScheme := strings.Contains(input, "://")
	if strings.HasPrefix(input, "//") {
		parseable = "https:" + input
		hasScheme = true
	} else if !hasScheme && strings.Contains(input, ".") {
		// Raw domain, no scheme -- give url.Parse a scheme purely so it can
		// still find "user:pass@host" the same way the scheme-having branch
		// does. hasScheme stays false: we chose https ourselves, so there's
		// no caller-supplied scheme to warn about.
		parseable = "https://" + input
	}

	if strings.Contains(parseable, "://") {
		u, err := url.Parse(parseable)
		if err == nil && u.Host != "" {
			if u.User != nil {
				warnings = append(warnings, fmt.Sprintf(
					"--url embedded credentials (user %q) were dropped; c1i authenticates via OAuth device flow or a keychain-stored client_id/client_secret, never via the URL",
					u.User.Username()))
			}
			if hasScheme && !strings.EqualFold(u.Scheme, "https") {
				warnings = append(warnings, fmt.Sprintf("--url scheme %q is not supported; using https instead", u.Scheme))
			}
			return "https://" + strings.ToLower(u.Host), warnings
		}
		// url.Parse failed, or found no host: fall back to the literal input,
		// lower-cased -- but strip anything before a trailing "@" first so a
		// malformed "user:pass@" fragment still can't echo a password even
		// on this degenerate path.
		return "https://" + strings.ToLower(withoutUserinfoFallback(input)), warnings
	}
	if strings.Contains(input, ".") {
		return "https://" + strings.ToLower(input), warnings
	}
	// Bare short name ("acme" -> "acme.conductor.one"): deliberately NOT
	// lower-cased. With more than one tenant domain family now valid
	// (*.conductor.one, *.c1eu.ai, ...), which family a bare short name
	// expands to is a genuinely open question this fix does not decide --
	// left exactly as-is, case included, pending that separate decision.
	return fmt.Sprintf("https://%s.conductor.one", input), warnings
}

// withoutUserinfoFallback strips a "user:pass@" prefix (if any) from s. Only
// used on ParseURL's rare url.Parse-failed/no-host path, as a last line of
// defense so that path can't echo a password either.
func withoutUserinfoFallback(s string) string {
	if i := strings.LastIndex(s, "@"); i != -1 {
		return s[i+1:]
	}
	return s
}

// KeychainService returns the keychain service name for a given base URL.
// The host is lower-cased independent of whether baseURL already went
// through ParseURL, so a caller that bypasses ParseURL can't reintroduce a
// case-sensitive lookup.
func KeychainService(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err == nil && u.Host != "" {
		return "c1i/" + strings.ToLower(u.Host)
	}
	return "c1i/" + strings.ToLower(baseURL)
}

// LegacyKeychainService returns the old-style keychain service name if the host
// is a *.conductor.one domain, or empty string otherwise. Deliberately
// unchanged by the case-insensitivity fix above: it exists only to migrate a
// pre-existing legacy key format for *.conductor.one, and by the time
// baseURL reaches here it has already been through ParseURL (which now
// lower-cases the host), so this never sees the mixed-case input ParseURL
// itself had to handle.
func LegacyKeychainService(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return ""
	}
	if strings.HasSuffix(u.Host, ".conductor.one") {
		shortName := strings.TrimSuffix(u.Host, ".conductor.one")
		if shortName != "" {
			return "c1i/" + shortName
		}
	}
	return ""
}
