package cmd

import (
	"strings"
	"testing"
)

func TestTestConnectionDryRunSendsNothingAndRedactsSecrets(t *testing.T) {
	const secret = "s3cr3t-value-123"
	cases := map[string][]string{
		"bearer":       {"--auth=bearer-token", "--bearer-token=" + secret},
		"basic":        {"--auth=basic-auth", "--basic-auth-username=user", "--basic-auth-password=" + secret},
		"custom":       {"--auth=custom-header", "--header-name=X-Key", "--header-value=" + secret},
		"unauthed":     {},
		"config-file":  nil, // filled below
		"config-oauth": nil,
	}
	cases["config-file"] = []string{"--external-config-file", writeTempJSON(t, "ext.json", `{"url":"https://mcp.example.com/sse","bearerToken":{"token":"`+secret+`"}}`)}
	cases["config-oauth"] = []string{"--external-config-file", writeTempJSON(t, "oauth.json", `{"url":"https://mcp.example.com/sse","oauth2":{"clientId":"cid","clientSecret":"`+secret+`"}}`)}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			withDryRun(t)
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, `{"reachable":true}`))
			args := append([]string{"mcp", "servers", "test-connection"}, extra...)
			if name != "config-file" && name != "config-oauth" {
				args = append(args, "--server-url=https://mcp.example.com/sse")
			}
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
			if got := len(s.requests()); got != 0 {
				t.Errorf("%d request(s) sent on --dry-run, want none", got)
			}
			for _, want := range []string{`"dryRun": true`, `"method": "POST"`, "/api/v1/mcp_servers/test_connection", "https://mcp.example.com/sse"} {
				if !strings.Contains(r.stdout, want) {
					t.Errorf("preview missing %q:\n%s", want, r.stdout)
				}
			}
			if strings.Contains(r.stdout+r.stderr, secret) {
				t.Errorf("preview echoed the secret:\n%s%s", r.stdout, r.stderr)
			}
			if name != "unauthed" && !strings.Contains(r.stdout, "[redacted]") {
				t.Errorf("preview has no %s marker:\n%s", "[redacted]", r.stdout)
			}
		})
	}
}

func TestTestConnectionDryRunRedactsEveryKeySpelling(t *testing.T) {
	const secret = "leak-me-42"
	keys := []string{"token", "headerValue", "password", "clientSecret", "secretAccessKey", "sessionToken", "credentialsJson"}
	spell := map[string]func(string) string{
		"camel": func(k string) string { return k },
		"snake": spellSnake,
		"kebab": func(k string) string { return strings.ReplaceAll(spellSnake(k), "_", "-") },
		"upper": strings.ToUpper,
	}
	values := map[string]string{
		"string": `"` + secret + `"`,
		"object": `{"inner":"` + secret + `"}`,
		"array":  `["` + secret + `"]`,
		"number": `12345678`,
	}
	for sp, fn := range spell {
		for _, k := range keys {
			for vn, v := range values {
				t.Run(sp+"/"+k+"/"+vn, func(t *testing.T) {
					withDryRun(t)
					t.Setenv("C1I_URL", mutTestURL)
					s := newMutServer(t, fixedReply(200, `{}`))
					cfg := `{"url":"https://mcp.example.com/sse","bearerToken":{"` + fn(k) + `":` + v + `}}`
					r := runMutationCLI(t, s.srv, 0, nil, "mcp", "servers", "test-connection", "--external-config-file", writeTempJSON(t, "e.json", cfg))
					if r.err != nil {
						t.Fatalf("exit %d: %v", r.exit, r.err)
					}
					if strings.Contains(r.stdout, secret) || strings.Contains(r.stdout, "12345678") {
						t.Errorf("preview leaked the value:\n%s", r.stdout)
					}
					if !strings.Contains(r.stdout, "[redacted]") {
						t.Errorf("no [redacted] marker:\n%s", r.stdout)
					}
				})
			}
		}
	}
}

func spellSnake(k string) string {
	var b strings.Builder
	for _, r := range k {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
