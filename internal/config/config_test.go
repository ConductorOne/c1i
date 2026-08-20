package config

import (
	"strings"
	"testing"
)

func TestParseURLBasicNormalization(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"full URL with trailing slash", "https://acme.conductor.one/", "https://acme.conductor.one"},
		{"raw domain", "acme.conductor.one", "https://acme.conductor.one"},
		{"legacy short name", "acme", "https://acme.conductor.one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings := ParseURL(tc.input)
			if got != tc.want {
				t.Errorf("ParseURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if len(warnings) != 0 {
				t.Errorf("ParseURL(%q) warnings = %v, want none", tc.input, warnings)
			}
		})
	}
}

// TestParseURLCaseInsensitiveHost is sub-issue (c), the priority bug: a
// legitimate but differently-cased --url must not be rejected. Hosts are
// case-insensitive (DNS/HTTP), but the keychain key built from the host
// (KeychainService) is not, so a mixed-case host that survives unchanged
// through ParseURL spuriously fails "no credentials found" against a
// lower-case key stored at login.
func TestParseURLCaseInsensitiveHost(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"HTTPS://LEET.CONDUCTOR.ONE", "https://leet.conductor.one"},
		{"LEET.CONDUCTOR.ONE", "https://leet.conductor.one"},
		{"ACME", "https://acme.conductor.one"},
	}
	for _, tc := range cases {
		got, warnings := ParseURL(tc.input)
		if got != tc.want {
			t.Errorf("ParseURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
		if len(warnings) != 0 {
			t.Errorf("ParseURL(%q) warnings = %v, want none (case alone isn't a warning-worthy rewrite)", tc.input, warnings)
		}
	}
}

// TestParseURLProtocolRelative is sub-issue (b): "//host" is a plausible
// typo for "https://host". Before the fix it missed the "://" fast path and
// fell through to the raw-domain branch, which prepended "https://" onto the
// literal leading "//" and produced "https:////host".
func TestParseURLProtocolRelative(t *testing.T) {
	got, _ := ParseURL("//leet.conductor.one")
	if want := "https://leet.conductor.one"; got != want {
		t.Errorf("ParseURL(%q) = %q, want %q", "//leet.conductor.one", got, want)
	}
}

// TestParseURLNonHTTPSSchemeWarns is sub-issue (a), part 1: a non-https
// scheme is rewritten to https (a hard reject would turn a typo into a hard
// failure with no way to inspect what was actually sent), but silently is
// no longer acceptable -- the caller must be told.
func TestParseURLNonHTTPSSchemeWarns(t *testing.T) {
	got, warnings := ParseURL("ftp://host.example.com")
	if want := "https://host.example.com"; got != want {
		t.Errorf("ParseURL(%q) = %q, want %q", "ftp://host.example.com", got, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "ftp") {
		t.Errorf("warnings = %v, want one mentioning the rejected scheme", warnings)
	}
}

// TestParseURLDropsEmbeddedCredentialsWithWarning is sub-issue (a), part 2:
// embedded userinfo is dropped (unchanged -- c1i has no way to send HTTP
// Basic credentials through its OAuth-based client), but that drop must not
// be silent, and the warning must not leak the password.
func TestParseURLDropsEmbeddedCredentialsWithWarning(t *testing.T) {
	got, warnings := ParseURL("https://user:hunter2@host.example.com")
	if want := "https://host.example.com"; got != want {
		t.Errorf("ParseURL(%q) = %q, want %q", "https://user:hunter2@host.example.com", got, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "user") {
		t.Errorf("warnings = %v, want one mentioning the dropped credentials", warnings)
	}
	for _, w := range warnings {
		if strings.Contains(w, "hunter2") {
			t.Errorf("warning leaked the password: %q", w)
		}
	}
}

// TestKeychainServiceLowerCasesHost pins that KeychainService itself is also
// insensitive to input case, independent of whether the caller already
// normalized via ParseURL -- defense in depth so a bypassed ParseURL call
// can't silently reintroduce sub-issue (c).
func TestKeychainServiceLowerCasesHost(t *testing.T) {
	got := KeychainService("https://LEET.CONDUCTOR.ONE")
	want := "c1i/leet.conductor.one"
	if got != want {
		t.Errorf("KeychainService = %q, want %q", got, want)
	}
}
