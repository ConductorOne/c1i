package cmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// Text-mode stderr is unchanged by the mutation output contract except for
// the lines in permittedNewStderr (spec R38). The goldens hold each
// scenario's stderr from before the change; this file also builds against
// that tree, which is how they were captured.

// permittedNewStderr are the only text-mode stderr lines the change adds.
var permittedNewStderr = []*regexp.Regexp{
	regexp.MustCompile(`^Resolved policy step: \S+$`),
	regexp.MustCompile(`^Resolved catalog id: \S+$`),
	regexp.MustCompile(`^Waiting for owners to provision on app \S+ `),
	regexp.MustCompile(`^Owners provisioned on app \S+ after <dur>`),
	regexp.MustCompile(`^requests are sent in order; NEW_APP_RESOURCE_TYPE_ID`),
	regexp.MustCompile(`^dry run: nothing was sent$`),
}

type stderrScenario struct {
	name  string
	args  []string
	dry   bool
	route func(r *http.Request) (int, string)
}

var stderrScenarios = []stderrScenario{
	{"tasks-approve-resolves-step", []string{"tasks", "approve", "task-1"}, false, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, `{"taskView":{"task":{"id":"task-1","policy":{"current":{"id":"step-9"}}}}}`
		}
		return 200, `{"taskView":{"task":{"id":"task-1","state":"TASK_STATE_OPEN"}}}`
	}},
	{"update-credentials-resolves-catalog", []string{"mcp", "servers", "update-credentials", "cn1", "--app-id=a1", "--type=hosted", "--auth=bearer-token", "--bearer-token=placeholder"}, false, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, `{"mcpServer":{"mcpServerCatalogId":"cat-1"}}`
		}
		return 200, `{"mcpServer":{"connectorId":"cn1"}}`
	}},
	{"set-owners-wait", []string{"apps", "set-owners", "app-1", "--user-id", "u1", "--wait"}, false, func(r *http.Request) (int, string) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/ownerids"):
			return 200, `{"userIds":["u1"]}`
		case r.Method == http.MethodGet:
			return 200, `{"user":{"id":"u1"}}`
		}
		return 200, `{}`
	}},
	{"entitlements-create-dry-run", []string{"entitlements", "create", "--app-id", "app-1", "--display-name", "e"}, true, nil},
	{"apps-delete-dry-run", []string{"apps", "delete", "app-1"}, true, nil},
	{"apps-delete-404", []string{"apps", "delete", "app-1"}, false, func(*http.Request) (int, string) { return 404, `{"code":5,"message":"not found"}` }},
	{"apps-create-500", []string{"apps", "create", "--display-name", "zz"}, false, func(*http.Request) (int, string) { return 500, `` }},
	{"tasks-close-400", []string{"tasks", "close", "task-1"}, false, func(*http.Request) (int, string) { return 400, `{"code":3,"message":"bad"}` }},
	{"tools-approve-partial", []string{"mcp", "tools", "approve", "t1", "t2", "--app-id=a1", "--connector-id=cn1"}, false, func(r *http.Request) (int, string) {
		if strings.Contains(r.URL.Path, "/t2") {
			return 404, `{"code":5}`
		}
		return 200, `{"tool":{"id":"t1","state":"MCP_TOOL_STATE_APPROVED"}}`
	}},
	{"webhooks-delete", []string{"webhooks", "delete", "wh-1"}, false, nil},
	{"print-config-template", []string{"mcp", "servers", "register", "--print-config-template", "--type=external"}, false, nil},
}

var durationRE = regexp.MustCompile(`\b\d+(\.\d+)?(ns|µs|ms|s|m|h)+\b`)

// runStderrScenario runs sc through rootCmd and returns its text-mode stderr
// with durations normalized.
func runStderrScenario(t *testing.T, sc stderrScenario) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		status, body := 200, `{}`
		if sc.route != nil {
			status, body = sc.route(r)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	orig, origInterval := newClient, ownerWaitInterval
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client(), client.WithMaxRetries(0)), nil
	}
	ownerWaitInterval = time.Millisecond
	t.Cleanup(func() { newClient, ownerWaitInterval = orig, origInterval })
	t.Setenv("C1I_URL", "https://tenant.example.invalid")
	if sc.dry {
		withDryRun(t)
	}
	resetCmdFlags(t, rootCmd)
	leaf, _, err := rootCmd.Find(sc.args)
	if err != nil {
		t.Fatal(err)
	}
	resetCmdFlags(t, leaf)
	var stdout, stderr bytes.Buffer
	for _, c := range []*cobra.Command{rootCmd, leaf} {
		c.SetOut(&stdout)
		c.SetErr(&stderr)
		c.SetContext(context.Background())
	}
	t.Cleanup(func() {
		for _, c := range []*cobra.Command{rootCmd, leaf} {
			c.SetOut(nil)
			c.SetErr(nil)
		}
	})
	rootCmd.SetArgs(sc.args)
	ran, err := rootCmd.ExecuteC()
	if err != nil {
		renderTextError(&stderr, err, ran)
	}
	return durationRE.ReplaceAllString(stderr.String(), "<dur>")
}

// withoutPermitted drops permitted lines; a golden may already hold one
// (set-owners --wait wrote its success line to stderr before the change).
func withoutPermitted(s string) string {
	var kept []string
	for _, line := range strings.SplitAfter(s, "\n") {
		permitted := false
		for _, re := range permittedNewStderr {
			permitted = permitted || re.MatchString(strings.TrimSuffix(line, "\n"))
		}
		if !permitted {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "")
}

func stderrGoldenPath(name string) string {
	return filepath.Join("testdata", "stderr_text", name+".golden")
}

func TestTextStderrGolden(t *testing.T) {
	for _, sc := range stderrScenarios {
		t.Run(sc.name, func(t *testing.T) {
			want, err := os.ReadFile(stderrGoldenPath(sc.name))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := withoutPermitted(runStderrScenario(t, sc)), withoutPermitted(string(want)); got != want {
				t.Errorf("text stderr changed beyond the permitted lines:\n got: %q\nwant: %q", got, want)
			}
		})
	}
}
