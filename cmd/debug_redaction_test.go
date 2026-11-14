package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/redact"
	"github.com/spf13/cobra"
)

const debugSecret = "SEKR3T"

// --debug traces and transport errors apply the same URL redaction as a
// --dry-run preview: a secret-named parameter is masked, the rest stays.
func TestDebugTraceAndTransportErrorRedactURL(t *testing.T) {
	const path = "/api/v1/x?token=" + debugSecret + "&visible=1"
	for _, format := range []string{"text", "json"} {
		t.Run("trace/"+format, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, `{}`))
			t.Setenv("C1I_URL", mutTestURL)
			var r mutResult
			trace := captureStderr(t, func() {
				r = runMutationCLI(t, s.srv, retriesFromConfig, nil, "api", "--path", path, "--debug", "--error-format", format)
			})
			if r.err != nil {
				t.Fatal(r.err)
			}
			if strings.Contains(trace+r.stderr, debugSecret) {
				t.Errorf("secret leaked:\n%s%s", trace, r.stderr)
			}
			if !strings.Contains(trace, "token=[redacted]") || !strings.Contains(trace, "visible=1") {
				t.Errorf("trace = %q, want token=[redacted] and visible=1", trace)
			}
		})
		t.Run("error/"+format, func(t *testing.T) {
			s := newMutServer(t, func(*http.Request, int) mutReply { return mutReply{hijack: true} })
			t.Setenv("C1I_URL", mutTestURL)
			var r mutResult
			trace := captureStderr(t, func() {
				r = runMutationCLI(t, s.srv, 0, nil, "api", "--path", path, "--debug", "--error-format", format)
			})
			if r.err == nil {
				t.Fatal("want a transport error")
			}
			if strings.Contains(trace+r.stderr, debugSecret) {
				t.Errorf("secret leaked:\n%s%s", trace, r.stderr)
			}
		})
	}
	t.Run("dial error", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, closedPortServer(t), 0, nil, "api", "--path", path)
		if r.err == nil || strings.Contains(r.stderr, debugSecret) || !strings.Contains(r.stderr, "visible=1") {
			t.Errorf("err %v stderr %q: want an error without the secret and with visible=1", r.err, r.stderr)
		}
	})
}

// The partial-result notice and the pagination guard's INCOMPLETE text echo the
// --path; a secret query value stays masked, in both stderr formats.
func TestApiNoticesAndGuardErrorsRedactPath(t *testing.T) {
	const path = "/api/v1/apps?token=" + debugSecret + "&visible=1"
	for _, format := range []string{"text", "json"} {
		t.Run("partial/"+format, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, `{"list":[{"id":"1"}],"nextPageToken":"abc"}`))
			t.Setenv("C1I_URL", mutTestURL)
			var r mutResult
			trace := captureStderr(t, func() {
				r = runMutationCLI(t, s.srv, 0, nil, "api", "--path", path, "--error-format", format)
			})
			if r.err != nil {
				t.Fatal(r.err)
			}
			all := trace + r.stderr
			if !strings.Contains(all, "partial result") || strings.Contains(all, debugSecret) || !strings.Contains(all, "visible=1") {
				t.Errorf("stderr = %q", all)
			}
		})
		t.Run("stuck cursor/"+format, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, `{"list":[{"id":"1"}],"nextPageToken":"same"}`))
			t.Setenv("C1I_URL", mutTestURL)
			var r mutResult
			trace := captureStderr(t, func() {
				r = runMutationCLI(t, s.srv, 0, nil, "api", "--path", path, "--paginate", "--error-format", format)
			})
			if r.err == nil {
				t.Fatal("want the stuck-cursor error")
			}
			all := trace + r.stderr
			if !strings.Contains(all, "INCOMPLETE") || strings.Contains(all, debugSecret) || !strings.Contains(all, "visible=1") {
				t.Errorf("stderr = %q", all)
			}
		})
	}
	t.Run("missing slash", func(t *testing.T) {
		// Without a URL the "url is required" check fires first.
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, nil, 0, nil, "api", "--path", "api/v1/x?token="+debugSecret+"&visible=1")
		if r.exit != exitUsage || strings.Contains(r.stderr, debugSecret) || !strings.Contains(r.stderr, "visible=1") {
			t.Errorf("exit %d stderr %q", r.exit, r.stderr)
		}
	})
	t.Run("destination-url", func(t *testing.T) {
		if err := validateWebhookURL("http://h.example/x?token=" + debugSecret + "&visible=1"); err == nil || strings.Contains(err.Error(), debugSecret) || !strings.Contains(err.Error(), "visible=1") {
			t.Errorf("err = %v", err)
		}
	})
}

// A request that cannot be built echoes its URL; the secret is masked, the
// rest stays, and the exit code is still usage (2).
func TestUnbuildableRequestErrorRedactsURL(t *testing.T) {
	withDryRun(t)
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, nil, 0, nil, "api", "--method", "POST", "--path", "/api/v1/x?token="+debugSecret+"&visible=1\x01", "--body", "{}")
	if r.exit != exitUsage || r.err == nil {
		t.Fatalf("exit %d err %v, want usage", r.exit, r.err)
	}
	if strings.Contains(r.stderr, debugSecret) || !strings.Contains(r.stderr, "visible=1") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

