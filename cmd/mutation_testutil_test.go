package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/testenv"
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

// liveRefBody is a live object for a GET of an id a command looks up before
// a write (app, entitlement, resource type, function, commit, role, service
// principal), in the server's envelope; ok is false for any other request.
// It is for stubs that would otherwise answer {}, which a pre-read refuses.
func liveRefBody(r *http.Request) (body string, ok bool) {
	if r.Method != http.MethodGet {
		return "", false
	}
	seg := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/api/v1/"), "/")
	for i, s := range seg {
		u, err := url.PathUnescape(s)
		if err != nil {
			return "", false
		}
		seg[i] = u
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	switch {
	case len(seg) == 2 && seg[0] == "apps":
		return `{"app":{"id":` + q(seg[1]) + `,"deletedAt":null}}`, true
	case len(seg) == 4 && seg[0] == "apps" && seg[2] == "entitlements":
		return `{"appEntitlementView":{"appEntitlement":{"id":` + q(seg[3]) + `,"appId":` + q(seg[1]) + `,"deletedAt":null}}}`, true
	case len(seg) == 4 && seg[0] == "apps" && seg[2] == "resource_types":
		return `{"appResourceTypeView":{"appResourceType":{"id":` + q(seg[3]) + `,"appId":` + q(seg[1]) + `,"deletedAt":null}}}`, true
	case len(seg) == 2 && seg[0] == "functions":
		return `{"function":{"id":` + q(seg[1]) + `,"deletedAt":null}}`, true
	case len(seg) == 4 && seg[0] == "functions" && seg[2] == "commits":
		return `{"commit":{"id":` + q(seg[3]) + `,"functionId":` + q(seg[1]) + `},"files":{}}`, true
	case len(seg) == 3 && seg[0] == "iam" && seg[1] == "roles":
		return `{"role":{"id":` + q(seg[2]) + `,"deletedAt":null}}`, true
	case len(seg) == 2 && seg[0] == "service_principals":
		return `{"servicePrincipal":{"id":` + q(seg[1]) + `}}`, true
	}
	return "", false
}

// withLiveRefs answers liveRefBody's GETs and passes every other request to
// route.
func withLiveRefs(route func(*http.Request, int) mutReply) func(*http.Request, int) mutReply {
	return func(r *http.Request, n int) mutReply {
		if body, ok := liveRefBody(r); ok {
			return mutReply{body: body}
		}
		return route(r, n)
	}
}

// liveRefHandler is withLiveRefs for a plain httptest handler.
func liveRefHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if body, ok := liveRefBody(r); ok {
			_, _ = io.WriteString(w, body)
			return
		}
		next(w, r)
	}
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

// stubReporter is the part of testing.TB the client stub uses, so a test can
// hand it a recording fake.
type stubReporter interface {
	Helper()
	Error(args ...any)
	Cleanup(func())
}

// stubMutationClient points newClient at srv for the test; srv nil fails the
// test if a client is built.
func stubMutationClient(t stubReporter, srv *httptest.Server, retries int) {
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
	}
	t.Cleanup(func() {
		for _, c := range []*cobra.Command{rootCmd, leaf} {
			c.SetOut(nil)
			c.SetErr(nil)
		}
	})
	rootCmd.SetArgs(args)
	ran, err := executeRootC()
	if err != nil {
		format, _ := rootCmd.PersistentFlags().GetString("error-format")
		writeError(&stderr, err, format, ran)
	}
	return mutResult{stdout: stdout.String(), stderr: stderr.String(), exit: exitCode(err), err: err}
}

// closedPortServer returns a server whose URL refuses connections
// (testenv.DeadURL).
func closedPortServer(t *testing.T) *httptest.Server {
	t.Helper()
	return &httptest.Server{URL: testenv.DeadURL}
}

func indentJSON(t *testing.T, compact string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Indent(&b, []byte(compact), "", "  "); err != nil {
		t.Fatal(err)
	}
	return b.String() + "\n"
}
