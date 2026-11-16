package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A server that echoes the request URL in an error body must not carry a
// secret parameter to stderr, in either format, with or without --debug.
func TestErrorBodyEchoRedactsSecrets(t *testing.T) {
	bodies := []struct {
		name, body string
		leaks      []string
		keeps      []string
	}{
		{"json path query", `{"message":"bad request /api/v1/x?token=SEKB1\u0026visible=1"}`, []string{"SEKB1"}, []string{"visible=1"}},
		{"json full url semicolon", `{"message":"no route https://h.example/path;token=SEKB2/y?keep=2","code":7}`, []string{"SEKB2"}, []string{"keep=2"}},
		{"json nested and list", `{"error":{"detail":["/z?api_key=SEKB3&n=3"]}}`, []string{"SEKB3"}, []string{"n=3"}},
		{"plain text", "bad request /api/v1/x?access_token=SEKB4&visible=4", []string{"SEKB4"}, []string{"visible=4"}},
		{"bare pairs", "token=SEKB5&visible=5", []string{"SEKB5"}, []string{"visible=5"}},
	}
	for _, b := range bodies {
		for _, format := range []string{"text", "json"} {
			for _, debug := range []bool{false, true} {
				name := b.name + "/" + format
				args := []string{"api", "--path", "/api/v1/x", "--error-format", format}
				if debug {
					name += "/debug"
					args = append(args, "--debug")
				}
				t.Run(name, func(t *testing.T) {
					s := newMutServer(t, fixedReply(400, b.body))
					t.Setenv("C1I_URL", mutTestURL)
					var r mutResult
					trace := captureStderr(t, func() { r = runMutationCLI(t, s.srv, 0, nil, args...) })
					if r.err == nil {
						t.Fatal("want a 400")
					}
					all := trace + r.stderr + r.stdout
					for _, l := range b.leaks {
						if strings.Contains(all, l) {
							t.Errorf("secret %s leaked:\n%s", l, all)
						}
					}
					for _, k := range b.keeps {
						if !strings.Contains(all, k) {
							t.Errorf("lost %q:\n%s", k, all)
						}
					}
					if format == "json" {
						var obj map[string]any
						if err := json.Unmarshal([]byte(strings.TrimSpace(r.stderr)), &obj); err != nil {
							t.Fatalf("stderr is not one JSON object: %v\n%s", err, r.stderr)
						}
						if strings.HasPrefix(b.body, "{") {
							if _, ok := obj["body"].(map[string]any); !ok {
								t.Errorf("body = %#v, want a JSON object", obj["body"])
							}
						}
					}
				})
			}
		}
	}
}

// A mutation's error goes through the same sinks.
func TestMutationErrorBodyEchoRedactsSecrets(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			s := newMutServer(t, fixedReply(400, `{"message":"bad /api/v1/users?token=SEKM1&keep=1"}`))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, "api", "--method", "POST", "--path", "/api/v1/things", "--body", "{}", "--error-format", format)
			if r.err == nil || strings.Contains(r.stderr+r.stdout, "SEKM1") || !strings.Contains(r.stderr, "keep=1") {
				t.Errorf("err %v stderr %q stdout %q", r.err, r.stderr, r.stdout)
			}
		})
	}
}

// docs search/page print the Mintlify error body in their error text.
func TestDocsErrorBodyEchoRedactsSecrets(t *testing.T) {
	for _, sub := range []string{"search", "page"} {
		t.Run(sub, func(t *testing.T) {
			_, stderr, err := runDocsCLI(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"bad /x?token=SEKD1&keep=1"}`))
			}, sub, "x")
			if err == nil || strings.Contains(stderr+err.Error(), "SEKD1") || !strings.Contains(err.Error(), "keep=1") {
				t.Errorf("err %v stderr %q", err, stderr)
			}
		})
	}
}
