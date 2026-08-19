package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubOpenAPISpec is a minimal spec with one path, just enough for
// fetchOpenAPISpec to parse successfully and for filters to have something
// (or nothing) to match against.
const stubOpenAPISpec = `
paths:
  /api/v1/users/{id}:
    get:
      summary: Get User
      operationId: c1.api.user.v1.Users.Get
`

// primeOpenAPICache points HOME at a temp dir and pre-populates the OpenAPI
// cache file so fetchOpenAPISpec reads the stub spec above without hitting
// the network. The cache file's mtime is "now", which is inside the 24h
// cacheMaxAge window fetchOpenAPISpec checks.
func primeOpenAPICache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	cacheDir := filepath.Join(dir, cacheDirName, "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	cachePath := filepath.Join(cacheDir, cacheFileName)
	if err := os.WriteFile(cachePath, []byte(stubOpenAPISpec), 0o600); err != nil {
		t.Fatalf("failed to write stub cache: %v", err)
	}
}

func runDocsEndpoints(t *testing.T, filter string) (stdout, stderr string) {
	t.Helper()
	primeOpenAPICache(t)

	cmd := docsEndpointsCmd
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	if err := cmd.Flags().Set("filter", filter); err != nil {
		t.Fatalf("failed to set --filter: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Flags().Set("filter", "")
	})

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE returned unexpected error: %v", err)
	}
	return outBuf.String(), errBuf.String()
}

// TestDocsEndpointsMissNamesHiddenFamilies pins that a filter miss names the
// two endpoint families we've confirmed are real, working live, and
// intentionally absent from the public OpenAPI spec (mcp_servers/mcp_tools/
// mcp_toolsets and access_review/access_reviews), each with a concrete next
// step, rather than only pointing at 'docs search'. See docs_openapi.go for
// how each was verified.
func TestDocsEndpointsMissNamesHiddenFamilies(t *testing.T) {
	_, stderr := runDocsEndpoints(t, "does-not-exist-anywhere")

	wantSubstrings := []string{
		`"does-not-exist-anywhere"`,
		"mcp_servers",
		"c1i mcp",
		"access_review",
		"c1i api --path=/api/v1/access_review",
		`c1i docs search "does-not-exist-anywhere"`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(stderr, want) {
			t.Errorf("miss message missing %q; got:\n%s", want, stderr)
		}
	}
}

// TestDocsEndpointsMatchHasNoMissMessage pins that a filter which actually
// matches an endpoint in the spec prints only the matching row(s) and never
// the hidden-family hint, so the new message doesn't leak onto the success
// path.
func TestDocsEndpointsMatchHasNoMissMessage(t *testing.T) {
	stdout, stderr := runDocsEndpoints(t, "users")

	if stderr != "" {
		t.Errorf("expected no stderr output on a match, got: %q", stderr)
	}
	if !strings.Contains(stdout, "/api/v1/users/{id}") {
		t.Errorf("expected matching endpoint in stdout, got: %q", stdout)
	}
}

// TestDocsEndpointsNoFilterHasNoMissMessage pins that omitting --filter
// entirely never triggers the miss message, even though it also produces no
// matches to filter against explicitly (empty filter means "list all").
func TestDocsEndpointsNoFilterHasNoMissMessage(t *testing.T) {
	_, stderr := runDocsEndpoints(t, "")

	if stderr != "" {
		t.Errorf("expected no stderr output with no filter, got: %q", stderr)
	}
}
