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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings, err := ParseURL(tc.input)
			if err != nil {
				t.Fatalf("ParseURL(%q) error = %v, want nil", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if len(warnings) != 0 {
				t.Errorf("ParseURL(%q) warnings = %v, want none", tc.input, warnings)
			}
		})
	}
}

// TestParseURLCaseInsensitiveHost: a legitimate but differently-cased --url
// must not be rejected. Hosts are
// case-insensitive (DNS/HTTP), but the keychain key built from the host
// (KeychainService) is not, so a mixed-case host that survives unchanged
// through ParseURL spuriously fails "no credentials found" against a
// lower-case key stored at login.
func TestParseURLCaseInsensitiveHost(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"HTTPS://ACME.CONDUCTOR.ONE", "https://acme.conductor.one"},
		{"ACME.CONDUCTOR.ONE", "https://acme.conductor.one"},
		// A second tenant domain family (EU) must normalize identically --
		// this fix is about URL shape, never about which domain a host
		// belongs to.
		{"HTTPS://ACME.C1EU.AI", "https://acme.c1eu.ai"},
		{"ACME.C1EU.AI", "https://acme.c1eu.ai"},
	}
	for _, tc := range cases {
		got, warnings, err := ParseURL(tc.input)
		if err != nil {
			t.Fatalf("ParseURL(%q) error = %v, want nil", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("ParseURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
		if len(warnings) != 0 {
			t.Errorf("ParseURL(%q) warnings = %v, want none (case alone isn't a warning-worthy rewrite)", tc.input, warnings)
		}
	}
}

// TestParseURLBareTokenIsError covers the retired shortcut: a bare token (no
// "://" and no ".") used to silently expand to "<input>.conductor.one",
// which is now ambiguous with *.c1eu.ai. It must be refused, with a message
// that is actionable on its own: it names the rejected input, shows both
// domain families, and calls out that local development needs an explicit
// scheme.
func TestParseURLBareTokenIsError(t *testing.T) {
	for _, in := range []string{"acme", "mycompany", "localhost", "localhost:8080"} {
		t.Run(in, func(t *testing.T) {
			got, warnings, err := ParseURL(in)
			if err == nil {
				t.Fatalf("ParseURL(%q) error = nil, want an error (bare short names are retired)", in)
			}
			if got != "" {
				t.Errorf("ParseURL(%q) result = %q, want empty on error", in, got)
			}
			if len(warnings) != 0 {
				t.Errorf("ParseURL(%q) warnings = %v, want none on error", in, warnings)
			}
			msg := err.Error()
			for _, want := range []string{in, "conductor.one", "c1eu.ai"} {
				if !strings.Contains(msg, want) {
					t.Errorf("ParseURL(%q) error = %q, want it to mention %q", in, msg, want)
				}
			}
			// The bare-name error used to close with local-dev advice to use
			// an explicit http:// scheme -- that advice never worked (http
			// was silently coerced to https) and is now plainly wrong (http
			// is rejected outright). It must not reappear.
			if strings.Contains(msg, "http") {
				t.Errorf("ParseURL(%q) error = %q, must not mention http (no supported plain-http path)", in, msg)
			}
		})
	}
}

// TestParseURLIPv4LoopbackStillWorks: unlike "localhost", "127.0.0.1" and
// "127.0.0.1:8080" contain a dot, so they already take the raw-domain
// branch today and must be unaffected by retiring the bare-token branch.
func TestParseURLIPv4LoopbackStillWorks(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1":      "https://127.0.0.1",
		"127.0.0.1:8080": "https://127.0.0.1:8080",
	}
	for in, want := range cases {
		got, warnings, err := ParseURL(in)
		if err != nil {
			t.Fatalf("ParseURL(%q) error = %v, want nil", in, err)
		}
		if got != want {
			t.Errorf("ParseURL(%q) = %q, want %q", in, got, want)
		}
		if len(warnings) != 0 {
			t.Errorf("ParseURL(%q) warnings = %v, want none", in, warnings)
		}
	}
}

// TestParseURLHTTPSchemeIsRejected: c1i requires https, full stop -- an
// explicit http:// scheme is no longer a working local-dev escape hatch (it
// used to be silently coerced to https; that coercion is gone) and there is
// no loopback/local exception.
func TestParseURLHTTPSchemeIsRejected(t *testing.T) {
	got, warnings, err := ParseURL("http://localhost:8080")
	if err == nil {
		t.Fatalf("ParseURL(%q) error = nil, want an error (c1i requires https)", "http://localhost:8080")
	}
	if got != "" {
		t.Errorf("ParseURL(%q) result = %q, want empty on error", "http://localhost:8080", got)
	}
	if len(warnings) != 0 {
		t.Errorf("ParseURL(%q) warnings = %v, want none on error", "http://localhost:8080", warnings)
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("ParseURL(%q) error = %q, want it to name the https requirement", "http://localhost:8080", err.Error())
	}
}

// TestParseURLProtocolRelative: "//host" is a plausible typo for
// "https://host". Before the fix it missed the "://" fast path and
// fell through to the raw-domain branch, which prepended "https://" onto the
// literal leading "//" and produced "https:////host".
func TestParseURLProtocolRelative(t *testing.T) {
	got, _, err := ParseURL("//acme.conductor.one")
	if err != nil {
		t.Fatalf("ParseURL(%q) error = %v, want nil", "//acme.conductor.one", err)
	}
	if want := "https://acme.conductor.one"; got != want {
		t.Errorf("ParseURL(%q) = %q, want %q", "//acme.conductor.one", got, want)
	}
}

// TestParseURLNonHTTPSSchemeIsError: c1i requires https -- a non-https
// scheme (this used to be silently rewritten to https with a warning) is
// now a hard error naming the https requirement, not a silent upgrade.
func TestParseURLNonHTTPSSchemeIsError(t *testing.T) {
	for _, in := range []string{"http://host.example.com", "ftp://host.example.com"} {
		t.Run(in, func(t *testing.T) {
			got, warnings, err := ParseURL(in)
			if err == nil {
				t.Fatalf("ParseURL(%q) error = nil, want an error (c1i requires https)", in)
			}
			if got != "" {
				t.Errorf("ParseURL(%q) result = %q, want empty on error", in, got)
			}
			if len(warnings) != 0 {
				t.Errorf("ParseURL(%q) warnings = %v, want none on error", in, warnings)
			}
			if !strings.Contains(err.Error(), "https") {
				t.Errorf("ParseURL(%q) error = %q, want it to name the https requirement", in, err.Error())
			}
		})
	}
}

// TestParseURLDropsEmbeddedCredentialsWithWarning: embedded userinfo is
// dropped (unchanged -- c1i has no way to send HTTP
// Basic credentials through its OAuth-based client), but that drop must not
// be silent, and the warning must not leak the password.
func TestParseURLDropsEmbeddedCredentialsWithWarning(t *testing.T) {
	got, warnings, err := ParseURL("https://user:hunter2@host.example.com")
	if err != nil {
		t.Fatalf("ParseURL(%q) error = %v, want nil", "https://user:hunter2@host.example.com", err)
	}
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

// TestParseURLDropsEmbeddedCredentialsSchemeless: the scheme-having branch
// above drops/warns about embedded
// userinfo, but a scheme-LESS input ("user:pass@host", the ordinary mistake
// of pasting a URL and forgetting "https://") took the raw-domain branch
// untouched -- nothing dropped, nothing warned, and the password rode
// straight into the base URL c1i then sends on every request (visible in
// --debug's request trace and in a failed-auth error). Reproduced live:
// "c1i users list --url \"user:hunter2@acme.conductor.one\" --debug" printed
// the password three times in stderr before this fix.
func TestParseURLDropsEmbeddedCredentialsSchemeless(t *testing.T) {
	got, warnings, err := ParseURL("user:hunter2@acme.conductor.one")
	if err != nil {
		t.Fatalf("ParseURL(%q) error = %v, want nil", "user:hunter2@acme.conductor.one", err)
	}
	if want := "https://acme.conductor.one"; got != want {
		t.Errorf("ParseURL(%q) = %q, want %q", "user:hunter2@acme.conductor.one", got, want)
	}
	if strings.Contains(got, "hunter2") {
		t.Errorf("result leaked the password: %q", got)
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
// can't silently reintroduce the case-sensitivity bug.
func TestKeychainServiceLowerCasesHost(t *testing.T) {
	got := KeychainService("https://ACME.CONDUCTOR.ONE")
	want := "c1i/acme.conductor.one"
	if got != want {
		t.Errorf("KeychainService = %q, want %q", got, want)
	}
}

// TestLegacyKeychainCredentialNotOrphanedByShortcutRetirement: a
// user who previously ran "--url acme" (when that expanded to
// "https://acme.conductor.one") and stored a credential must still resolve
// it once the shortcut is retired and they type the full host instead.
// Both KeychainService and LegacyKeychainService derive their key from the
// RESOLVED base URL, never from what the user originally typed, so they are
// unaffected by ParseURL's bare-token branch being removed -- this pins that
// invariant.
func TestLegacyKeychainCredentialNotOrphanedByShortcutRetirement(t *testing.T) {
	// What the old shortcut used to resolve "acme" to.
	oldExpansion := "https://acme.conductor.one"
	// What a user must now type instead.
	newFull, _, err := ParseURL("acme.conductor.one")
	if err != nil {
		t.Fatalf("ParseURL(%q) error = %v, want nil", "acme.conductor.one", err)
	}
	if newFull != oldExpansion {
		t.Fatalf("ParseURL(%q) = %q, want %q (must match what the retired shortcut used to produce)", "acme.conductor.one", newFull, oldExpansion)
	}
	if got, want := KeychainService(newFull), KeychainService(oldExpansion); got != want {
		t.Errorf("KeychainService(%q) = %q, want %q (same as the old shortcut's expansion)", newFull, got, want)
	}
	if got, want := LegacyKeychainService(newFull), LegacyKeychainService(oldExpansion); got != want {
		t.Errorf("LegacyKeychainService(%q) = %q, want %q (same as the old shortcut's expansion)", newFull, got, want)
	}
}
