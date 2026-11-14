package redact

import (
	"strings"
	"testing"
)

func TestURLMasksSecretsAndKeepsTheRest(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		leaks []string // must not appear
		keeps []string // must still appear
	}{
		{"url secret query name spellings", "https://h.example/x?api_key=Aleak&Access-Token=Bleak&ok=2", []string{"Aleak", "Bleak"}, []string{"ok=2"}},
		{"semicolon query and fragment", "https://h.example/x?a=1;token=Q1leak&b=2#access_token=Q2leak&c=3", []string{"Q1leak", "Q2leak"}, []string{"a=1", "b=2", "c=3"}},
		{"malformed url keeps no userinfo", "https://us er:PWleak@h.example/x?api_key=Q3leak", []string{"PWleak", "Q3leak"}, nil},
		{"scheme-less userinfo", "user:PWleak@h.example/x?secret=Q4leak&z=9", []string{"PWleak", "Q4leak"}, []string{"z=9"}},
		{"username-only userinfo (PAT)", "https://ghp_SU1leak@github.example/x", []string{"ghp_SU1leak"}, []string{"github.example/x"}},
		{"username-only userinfo, malformed", "https://ghp_SU2leak@gith ub.example/x", []string{"ghp_SU2leak"}, nil},
		{"scheme-relative userinfo", "//u:PW3leak@h.example/x", []string{"PW3leak"}, []string{"h.example/x"}},
		{"key/sig/signature params", "https://h.example/x?key=P1leak&sig=P2leak&Signature=P3leak&X-Amz-Signature=P4leak&ok=1", []string{"P1leak", "P2leak", "P3leak", "P4leak"}, []string{"ok=1"}},
		{"semicolon before query", "https://h.example/p;token=M1leak?a=1", []string{"M1leak"}, nil},
		{"? inside a secret value", "https://h.example/x?token=abc?defleak", []string{"abc?defleak", "defleak"}, nil},
		{"? inside a secret value, neighbours kept", "https://h.example/x?a=1&token=x?yleak&b=2", []string{"yleak"}, []string{"a=1", "b=2"}},
		{"? inside a secret value, fragment", "https://h.example/x#a=1&token=x?zleak&b=2", []string{"zleak"}, []string{"a=1", "b=2"}},
		{"non-secret a=b=c unchanged", "https://h.example/x?a=b=c&d=1", nil, []string{"a=b=c", "d=1"}},
		{"path-only semicolon param", "https://h.example/p;api_key=M2leak/x", []string{"M2leak"}, nil},
		{"scheme-less token@host", "TOKENleak@h.example/p", []string{"TOKENleak"}, []string{"h.example/p"}},
		{"; inside a query value", "https://h.example/x?token=a;bleak&ok=1", []string{"bleak"}, []string{"ok=1"}},
		{"# inside a query value", "https://h.example/x?token=a#cleak", []string{"cleak"}, nil},
		{"percent-encoded pair", "https://h.example/x?token%3Dpleak&ok=1", []string{"pleak"}, []string{"ok=1"}},
		{"; separated pair after a plain one", "https://h.example/x?a=1;token=sleak", []string{"sleak"}, []string{"a=1"}},
		{"userinfo and query", "https://user:PWleak@h.example/x?token=QTleak&keep=1", []string{"PWleak", "QTleak"}, []string{"h.example/x", "keep=1", "user"}},
		{"git@ is not a secret", "ssh://git@github.example/org/repo", nil, []string{"ssh://git@github.example/org/repo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := URL(tc.url)
			for _, l := range tc.leaks {
				if strings.Contains(got, l) {
					t.Errorf("URL(%q) = %q leaked %q", tc.url, got, l)
				}
			}
			for _, k := range tc.keeps {
				if !strings.Contains(got, k) {
					t.Errorf("URL(%q) = %q lost %q", tc.url, got, k)
				}
			}
		})
	}
}

// One sentinel: a URL password prints as [redacted], not url.Redacted's xxxxx.
func TestURLPasswordUsesTheSingleSentinel(t *testing.T) {
	got := URL("https://user:PWleak@h.example/x")
	if strings.Contains(got, "xxxxx") || strings.Contains(got, "PWleak") || !strings.Contains(got, "[redacted]") {
		t.Errorf("URL = %q", got)
	}
}
