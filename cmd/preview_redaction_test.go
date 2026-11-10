package cmd

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const previewSecret = "tok-should-never-print"

// Every --dry-run preview path must mask secret keys; a new path that skips
// writeMutationPreview fails here.
func TestEveryPreviewPathRedactsSecrets(t *testing.T) {
	secretBody := func() map[string]any {
		return map[string]any{"name": "visible", "bearerToken": map[string]any{"token": previewSecret}}
	}
	direct := func(t *testing.T, fn func(cmd *cobra.Command) error) (string, error) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		var out bytes.Buffer
		cmd := &cobra.Command{Use: "x"}
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetContext(t.Context())
		err := fn(cmd)
		return out.String(), err
	}
	check := func(t *testing.T, out string) {
		t.Helper()
		if strings.Contains(out, previewSecret) {
			t.Errorf("preview leaked the secret:\n%s", out)
		}
		if !strings.Contains(out, "[redacted]") || !strings.Contains(out, "visible") {
			t.Errorf("preview should mask only the secret:\n%s", out)
		}
	}
	t.Run("sendMutation", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		var out bytes.Buffer
		cmd := &cobra.Command{Use: "x"}
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		if err := sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/x", Body: secretBody()}); err != nil {
			t.Fatal(err)
		}
		check(t, out.String())
	})
	t.Run("sendMutationSequence", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		var out bytes.Buffer
		cmd := &cobra.Command{Use: "x"}
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		steps := []mutationStep{{Build: func([]string) mutation {
			return mutation{Method: http.MethodPost, Path: "/api/v1/x", Body: secretBody()}
		}}}
		if err := sendMutationSequence(cmd, steps, ""); err != nil {
			t.Fatal(err)
		}
		check(t, out.String())
	})
	t.Run("sendMutationBatch", func(t *testing.T) {
		out, err := direct(t, func(cmd *cobra.Command) error {
			ms := []mutation{{Method: http.MethodPost, Path: "/api/v1/x", Body: secretBody()}}
			return sendMutationBatch(cmd, ms, "id", func(int) string { return "1" })
		})
		if err != nil {
			t.Fatal(err)
		}
		check(t, out)
	})
	t.Run("api", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{}`))
		t.Setenv("C1I_URL", mutTestURL)
		withDryRun(t)
		r := runMutationCLI(t, s.srv, 0, nil, "api", "--method", "POST", "--path", "/api/v1/x",
			"--body", `{"name":"visible","bearer_token":{"token":"`+previewSecret+`"}}`)
		if r.err != nil {
			t.Fatal(r.err)
		}
		check(t, r.stdout)
	})
	t.Run("register", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{}`))
		t.Setenv("C1I_URL", mutTestURL)
		withDryRun(t)
		r := runMutationCLI(t, s.srv, 0, nil, "mcp", "servers", "register", "--app-id=app1", "--type=external",
			"--display-name=visible", "--server-url=https://mcp.example.com/sse", "--auth=bearer-token", "--bearer-token="+previewSecret)
		if r.err != nil {
			t.Fatal(r.err)
		}
		check(t, r.stdout)
	})
	t.Run("probe", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{}`))
		t.Setenv("C1I_URL", mutTestURL)
		withDryRun(t)
		r := runMutationCLI(t, s.srv, 0, nil, "mcp", "servers", "test-connection",
			"--server-url=https://mcp.example.com/visible", "--auth=bearer-token", "--bearer-token="+previewSecret)
		if r.err != nil {
			t.Fatal(r.err)
		}
		if strings.Contains(r.stdout, previewSecret) || !strings.Contains(r.stdout, "[redacted]") {
			t.Errorf("probe preview:\n%s", r.stdout)
		}
	})
}

// Shapes beyond the auth-flag bodies: URLs, header maps, other credential key
// names, and a string placed directly under an arm key.
func TestPreviewRedactsUrlsHeadersAndKeyNames(t *testing.T) {
	cases := []struct {
		name  string
		body  map[string]any
		leaks []string // must not appear
		keeps []string // must still appear
	}{
		{"url userinfo and query", map[string]any{"externalConfig": map[string]any{"url": "https://user:PWleak@h.example/x?token=QTleak&keep=1"}},
			[]string{"PWleak", "QTleak"}, []string{"h.example/x", "keep=1", "user"}},
		{"url secret query name spellings", map[string]any{"url": "https://h.example/x?api_key=Aleak&Access-Token=Bleak&ok=2"},
			[]string{"Aleak", "Bleak"}, []string{"ok=2"}},
		{"header map", map[string]any{"headers": map[string]any{"Authorization": "Bearer Hleak", "X-Custom": "Cleak"}},
			[]string{"Hleak", "Cleak"}, nil},
		{"credential key names", map[string]any{"apiKey": "K1leak", "accessToken": "K2leak", "refreshToken": "K3leak", "secret": "K4leak", "name": "visible"},
			[]string{"K1leak", "K2leak", "K3leak", "K4leak"}, []string{"visible"}},
		{"string directly under an arm key", map[string]any{"externalConfig": map[string]any{"bearerToken": "Sleak", "basic_auth": "Tleak"}},
			[]string{"Sleak", "Tleak"}, nil},
		{"semicolon query and fragment", map[string]any{"url": "https://h.example/x?a=1;token=Q1leak&b=2#access_token=Q2leak&c=3"},
			[]string{"Q1leak", "Q2leak"}, []string{"a=1", "b=2", "c=3"}},
		{"malformed url keeps no userinfo", map[string]any{"url": "https://us er:PWleak@h.example/x?api_key=Q3leak"},
			[]string{"PWleak", "Q3leak"}, nil},
		{"scheme-less userinfo", map[string]any{"url": "user:PWleak@h.example/x?secret=Q4leak&z=9"},
			[]string{"PWleak", "Q4leak"}, []string{"z=9"}},
		{"header array", map[string]any{"headers": []any{map[string]any{"name": "Authorization", "value": "Aleak"}, map[string]any{"name": "X-Api-Key", "value": "Bleak"}}},
			[]string{"Aleak", "Bleak"}, []string{"Authorization"}},
		{"non-string under arm key", map[string]any{"externalConfig": map[string]any{"awsSigv4": 12345678, "customHeader": map[string]any{"headerName": "X", "headerValue": "Hleak"}}},
			[]string{"12345678", "Hleak"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDryRun(t)
			t.Setenv("C1I_URL", mutTestURL)
			var out bytes.Buffer
			cmd := &cobra.Command{Use: "x"}
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			if err := sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/x", Body: tc.body}); err != nil {
				t.Fatal(err)
			}
			for _, l := range tc.leaks {
				if strings.Contains(out.String(), l) {
					t.Errorf("preview leaked %q:\n%s", l, out.String())
				}
			}
			for _, k := range tc.keeps {
				if !strings.Contains(out.String(), k) {
					t.Errorf("preview lost %q:\n%s", k, out.String())
				}
			}
		})
	}
	t.Run("probe --server-url", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{}`))
		t.Setenv("C1I_URL", mutTestURL)
		withDryRun(t)
		r := runMutationCLI(t, s.srv, 0, nil, "mcp", "servers", "test-connection", "--server-url=https://user:PWleak@h.example/x?token=QTleak")
		if r.err != nil {
			t.Fatal(r.err)
		}
		if strings.Contains(r.stdout, "PWleak") || strings.Contains(r.stdout, "QTleak") {
			t.Errorf("leaked:\n%s", r.stdout)
		}
	})
	t.Run("api --path query", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{}`))
		t.Setenv("C1I_URL", mutTestURL)
		withDryRun(t)
		r := runMutationCLI(t, s.srv, 0, nil, "api", "--method", "POST", "--path", "/api/v1/x?token=QTleak&a=1", "--body", `{}`)
		if r.err != nil {
			t.Fatal(r.err)
		}
		if strings.Contains(r.stdout, "QTleak") || !strings.Contains(r.stdout, "a=1") {
			t.Errorf("preview URL:\n%s", r.stdout)
		}
	})
}
