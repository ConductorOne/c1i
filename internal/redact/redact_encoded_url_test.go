package redact

import (
	"strings"
	"testing"
)

// URL is the masking applied to request URLs in --dry-run previews, --debug
// traces and transport/redirect/path errors. It must apply the same
// encoded-value rule Text does: a secret inside a percent-encoded value.
func TestURLMasksSecretsNestedInEncodedValues(t *testing.T) {
	cases := []struct {
		name, in string
		leaks    []string
		keeps    []string
	}{
		{
			"encoded URL nested in a query value",
			"https://h.example/x?next=https%3A%2F%2Fh%2Fx%3Ftoken%3DSEK1&ok=1",
			[]string{"SEK1"},
			[]string{"ok=1", "next="},
		},
		{
			"double-encoded URL nested in a query value",
			"https://h.example/x?next=https%253A%252F%252Fh%252Fx%253Ftoken%253DSEK2&ok=1",
			[]string{"SEK2"},
			[]string{"ok=1"},
		},
		{
			"encoded path parameter with an encoded separator",
			"https://h.example/x;token%3DSEK3/y?ok=1",
			[]string{"SEK3"},
			[]string{"/y?ok=1"},
		},
		{
			"encoded URL nested in a path parameter value",
			"https://h.example/x;next=https%3A%2F%2Fh%2Fx%3Ftoken%3DSEK4",
			[]string{"SEK4"},
			[]string{"next="},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := URL(tc.in)
			for _, l := range tc.leaks {
				if strings.Contains(got, l) {
					t.Errorf("URL(%q) = %q leaked %q", tc.in, got, l)
				}
			}
			for _, k := range tc.keeps {
				if !strings.Contains(got, k) {
					t.Errorf("URL(%q) = %q lost %q", tc.in, got, k)
				}
			}
		})
	}
}

// Path masks the request path in error text and previews; the same encoded
// forms must not survive it.
func TestPathMasksSecretsNestedInEncodedValues(t *testing.T) {
	for _, in := range []string{
		"/api/v1/x;token%3DSEK5/y",
		"/api/v1/x;%74oken%3DSEK6/y",
		"/api/v1/x;next=https%3A%2F%2Fh%2Fx%3Ftoken%3DSEK7",
	} {
		if got := Path(in); strings.Contains(got, "SEK") {
			t.Errorf("Path(%q) = %q leaked a secret", in, got)
		}
	}
}

// A path or URL with nothing secret stays byte for byte.
func TestURLAndPathLeaveInnocentEncodedValuesAlone(t *testing.T) {
	for _, in := range []string{
		"/api/v1/x;filter%3Dname%20eq%20%22bob%22",
		"/api/v1/x;sort=name%3Dtoken",
		"/api/v1/x;a%3Db=c",
	} {
		if got := URL(in); got != in {
			t.Errorf("URL(%q) = %q, want unchanged", in, got)
		}
		if got := Path(in); got != in {
			t.Errorf("Path(%q) = %q, want unchanged", in, got)
		}
	}
	if in := "https://h.example/x?next=%2Fy%3Fa%3D1%26k%3D2&ok=1"; URL(in) != in {
		t.Errorf("URL(%q) = %q, want unchanged", in, URL(in))
	}
}

// The encoded-separator scan must not use a case-mapping that changes byte
// length: ı (U+0131) upper-cases to I, so an index taken from strings.ToUpper
// would miss the %3D and leave the value in clear.
func TestURLMasksEncodedSeparatorAfterNonASCIIBytes(t *testing.T) {
	for _, in := range []string{
		"/x;ıtoken%3DSEK8/y",
		"/x;ſtoken%3DSEK9/y",
	} {
		if got := Path(in); strings.Contains(got, "SEK") {
			t.Errorf("Path(%q) = %q leaked a secret", in, got)
		}
	}
}
