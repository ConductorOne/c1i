package transport

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// redirectOnlyTripper answers every request with the configured 3xx and
// Location, so redirectTripper's refusal path runs without any network hop.
type redirectOnlyTripper struct {
	status   int
	location string
}

func (t *redirectOnlyTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: t.status,
		Header:     http.Header{"Location": {t.location}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// TestRedirectErrorMessageNamesRule checks that a refusal names the rule that
// actually fired: a host-only refusal must not claim the path changed, and a
// scheme downgrade must say so.
func TestRedirectErrorMessageNamesRule(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		location string
		want     string
		notWant  []string
	}{
		{
			name:     "changed path",
			base:     "https://api.tenant.example/x",
			location: "/y",
			want:     "changes the request path",
			notWant:  []string{"host outside", "downgrades"},
		},
		{
			name:     "host-only change",
			base:     "https://api.tenant.example/x",
			location: "https://evil.example/x",
			want:     "host outside the request host's trust scope",
			notWant:  []string{"changes the request path", "downgrades"},
		},
		{
			name:     "https to http downgrade",
			base:     "https://api.tenant.example/x",
			location: "http://api.tenant.example/x",
			want:     "takes an https request to a non-https target",
			notWant:  []string{"changes the request path", "host outside"},
		},
		{
			name:     "empty location",
			base:     "https://api.tenant.example/x",
			location: "",
			want:     "empty Location",
			notWant:  []string{"changes the request path", "host outside", "downgrades"},
		},
		{
			name:     "unparseable location",
			base:     "https://api.tenant.example/x",
			location: "https://ex ample.test/x",
			want:     "cannot be parsed or resolved",
			notWant:  []string{"changes the request path", "host outside", "downgrades"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, err := url.Parse(tc.base)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(http.MethodGet, base.String(), nil)
			if err != nil {
				t.Fatal(err)
			}
			rt := &redirectTripper{next: &redirectOnlyTripper{status: http.StatusMovedPermanently, location: tc.location}}
			_, err = rt.RoundTrip(req)
			if err == nil {
				t.Fatalf("RoundTrip(%s -> %q) succeeded, want a refusal", tc.base, tc.location)
			}
			var redirErr *RedirectError
			if !errors.As(err, &redirErr) {
				t.Fatalf("error = %T (%v), want *RedirectError", err, err)
			}
			msg := redirErr.Error()
			if !strings.Contains(msg, tc.want) {
				t.Errorf("message %q does not name the rule (%q)", msg, tc.want)
			}
			for _, bad := range tc.notWant {
				if strings.Contains(msg, bad) {
					t.Errorf("message %q wrongly names another rule (%q)", msg, bad)
				}
			}
			// The structured message must keep its shape and redact nothing
			// away here: method, status, Location and the rule sentence.
			for _, part := range []string{"refusing to follow redirect: GET", "301"} {
				if !strings.Contains(msg, part) {
					t.Errorf("message %q is missing %q", msg, part)
				}
			}
		})
	}
}

// TestRedirectErrorUnknownReasonNamesEveryRule pins the fallback used when an
// error is assembled from its structured fields without a Reason: it must not
// claim a single rule fired.
func TestRedirectErrorUnknownReasonNamesEveryRule(t *testing.T) {
	msg := (&RedirectError{Method: "GET", URL: "https://x/x", StatusCode: 301, Location: "/y"}).Error()
	if !strings.Contains(msg, "changes the request path, host or scheme") {
		t.Errorf("fallback message = %q, want all three rules named", msg)
	}
}

// TestResolveAllowedRedirectReportsReason checks the reason the guard pairs
// with each refusal, including that an allowed hop reports none.
func TestResolveAllowedRedirectReportsReason(t *testing.T) {
	base, err := url.Parse("https://api.tenant.example/x")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		location string
		want     RedirectReason
	}{
		{"changed path", "/y", RedirectReasonPath},
		{"host-only change", "https://evil.example/x", RedirectReasonHost},
		{"https to http downgrade", "http://api.tenant.example/x", RedirectReasonScheme},
		{"empty location", "", RedirectReasonEmptyLocation},
		{"unparseable location", "https://ex ample.test/x", RedirectReasonBadLocation},
		{"allowed same-host hop", "https://api.tenant.example/x?q=1", RedirectReasonUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, reason, ok := resolveAllowedRedirect(base, tc.location)
			if reason != tc.want {
				t.Errorf("resolveAllowedRedirect(%q) reason = %d, want %d", tc.location, reason, tc.want)
			}
			if ok != (tc.want == RedirectReasonUnknown) {
				t.Errorf("resolveAllowedRedirect(%q) ok = %v, reason = %d", tc.location, ok, reason)
			}
		})
	}
}
