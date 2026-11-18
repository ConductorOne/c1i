package cmd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

func writeSourceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func requestBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("request body %q: %v", raw, err)
	}
	return body
}

func TestFunctionsCreateUploadsFilesByBaseName(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	dir := t.TempDir()
	main := writeSourceFile(t, dir, "src/main.ts", "export default async function main() { return {}; }")
	s := newMutServer(t, fixedReply(200, `{"function":{"id":"fn-1"},"commit":{"id":"c-1"}}`))
	r := runMutationCLI(t, s.srv, 0, nil, "functions", "create", "--display-name", "zz", "--file", main)
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	sent := s.requests()
	if len(sent) != 1 || sent[0].method != http.MethodPost || sent[0].rawPath != "/api/v1/functions" {
		t.Fatalf("requests = %+v", sent)
	}
	body := requestBody(t, sent[0].body)
	content, _ := body["initialContent"].(map[string]any)
	got, _ := base64.StdEncoding.DecodeString(content["main.ts"].(string))
	if len(content) != 1 || !strings.Contains(string(got), "export default") {
		t.Errorf("initialContent = %v, want main.ts holding the file", content)
	}
	if body["functionType"] != "FUNCTION_TYPE_ANY" || body["commitMessage"] != "Initial commit" {
		t.Errorf("body = %v", body)
	}
}

func TestFunctionsCreateRefusesDuplicateBaseNames(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	dir := t.TempDir()
	a := writeSourceFile(t, dir, "a/main.ts", "1")
	b := writeSourceFile(t, dir, "b/main.ts", "2")
	s := newMutServer(t, fixedReply(200, `{}`))
	r := runMutationCLI(t, s.srv, 0, nil, "functions", "create", "--display-name", "zz", "--file", a, "--file", b)
	if r.exit != exitUsage || len(s.requests()) != 0 {
		t.Fatalf("exit %d, %d request(s); want 2 and none", r.exit, len(s.requests()))
	}
	r = runMutationCLI(t, s.srv, 0, nil, "functions", "create", "--display-name", "zz", "--file", filepath.Join(dir, "missing.ts"))
	if r.exit != exitUsage || len(s.requests()) != 0 {
		t.Fatalf("missing file: exit %d, %d request(s); want 2 and none", r.exit, len(s.requests()))
	}
}

func TestFunctionsUpdateCodePushSendsEmptyMaskAndCurrentName(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	main := writeSourceFile(t, t.TempDir(), "main.ts", "code")
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{body: `{"function":{"id":"fn-1","displayName":"current"}}`}
		}
		return mutReply{body: `{"function":{"id":"fn-1"},"commit":{"id":"c-2"}}`}
	})
	r := runMutationCLI(t, s.srv, 0, nil, "functions", "update", "fn-1", "--file", main, "--commit-message", "m")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	sent := s.requests()
	if len(sent) != 2 || sent[0].rawPath != "/api/v1/functions/fn-1" || sent[1].rawPath != "/api/v1/functions/update" {
		t.Fatalf("requests = %+v", sent)
	}
	body := requestBody(t, sent[1].body)
	fn, _ := body["function"].(map[string]any)
	if body["updateMask"] != "" || fn["displayName"] != "current" || fn["id"] != "fn-1" || body["commitMessage"] != "m" {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["content"].(map[string]any)["main.ts"]; !ok {
		t.Errorf("content = %v, want main.ts", body["content"])
	}
}

func TestFunctionsUpdatePublishCommitIsMasked(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		return mutReply{body: `{"function":{"id":"fn-1","displayName":"current"},"commit":null}`}
	})
	r := runMutationCLI(t, s.srv, 0, nil, "functions", "update", "fn-1", "--publish-commit", "c-2")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	sent := s.requests()
	body := requestBody(t, sent[len(sent)-1].body)
	fn, _ := body["function"].(map[string]any)
	if body["updateMask"] != "publishedCommitId" || fn["publishedCommitId"] != "c-2" {
		t.Errorf("body = %v", body)
	}
}

func TestFunctionsUpdateUsageErrors(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for name, args := range map[string][]string{
		"nothing to update":             {"functions", "update", "fn-1"},
		"commit message without --file": {"functions", "update", "fn-1", "--description", "d", "--commit-message", "m"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, `{}`))
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != exitUsage || len(s.requests()) != 0 {
				t.Fatalf("exit %d, %d request(s); want 2 and none: %v", r.exit, len(s.requests()), r.err)
			}
		})
	}
}

