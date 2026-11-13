package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// Shared harness for the mutation output tests (cmd/mutation_*_test.go).

// renderTextError renders err as Run does in text mode.
func renderTextError(w io.Writer, err error, ran *cobra.Command) { writeError(w, err, "text", ran) }

const mutTestURL = "https://tenant.example.invalid"

// mutReply is one canned answer; a nil hijack writes status and body.
type mutReply struct {
	status int
	body   string
	hijack bool
}

// mutServer answers requests in order from replies (the last repeats) and
// records what it saw.
type mutServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []entOwnerRequest
}

func newMutServer(t *testing.T, route func(r *http.Request, n int) mutReply) *mutServer {
	t.Helper()
	m := &mutServer{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.seen = append(m.seen, entOwnerRequest{r.Method, r.URL.EscapedPath(), string(b)})
		n := len(m.seen)
		m.mu.Unlock()
		rep := route(r, n)
		if rep.hijack {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		if rep.status == 0 {
			rep.status = http.StatusOK
		}
		w.WriteHeader(rep.status)
		_, _ = io.WriteString(w, rep.body)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mutServer) requests() []entOwnerRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]entOwnerRequest(nil), m.seen...)
}

func fixedReply(status int, body string) func(*http.Request, int) mutReply {
	return func(*http.Request, int) mutReply { return mutReply{status: status, body: body} }
}

type mutResult struct {
	stdout, stderr string
	exit           int
	err            error
}

// stderrObjects parses every stderr line as one JSON object.
func (r mutResult) stderrObjects(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimRight(r.stderr, "\n"), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("stderr line is not a JSON object: %q", line)
		}
		out = append(out, obj)
	}
	return out
}

// errorObject returns the last stderr object carrying "error".
func (r mutResult) errorObject(t *testing.T) map[string]any {
	t.Helper()
	objs := r.stderrObjects(t)
	for i := len(objs) - 1; i >= 0; i-- {
		if _, ok := objs[i]["error"]; ok {
			return objs[i]
		}
	}
	t.Fatalf("no error object on stderr: %q", r.stderr)
	return nil
}

// retriesFromConfig makes the stub client take its retry budget from viper, as
// the real one does, instead of a fixed count.
const retriesFromConfig = -1

// stubMutationClient points newClient at srv for the test; srv nil fails the
// test if a client is built.
func stubMutationClient(t *testing.T, srv *httptest.Server, retries int) {
	t.Helper()
	orig := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		if srv == nil {
			t.Error("a client was built")
			return nil, &client.AuthError{Err: io.EOF}
		}
		if retries == retriesFromConfig {
			return client.NewForTesting(srv.URL, srv.Client(), globalClientOptions()...), nil
		}
		return client.NewForTesting(srv.URL, srv.Client(), client.WithMaxRetries(retries)), nil
	}
	t.Cleanup(func() { newClient = orig })
}

// wantPreview is the exact stdout of a one-request preview; body is the
// request body already indented to its depth, or "" for none.
func wantPreview(method, path, body string) string {
	out := "{\n  \"dryRun\": true,\n  \"requests\": [\n    {\n      \"method\": \"" + method + "\",\n      \"url\": \"" + mutTestURL + path + "\""
	if body != "" {
		out += ",\n      \"body\": " + body
	}
	return out + "\n    }\n  ]\n}\n"
}

// runMutationCLI runs args through rootCmd the way Run does, rendering a
// failure with writeError. srv nil means no client can be built.
func runMutationCLI(t *testing.T, srv *httptest.Server, retries int, out io.Writer, args ...string) mutResult {
	t.Helper()
	return runCLI(t, func() { stubMutationClient(t, srv, retries) }, out, args...)
}

// runCLI is runMutationCLI with the client stub supplied by the caller, or nil
// to build the real client.
func runCLI(t *testing.T, stub func(), out io.Writer, args ...string) mutResult {
	t.Helper()
	resetCmdFlags(t, rootCmd)
	leaf, _, err := rootCmd.Find(args)
	if err != nil {
		t.Fatal(err)
	}
	resetCmdFlags(t, leaf)
	if stub != nil {
		stub()
	}
	var stdout, stderr bytes.Buffer
	if out == nil {
		out = &stdout
	}
	for _, c := range []*cobra.Command{rootCmd, leaf} {
		c.SetOut(out)
		c.SetErr(&stderr)
		c.SetContext(context.Background())
	}
	t.Cleanup(func() {
		for _, c := range []*cobra.Command{rootCmd, leaf} {
			c.SetOut(nil)
			c.SetErr(nil)
		}
	})
	rootCmd.SetArgs(args)
	ran, err := rootCmd.ExecuteContextC(context.Background())
	if err != nil {
		format, _ := rootCmd.PersistentFlags().GetString("error-format")
		writeError(&stderr, err, format, ran)
	}
	return mutResult{stdout: stdout.String(), stderr: stderr.String(), exit: exitCode(err), err: err}
}

// closedPortServer returns a server whose URL refuses connections. Port 1 is
// privileged, so no test process can bind it; a closed ephemeral port could be
// reused by a concurrent run and answer with EOF instead of a dial error.
func closedPortServer(t *testing.T) *httptest.Server {
	t.Helper()
	return &httptest.Server{URL: "http://127.0.0.1:1"}
}

func indentJSON(t *testing.T, compact string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Indent(&b, []byte(compact), "", "  "); err != nil {
		t.Fatal(err)
	}
	return b.String() + "\n"
}
