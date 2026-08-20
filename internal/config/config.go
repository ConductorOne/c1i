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
//
// A single-label host is accepted when it arrives as a URL
// ("https://c1-staging", "//c1-staging") -- no domain is guessed, the host is
// used as typed, which is how an internal-resolver name is reached. It is only
// a bare token (no "://" and no ".", e.g. "acme" or "localhost") that is
// rejected: with more than one valid tenant domain family, expanding it to
// one of them by default is a silent wrong-tenant risk. err is non-nil only
// for this case, and the caller should name where input came from (--url
// flag, C1I_URL, config file, interactive prompt) in how it surfaces err --
// see GetBaseURLWithSource.
//
// A scheme other than https (e.g. "http://", "ftp://") is rejected outright
// -- c1i requires https, full stop, no silent upgrade and no exception for
// loopback/local hosts.
//
// It also returns human-readable warnings for anything silently altered:
// embedded userinfo. Userinfo is dropped (c1i authenticates via OAuth
// device flow or a keychain-stored client_id/client_secret, never HTTP
// Basic in the URL) and never echoed back, password included, in the
// warning -- true for a scheme-less "user:pass@host" too (an ordinary
// paste-and-forgot-the-scheme mistake), not only when "://" is present.
func ParseURL(input string) (result string, warnings []string, err error) {
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
			if hasScheme && !strings.EqualFold(u.Scheme, "https") {
				return "", nil, fmt.Errorf("c1i requires https; got scheme %q", u.Scheme)
			}
			if u.User != nil {
				warnings = append(warnings, fmt.Sprintf(
					"--url embedded credentials (user %q) were dropped; %s",
					u.User.Username(), credentialsNotInURL))
			}
			return "https://" + strings.ToLower(u.Host), warnings, nil
		}
		// url.Parse failed, or found no host: fall back to the literal input,
		// lower-cased -- but strip anything before a trailing "@" first so a
		// malformed "user:pass@" fragment still can't echo a password even
		// on this degenerate path. There is no parsed URL to name the user
		// from, but the drop must still not be silent.
		stripped := withoutUserinfo(input)
		if stripped != input {
			warnings = append(warnings, "--url embedded credentials were dropped; "+credentialsNotInURL)
		}
		return "https://" + strings.ToLower(stripped), warnings, nil
	}
	// Bare token, e.g. "acme" or "localhost": retired. It used to expand to
	// "<input>.conductor.one", but with a second tenant domain family
	// (*.c1eu.ai) now valid, guessing which one is a silent wrong-tenant
	// risk -- an EU customer typing "acme" would land on a US host.
	return "", nil, fmt.Errorf(
		"url %q is not a full host: c1i no longer expands a bare name to a domain; "+
			"pass a full host such as acme.conductor.one or acme.c1eu.ai",
		withoutUserinfo(input))
}

// credentialsNotInURL is shared by both credential-dropping warnings so they
// cannot drift apart.
const credentialsNotInURL = "c1i authenticates via OAuth device flow or a keychain-stored client_id/client_secret, never via the URL"

// withoutUserinfo strips a "user:pass@" prefix (if any) from s. Applied on
// every ParseURL path that echoes or returns raw input, so none of them can
// print a password -- including the error paths, where there is no parsed URL
// to take a host from.
func withoutUserinfo(s string) string {
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
