package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
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

// primeOpenAPICache points the home dir at a temp dir and pre-populates the
// OpenAPI cache file so fetchOpenAPISpec reads the stub spec above without
// hitting the network. The cache file's mtime is "now", which is inside the 24h
// cacheMaxAge window fetchOpenAPISpec checks.
func primeOpenAPICache(t *testing.T) { primeOpenAPICacheWith(t, stubOpenAPISpec) }

func primeOpenAPICacheWith(t *testing.T, spec string) {
	t.Helper()
	cacheDir := filepath.Dir(useTempHome(t))
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}
	cachePath := filepath.Join(cacheDir, cacheFileName)
	if err := os.WriteFile(cachePath, []byte(spec), 0o600); err != nil {
		t.Fatalf("failed to write stub cache: %v", err)
	}
}

func runDocsEndpoints(t *testing.T, filter string) (stdout, stderr string) {
	t.Helper()
	primeOpenAPICache(t)

	cmd := docsEndpointsCmd
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	restoreCmdWriters(t)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	resetCmdFlags(t, cmd)
	if err := cmd.Flags().Set("filter", filter); err != nil {
		t.Fatalf("failed to set --filter: %v", err)
	}

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE returned unexpected error: %v", err)
	}
	return outBuf.String(), errBuf.String()
}

// A miss prints no rows, quotes the filter, and offers docs search for the
// same query. The spec now carries the MCP and access-review APIs, so the miss
// must not claim they are missing from it.
func TestDocsEndpointsMissSuggestsDocsSearch(t *testing.T) {
	stdout, stderr := runDocsEndpoints(t, "does-not-exist-anywhere")

	if stdout != "" {
		t.Errorf("expected no rows on a miss, got: %q", stdout)
	}
	for _, want := range []string{
		`"does-not-exist-anywhere"`,
		`c1i docs search "does-not-exist-anywhere"`,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("miss message missing %q; got:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "aren't in the public OpenAPI spec") {
		t.Errorf("miss message still claims endpoints are missing from the spec:\n%s", stderr)
	}
}

// Matching endpoint rows keep diagnostics on a separate stream.
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

// Siblings sharing a ref must resolve the same way on every run.
func TestResolveRefsDeterministicAcrossSiblings(t *testing.T) {
	const spec = `
paths:
  /things:
    get:
      responses:
        "200": {$ref: "#/components/responses/Thing"}
    post:
      requestBody: {$ref: "#/components/schemas/Thing"}
      responses:
        "200": {$ref: "#/components/responses/Thing"}
    put:
      requestBody: {$ref: "#/components/schemas/Thing"}
components:
  responses:
    Thing:
      content: {schema: {$ref: "#/components/schemas/Thing"}}
  schemas:
    Thing:
      properties: {id: {type: string}}
`
	var root map[string]any
	if err := yaml.Unmarshal([]byte(spec), &root); err != nil {
		t.Fatal(err)
	}
	pathObj := root["paths"].(map[string]any)["/things"]
	var first []byte
	for i := 0; i < 20; i++ {
		out, err := json.Marshal(resolveRefs(pathObj, root, 0, nil))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = out
		} else if !bytes.Equal(out, first) {
			t.Fatalf("run %d differs:\n%s\nfirst:\n%s", i, out, first)
		}
	}
}

// docs endpoint projects --fields like every other single-object read.
func TestDocsEndpointProjectsFields(t *testing.T) {
	primeOpenAPICache(t)
	clearViperOverrides(t, "fields")
	viper.Set("fields", "summary")
	t.Cleanup(func() { clearViperOverrides(t, "fields") })

	var out bytes.Buffer
	restoreCmdWriters(t)
	resetCmdFlags(t, docsEndpointCmd)
	docsEndpointCmd.SetOut(&out)
	docsEndpointCmd.SetErr(&bytes.Buffer{})
	if err := docsEndpointCmd.RunE(docsEndpointCmd, []string{"/api/v1/users/{id}"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"summary": "Get User"`) || strings.Contains(got, "operationId") {
		t.Errorf("projected output = %s, want only summary", got)
	}
}

// stubMultiMethodSpec has a path with two methods, so --method has something
// real to narrow and something real to list as "not defined".
const stubMultiMethodSpec = `
paths:
  /api/v1/things:
    get:
      summary: List Things
      operationId: c1.api.thing.v1.Things.List
    post:
      summary: Create Thing
      operationId: c1.api.thing.v1.Things.Create
`

func runDocsEndpoint(t *testing.T, path, method string) (stdout string, err error) {
	t.Helper()
	var out bytes.Buffer
	restoreCmdWriters(t)
	resetCmdFlags(t, docsEndpointCmd)
	docsEndpointCmd.SetOut(&out)
	docsEndpointCmd.SetErr(&bytes.Buffer{})
	if method != "" {
		if setErr := docsEndpointCmd.Flags().Set("method", method); setErr != nil {
			t.Fatalf("failed to set --method: %v", setErr)
		}
	}
	err = docsEndpointCmd.RunE(docsEndpointCmd, []string{path})
	return out.String(), err
}

// --method narrows the output to just that method's operation.
func TestDocsEndpointMethodFiltersToOneMethod(t *testing.T) {
	primeOpenAPICacheWith(t, stubMultiMethodSpec)

	got, err := runDocsEndpoint(t, "/api/v1/things", "post")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(got, "Create Thing") || strings.Contains(got, "List Things") {
		t.Errorf("output = %s, want only the post operation", got)
	}
}

// --method matches case-insensitively.
func TestDocsEndpointMethodCaseInsensitive(t *testing.T) {
	primeOpenAPICacheWith(t, stubMultiMethodSpec)

	got, err := runDocsEndpoint(t, "/api/v1/things", "POST")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(got, "Create Thing") || strings.Contains(got, "List Things") {
		t.Errorf("output = %s, want only the post operation", got)
	}
}

// Omitting --method keeps every method, unchanged from before the flag existed.
func TestDocsEndpointNoMethodKeepsEveryMethod(t *testing.T) {
	primeOpenAPICacheWith(t, stubMultiMethodSpec)

	got, err := runDocsEndpoint(t, "/api/v1/things", "")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(got, "Create Thing") || !strings.Contains(got, "List Things") {
		t.Errorf("output = %s, want both operations", got)
	}
}

// A method outside get|post|put|patch|delete is a usage error naming the
// accepted values, not a silent pass-through.
func TestDocsEndpointMethodUnknownValueIsUsageError(t *testing.T) {
	primeOpenAPICacheWith(t, stubMultiMethodSpec)

	_, err := runDocsEndpoint(t, "/api/v1/things", "fetch")
	if err == nil {
		t.Fatal("expected an error for an unrecognized --method value")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("expected a usageError, got %T: %v", err, err)
	}
	for _, want := range []string{"fetch", "get", "post", "put", "patch", "delete"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

// A valid method the path doesn't define lists the methods it does define.
func TestDocsEndpointMethodNotDefinedListsAvailable(t *testing.T) {
	primeOpenAPICacheWith(t, stubMultiMethodSpec)

	_, err := runDocsEndpoint(t, "/api/v1/things", "delete")
	if err == nil {
		t.Fatal("expected an error for a method the path doesn't define")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("expected a usageError, got %T: %v", err, err)
	}
	for _, want := range []string{"/api/v1/things", "DELETE", "GET", "POST"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}
