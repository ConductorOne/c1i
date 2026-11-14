package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// config-field keys are JSON keys, not headers: only control characters are refused.
func TestConfigFieldKeysAreNotHeaderNames(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cmds := map[string][]string{
		"register":           {"mcp", "servers", "register", "--app-id", "a", "--type", "hosted", "--display-name", "d", "--catalog-id", "cat1"},
		"update-credentials": {"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "hosted", "--catalog-id", "cat1", "--update-mask", "hostedConfig"},
	}
	for name, base := range cmds {
		for _, key := range []string{"a b", "a/b", "a:b", "ключ"} {
			srv := newMutServer(t, fixedReply(200, "{}"))
			res := runMutationCLI(t, srv.srv, 0, nil, append(append([]string{}, base...), "--config-field", key+"=v", "--dry-run")...)
			if res.exit != 0 {
				t.Errorf("%s key %q: exit %d err %v, want accepted", name, key, res.exit, res.err)
			}
		}
		for _, key := range []string{"a\nb", "a\tb", "a\x00b"} {
			srv := newMutServer(t, fixedReply(200, "{}"))
			res := runMutationCLI(t, srv.srv, 0, nil, append(append([]string{}, base...), "--config-field", key+"=v", "--dry-run")...)
			if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), "--config-field") ||
				!strings.Contains(res.err.Error(), "control character") || strings.Contains(res.err.Error(), "header") {
				t.Errorf("%s key %q: exit %d err %v, want a control-character usage error without 'header'", name, key, res.exit, res.err)
			}
		}
	}
}

// Protobuf JSON accepts either spelling per field, so every arm x field
// spelling combination must be checked, plus awsSigv4's session token.
func TestConfigFileHeaderFieldsEverySpelling(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cmds := map[string][]string{
		"register":        {"mcp", "servers", "register", "--app-id", "a", "--type", "external", "--display-name", "d", "--external-config-file"},
		"creds":           {"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "external", "--update-mask", "externalConfig", "--external-config-file"},
		"test-connection": {"mcp", "servers", "test-connection", "--external-config-file"},
	}
	type field struct {
		camel, snake string
		isName       bool
	}
	arms := []struct {
		camel, snake string
		fields       []field
	}{
		{"customHeader", "custom_header", []field{{"headerName", "header_name", true}, {"headerValue", "header_value", false}}},
		{"bearerToken", "bearer_token", []field{{"token", "token", false}}},
		{"awsSigv4", "aws_sigv4", []field{{"sessionToken", "session_token", false}}},
	}
	ran := 0
	for cname, base := range cmds {
		for _, arm := range arms {
			for _, armName := range []string{arm.camel, arm.snake} {
				for _, f := range arm.fields {
					for _, fieldName := range []string{f.camel, f.snake} {
						bad := `sekr3t\r\nX-Evil: 1`
						if f.isName {
							bad = `X Bad sekr3t`
						}
						file := filepath.Join(t.TempDir(), "cfg.json")
						body := `{"` + armName + `":{"` + fieldName + `":"` + bad + `"}}`
						if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
							t.Fatal(err)
						}
						srv := newMutServer(t, fixedReply(200, "{}"))
						res := runMutationCLI(t, srv.srv, 0, nil, append(append([]string{}, base...), file, "--dry-run")...)
						out := res.stdout + res.stderr
						if res.err != nil {
							out += res.err.Error()
						}
						ran++
						if res.exit != exitUsage || !strings.Contains(out, armName+"."+fieldName) {
							t.Errorf("%s %s: exit %d out %q, want a usage error naming %s.%s", cname, body, res.exit, out, armName, fieldName)
						}
						if strings.Contains(out, "sekr3t") || strings.Contains(out, "X-Evil") {
							t.Errorf("%s %s: output echoes the value: %q", cname, body, out)
						}
						if got := srv.requests(); len(got) != 0 {
							t.Errorf("%s %s: requests sent: %v", cname, body, got)
						}
					}
				}
			}
		}
	}
	if want := 3 * (2*2*2 + 2*1*2 + 2*1*2); ran != want {
		t.Errorf("ran %d cases, want %d", ran, want)
	}
}