func TestFunctionsInvokeResolvesPublishedCommit(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{body: `{"function":{"id":"fn-1","publishedCommitId":"pub-1","head":"head-2"}}`}
		}
		return mutReply{body: `{"invocationId":"inv-1"}`}
	})
	r := runMutationCLI(t, s.srv, 0, nil, "functions", "invoke", "fn-1", "--input", `{"a":1}`)
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	sent := s.requests()
	if len(sent) != 2 || sent[1].rawPath != "/api/v1/functions/fn-1/invoke" {
		t.Fatalf("requests = %+v", sent)
	}
	body := requestBody(t, sent[1].body)
	in, _ := base64.StdEncoding.DecodeString(body["json"].(string))
	if body["commitId"] != "pub-1" || string(in) != `{"a":1}` {
		t.Errorf("body = %v (json %q)", body, in)
	}
	if !strings.Contains(r.stdout, `"invocationId": "inv-1"`) {
		t.Errorf("stdout = %q", r.stdout)
	}
}

func TestFunctionsInvokeRefusals(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := []struct {
		name  string
		read  mutReply
		args  []string
		exit  int
		reads int
	}{
		{"unpublished function", mutReply{body: `{"function":{"id":"fn-1","head":"head-2"}}`}, nil, exitUsage, 1},
		{"commit the function lacks", mutReply{status: 404, body: `{"code":5,"message":"not found"}`}, []string{"--commit-id", "c-x"}, exitNotFound, 1},
		{"array input", mutReply{}, []string{"--commit-id", "c-1", "--input", `[1]`}, exitUsage, 0},
		{"null input", mutReply{}, []string{"--commit-id", "c-1", "--input", `null`}, exitUsage, 0},
		{"both inputs", mutReply{}, []string{"--commit-id", "c-1", "--input", `{}`, "--input-file", "x.json"}, exitUsage, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, func(r *http.Request, _ int) mutReply { return tc.read })
			r := runMutationCLI(t, s.srv, 0, nil, append([]string{"functions", "invoke", "fn-1"}, tc.args...)...)
			sent := s.requests()
			if r.exit != tc.exit || len(sent) != tc.reads {
				t.Fatalf("exit %d, requests %+v; want exit %d and %d read(s), no write: %v", r.exit, sent, tc.exit, tc.reads, r.err)
			}
			for _, req := range sent {
				if req.method != http.MethodGet {
					t.Errorf("sent a write: %+v", req)
				}
			}
		})
	}
}

func TestFunctionsInvokeWait(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, tc := range []struct {
		status string
		exit   int
	}{
		{"FUNCTION_INVOCATION_STATUS_SUCCESS", 0},
		{"FUNCTION_INVOCATION_STATUS_ERROR", exitError},
		{"FUNCTION_INVOCATION_STATUS_CANCELLED", exitError},
	} {
		t.Run(tc.status, func(t *testing.T) {
			s := newMutServer(t, func(r *http.Request, _ int) mutReply {
				switch {
				case r.Method == http.MethodPost:
					return mutReply{body: `{"invocationId":"inv-1"}`}
				case strings.Contains(r.URL.Path, "/invocations/"):
					return mutReply{body: `{"invocation":{"id":"inv-1","status":"` + tc.status + `","error":"boom"}}`}
				default:
					return mutReply{body: `{}`}
				}
			})
			r := runMutationCLI(t, s.srv, 0, nil, "functions", "invoke", "fn-1", "--commit-id", "c-1", "--wait", "--wait-timeout", "3s")
			if r.exit != tc.exit {
				t.Fatalf("exit %d, want %d: %v", r.exit, tc.exit, r.err)
			}
			if !strings.Contains(r.stdout, `"invocationId": "inv-1"`) {
				t.Errorf("stdout = %q, want the invocation id printed first", r.stdout)
			}
			if tc.exit != 0 && (!strings.Contains(r.stderr, "was applied") || !strings.Contains(r.stderr, tc.status) || !strings.Contains(r.stderr, "boom")) {
				t.Errorf("stderr = %q, want applied, the status and the function's error", r.stderr)
			}
			last := s.requests()[len(s.requests())-1]
			if last.rawPath != "/api/v1/functions/fn-1/invocations/inv-1" {
				t.Errorf("polled %s", last.rawPath)
			}
		})
	}
}

func TestFunctionsInvocationsRoutes(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"list":[{"id":"inv-1","functionId":"fn-1","status":"FUNCTION_INVOCATION_STATUS_SUCCESS","error":""}],"nextPageToken":""}`))
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(s.srv.URL, s.srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	r := runMutationCLI(t, s.srv, 0, nil, "functions", "invocations", "list", "fn-1")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	sent := s.requests()
	if len(sent) != 1 || sent[0].method != http.MethodPost || sent[0].rawPath != "/api/v1/functions/fn-1/invocations/search" {
		t.Fatalf("requests = %+v", sent)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &row); err != nil || row["id"] != "inv-1" || row["error"] != nil {
		t.Errorf("row = %q (%v)", r.stdout, err)
	}
}