// A malformed --url is rejected before any request; its secrets stay masked.
func TestMalformedURLFlagErrorRedactsSecrets(t *testing.T) {
	t.Setenv("C1I_URL", "")
	r := runMutationCLI(t, nil, 0, nil, "--url", "acme?token="+debugSecret+"&visible=1", "api", "--path", "/api/v1/x")
	if r.exit != exitUsage || r.err == nil {
		t.Fatalf("exit %d err %v, want usage", r.exit, r.err)
	}
	if strings.Contains(r.stderr, debugSecret) || !strings.Contains(r.stderr, "visible=1") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

// ";" path parameters, plain or %3B-encoded, are masked wherever a path is
// echoed: debug lines, the API error text and its JSON "path", the path guard.
func TestPathParamSecretsNeverReachStderr(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{"semicolon", "/api/v1/x;token=SEKA1/y?ok=1"},
		{"encoded", "/api/v1/x%3Btoken=SEKA2?ok=1"},
		{"encoded key", "/api/v1/x;%74oken=SEKA3/y?ok=1"},
	} {
		for _, format := range []string{"text", "json"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				s := newMutServer(t, fixedReply(404, `{}`))
				t.Setenv("C1I_URL", mutTestURL)
				var r mutResult
				trace := captureStderr(t, func() {
					r = runMutationCLI(t, s.srv, 0, nil, "api", "--path", tc.path, "--debug", "--error-format", format)
				})
				if r.err == nil {
					t.Fatal("want a 404")
				}
				if all := trace + r.stderr; strings.Contains(all, "SEKA") || !strings.Contains(all, "[redacted]") {
					t.Errorf("stderr = %q", all)
				}
			})
		}
	}
	t.Run("path guard", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{}`))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "api", "--path", "/api/v1/./x;token=SEKG1")
		if r.exit != exitUsage || strings.Contains(r.stderr, "SEKG1") {
			t.Errorf("exit %d stderr %q", r.exit, r.stderr)
		}
	})
	t.Run("GET that cannot be built", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		s := newMutServer(t, fixedReply(200, `{}`))
		r := runMutationCLI(t, s.srv, 0, nil, "api", "--path", "/x?token=SEKH1&ok=1\x01")
		if r.err == nil || strings.Contains(r.stderr, "SEKH1") || !strings.Contains(r.stderr, "ok=1") {
			t.Errorf("err %v stderr %q", r.err, r.stderr)
		}
	})
}

// A ";" parameter value that percent-decodes into a separator ("/", "?", "#",
// space) is masked whole: the path is masked in its escaped form.
func TestEscapedSeparatorsInPathParamStayMasked(t *testing.T) {
	leaks := []string{"AbC", "dEf", "SEKa", "SEKb", "SEKc", "SEKd", "SEKe"}
	run := func(t *testing.T, srv *httptest.Server, format string, args ...string) string {
		t.Setenv("C1I_URL", mutTestURL)
		var r mutResult
		trace := captureStderr(t, func() {
			r = runMutationCLI(t, srv, 0, nil, append(args, "--debug", "--error-format", format)...)
		})
		if r.err == nil {
			t.Fatal("want an error")
		}
		return trace + r.stderr
	}
	for _, format := range []string{"text", "json"} {
		t.Run("api 404/"+format, func(t *testing.T) {
			s := newMutServer(t, fixedReply(404, `{}`))
			all := run(t, s.srv, format, "api", "--path", "/api/v1/x;token=AbC%2FdEf%2B1==/y")
			for _, l := range leaks {
				if strings.Contains(all, l) {
					t.Errorf("leaked %q: %s", l, all)
				}
			}
			if !strings.Contains(all, "/y") {
				t.Errorf("lost the visible path: %s", all)
			}
		})
		t.Run("apps get 404/"+format, func(t *testing.T) {
			s := newMutServer(t, fixedReply(404, `{}`))
			all := run(t, s.srv, format, "apps", "get", "x;token=SEKa/SEKb?SEKc#SEKd SEKe")
			for _, l := range leaks {
				if strings.Contains(all, l) {
					t.Errorf("leaked %q: %s", l, all)
				}
			}
		})
		t.Run("connection error/"+format, func(t *testing.T) {
			all := run(t, closedPortServer(t), format, "apps", "get", "x;token=SEKa/SEKb?SEKc#SEKd SEKe")
			for _, l := range leaks {
				if strings.Contains(all, l) {
					t.Errorf("leaked %q: %s", l, all)
				}
			}
		})
	}
}

// The fixed-route list's partial-result notice and the gateway URL error echo
// a path / URL; both mask it.
func TestFindingsNoticeAndGatewayErrorRedact(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"list":[],"nextPageToken":"abc"}`))
	stubMutationClient(t, s.srv, 0)
	var errBuf bytes.Buffer
	cmd := &cobra.Command{Use: "x"}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errBuf)
	cmd.SetContext(t.Context())
	trace := captureStderr(t, func() {
		if err := getFixedList(cmd, "/api/v1/x;token=SEKF1?ok=1", func(m map[string]any) map[string]any { return m }); err != nil {
			t.Error(err)
		}
	})
	if all := trace + errBuf.String(); !strings.Contains(all, "partial result") || strings.Contains(all, "SEKF1") {
		t.Errorf("notice = %q", all)
	}
	if _, err := deriveGatewayURL("acme?token=SEKL1&ok=1"); err == nil || strings.Contains(err.Error(), "SEKL1") || !strings.Contains(err.Error(), "ok=1") {
		t.Errorf("err = %v", err)
	}
}

// cmd's own secret list must stay inside the core one the redactor owns.
func TestRedactorCoversMCPAuthKeys(t *testing.T) {
	for _, k := range []string{keyHeaderValue, keyCredentialsJSON, keySecretAccessKey} {
		if !redact.IsSecretKey(k) {
			t.Errorf("%q is not treated as secret", k)
		}
	}
}
