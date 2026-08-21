package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

func TestAttachSubcommandGuards(t *testing.T) {
	newTree := func() *cobra.Command {
		root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
		group := &cobra.Command{Use: "group"} // no Run → a group
		group.AddCommand(&cobra.Command{Use: "leaf", RunE: func(*cobra.Command, []string) error { return nil }})
		root.AddCommand(group)
		attachSubcommandGuards(root)
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		return root
	}

	// Unknown subcommand under a group → usageError (was: silent exit 0).
	root := newTree()
	root.SetArgs([]string{"group", "bogus"})
	err := root.Execute()
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Fatalf("unknown subcommand: want usageError, got %T: %v", err, err)
	}

	// Group with no args prints help and does not error.
	root = newTree()
	root.SetArgs([]string{"group"})
	if err := root.Execute(); err != nil {
		t.Fatalf("group with no args should not error, got %v", err)
	}

	// A real leaf still runs.
	root = newTree()
	root.SetArgs([]string{"group", "leaf"})
	if err := root.Execute(); err != nil {
		t.Fatalf("leaf should run, got %v", err)
	}
}

func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, exitOK},
		{"generic", errors.New("boom"), exitError},
		{"api 401", &client.APIError{StatusCode: 401}, exitAuth},
		{"api 403", &client.APIError{StatusCode: 403}, exitAuth},
		{"api 404", &client.APIError{StatusCode: 404}, exitNotFound},
		{"api 429", &client.APIError{StatusCode: 429}, exitRateLimited},
		{"api 500", &client.APIError{StatusCode: 500}, exitServer},
		{"api 503", &client.APIError{StatusCode: 503}, exitServer},
		// 408 is the one 4xx carved out of the usage range: grpc-gateway's
		// standard code table sends codes.Canceled to 408, which is C1's side
		// canceling/timing out, not a bad argument -- it groups with the
		// other "retry later" statuses instead of exitUsage.
		{"api 408", &client.APIError{StatusCode: 408}, exitServer},
		// Every other 4xx that isn't auth/not-found/rate-limited/408 is
		// caller-caused (bad request body/params/conflict/size/etc.), so it
		// maps to exitUsage rather than falling to the generic exitError
		// default.
		{"api 400", &client.APIError{StatusCode: 400}, exitUsage},
		{"api 409", &client.APIError{StatusCode: 409}, exitUsage},
		{"api 413", &client.APIError{StatusCode: 413}, exitUsage},
		{"api 414", &client.APIError{StatusCode: 414}, exitUsage},
		{"api 422", &client.APIError{StatusCode: 422}, exitUsage},
		// 425 stays in the usage range deliberately: no gRPC code maps to
		// it, so there's no evidence this API can ever return it -- this is
		// the negative pair proving 408 alone moved, not the whole low 4xx
		// range.
		{"api 425", &client.APIError{StatusCode: 425}, exitUsage},
		{"auth", &client.AuthError{Err: errors.New("no creds")}, exitAuth},
		// The keyring-unavailable diagnosis (internal/keychain.Load,
		// see keychain_test.go) still surfaces through loadCredentials as an
		// *AuthError like any other credential failure, so it must still map
		// to exitAuth despite its longer, more specific message.
		{"auth: keyring unavailable diagnosis", &client.AuthError{Err: fmt.Errorf("loading credentials: %w", errors.New(
			"no credentials found for c1i/example.test in the file store, and the OS keyring is currently "+
				"unavailable (unsupported platform: linux) — if a credential was saved to the keyring earlier, "+
				"it is unreachable until the keyring is available again; running 'c1i auth login' now stores a "+
				"new credential in the file store, not the keyring"))}, exitAuth},
		{"usage", &usageError{errors.New("bad flag")}, exitUsage},
		{"path guard: empty segment", &client.PathError{Method: "GET", Path: "/api/v1/policies/"}, exitUsage},
		{"wrapped path guard", fmt.Errorf("request failed: %w", &client.PathError{Method: "GET", Path: "/api/v1/policies/"}), exitUsage},
		{"redirect guard: 301", &client.RedirectError{Method: "GET", URL: "https://x/api/v1/users/%2F", StatusCode: 301, Location: "/api/v1/users/"}, exitUsage},
		{"wrapped redirect guard", fmt.Errorf("API error: %w", &client.RedirectError{Method: "GET", URL: "https://x/api/v1/users/%2F", StatusCode: 301, Location: "/api/v1/users/"}), exitUsage},
		{"redirect loop guard", &client.RedirectLoopError{Method: "GET", URL: "https://x/api/v1/users/abc", Hops: 5}, exitServer},
		{"wrapped redirect loop guard", fmt.Errorf("API error: %w", &client.RedirectLoopError{Method: "GET", URL: "https://x/api/v1/users/abc", Hops: 5}), exitServer},
		{"tool execution", &toolExecutionError{errors.New("isError")}, exitToolError},
		{"upstream failure", &upstreamError{errors.New("connector down")}, exitUpstream},
		{"wrapped upstream failure", fmt.Errorf("gateway handshake failed: %w", &upstreamError{errors.New("connector down")}), exitUpstream},
		{"wrapped api 404", fmt.Errorf("API error: %w", &client.APIError{StatusCode: 404}), exitNotFound},
		{"wrapped auth", fmt.Errorf("authentication failed: %w", &client.AuthError{Err: errors.New("x")}), exitAuth},
		{"cobra unknown command", errors.New(`unknown command "bogus" for "c1i"`), exitUsage},
		{"cobra required flag", errors.New(`required flag(s) "app-id" not set`), exitUsage},
		{"cobra arg count", errors.New("accepts 1 arg(s), received 2"), exitUsage},
		{"cobra unknown flag", errors.New("unknown flag: --nope"), exitUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.err); got != tc.want {
				t.Errorf("exitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestExitCodeConstantsArePinned guards the published exit-code contract
// (documented in README.md, cmd/agents.md, and .claude/commands/c1i.md, and
// relied on by agents that branch on these integers). A mutation that
// changed exitUpstream from 8 to 6 survived the full test suite because
// every assertion compared against the symbolic constant, so both sides
// moved together. These comparisons use literal integers so a renumbering
// (or an accidental alias between two codes) shows up here even though every
// other test still passes; if you're deliberately changing one of these
// values, update README.md, cmd/agents.md, and .claude/commands/c1i.md too.
func TestExitCodeConstantsArePinned(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"exitOK", exitOK, 0},
		{"exitError", exitError, 1},
		{"exitUsage", exitUsage, 2},
		{"exitAuth", exitAuth, 3},
		{"exitNotFound", exitNotFound, 4},
		{"exitRateLimited", exitRateLimited, 5},
		{"exitServer", exitServer, 6},
		{"exitToolError", exitToolError, 7},
		{"exitUpstream", exitUpstream, 8},
	}
	seen := make(map[int]string, len(cases))
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d -- this is a published exit-code contract "+
				"(documented in README.md, cmd/agents.md, and .claude/commands/c1i.md) "+
				"that agents branch on; if this change is deliberate, update those docs too",
				tc.name, tc.got, tc.want)
		}
		if other, dup := seen[tc.got]; dup {
			t.Errorf("%s and %s both equal %d -- two exit codes must never alias to the "+
				"same integer, since agents distinguish failure classes by this value",
				tc.name, other, tc.got)
		}
		seen[tc.got] = tc.name
	}
}

func TestWriteErrorText(t *testing.T) {
	var buf bytes.Buffer
	writeError(&buf, errors.New("boom"), "text")
	if got := buf.String(); got != "Error: boom\n" {
		t.Errorf("text error = %q", got)
	}
}

func TestWriteErrorFormatCaseInsensitive(t *testing.T) {
	for _, f := range []string{"json", "JSON", "Json"} {
		var buf bytes.Buffer
		writeError(&buf, errors.New("boom"), f)
		var got map[string]any
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Errorf("format %q did not produce JSON: %s", f, buf.String())
		}
	}
}

func TestWriteErrorJSON(t *testing.T) {
	var buf bytes.Buffer
	apiErr := &client.APIError{Method: "GET", Path: "/api/v1/x", StatusCode: 404, Body: `{"message":"not found"}`}
	writeError(&buf, fmt.Errorf("API error: %w", apiErr), "json")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%s)", err, buf.String())
	}
	if got["status"].(float64) != 404 || got["method"] != "GET" || got["path"] != "/api/v1/x" {
		t.Errorf("json error missing structured fields: %v", got)
	}
	// Body is valid JSON, so it should be embedded as an object, not a string.
	body, ok := got["body"].(map[string]any)
	if !ok || body["message"] != "not found" {
		t.Errorf("body not embedded as JSON object: %#v", got["body"])
	}
}

// A PathError never reaches the wire (do() refuses the request before
// sending), so any "API error:"-style prefix a call site wraps it in is a
// false claim. writeError must strip those inherited prefixes and print the
// PathError's own explanation instead — for both a bare PathError and one
// buried under other %w wrapping (e.g. resolvePolicyStepID's "failed to
// fetch task..." wrap in cmd/tasks.go).
func TestWriteErrorTextPathErrorDropsInheritedPrefix(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"bare", fmt.Errorf("API error: %w", &client.PathError{Method: "GET", Path: "/api/v1/policies/"})},
		// The wrap chain itself contains "API error:" (as an inner layer
		// beneath another wrap), so the "no false claim survives" assertion
		// below is only true if multi-level errors.As unwrapping actually
		// reaches the PathError and replaces the whole chain — a single-level
		// unwrap, or no unwrap at all, would leave "API error:" in the output.
		{"doubly wrapped", fmt.Errorf("failed to fetch task to determine current policy step: %w",
			fmt.Errorf("API error: %w", &client.PathError{Method: "GET", Path: "/api/v1/tasks/"}))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeError(&buf, tc.err, "text")
			got := buf.String()
			if strings.Contains(got, "API error:") {
				t.Errorf("message still carries the false API error: claim: %q", got)
			}
			if !strings.Contains(got, "empty path segment") {
				t.Errorf("message does not explain the empty path segment: %q", got)
			}
		})
	}
}

// TestWriteErrorTextRedirectErrorDropsInheritedPrefix mirrors the PathError
// case above for *client.RedirectError: the client refuses the redirect
// itself (redirectTripper), so it never reaches the wire either, and a call
// site's "API error:" wrap is just as false a claim for it.
func TestWriteErrorTextRedirectErrorDropsInheritedPrefix(t *testing.T) {
	var buf bytes.Buffer
	redirErr := &client.RedirectError{Method: "GET", URL: "https://x/api/v1/users/%2F", StatusCode: 301, Location: "/api/v1/users/"}
	writeError(&buf, fmt.Errorf("API error: %w", redirErr), "text")
	got := buf.String()
	if strings.Contains(got, "API error:") {
		t.Errorf("message still carries the false API error: claim: %q", got)
	}
	if !strings.Contains(got, "301") || !strings.Contains(got, "/api/v1/users/") {
		t.Errorf("message does not name the redirect target: %q", got)
	}
}

// TestWriteErrorTextRedirectLoopErrorDropsInheritedPrefix mirrors the
// RedirectError case above for *client.RedirectLoopError: it's also the
// client's own refusal, not a wire response, so an inherited "API error:"
// wrap is just as false a claim.
func TestWriteErrorTextRedirectLoopErrorDropsInheritedPrefix(t *testing.T) {
	var buf bytes.Buffer
	loopErr := &client.RedirectLoopError{Method: "GET", URL: "https://x/api/v1/users/abc", Hops: 5}
	writeError(&buf, fmt.Errorf("API error: %w", loopErr), "text")
	got := buf.String()
	if strings.Contains(got, "API error:") {
		t.Errorf("message still carries the false API error: claim: %q", got)
	}
	if !strings.Contains(got, "5") {
		t.Errorf("message does not name the hop count: %q", got)
	}
}

// TestWriteErrorTextAPIErrorKeepsPrefix guards displayError's PathError
// special-case from being widened to other error types: a wrapped
// *client.APIError must keep its inherited "API error: " prefix and its
// status/body context in the printed text, unlike a PathError.
func TestWriteErrorTextAPIErrorKeepsPrefix(t *testing.T) {
	var buf bytes.Buffer
	apiErr := &client.APIError{Method: "GET", Path: "/api/v1/x", StatusCode: 404, Body: `{"message":"not found"}`}
	writeError(&buf, fmt.Errorf("API error: %w", apiErr), "text")
	got := buf.String()
	if !strings.Contains(got, "API error:") {
		t.Errorf("wrapped API error lost its inherited prefix: %q", got)
	}
	if !strings.Contains(got, "404") || !strings.Contains(got, "not found") {
		t.Errorf("wrapped API error lost its status/body context: %q", got)
	}
}

// TestWriteErrorJSONAPIErrorKeepsPrefix is the --error-format json twin of
// TestWriteErrorTextAPIErrorKeepsPrefix: the "error" field's string content
// must still carry the inherited prefix, not just the structured
// status/method/path fields TestWriteErrorJSON already checks.
func TestWriteErrorJSONAPIErrorKeepsPrefix(t *testing.T) {
	var buf bytes.Buffer
	apiErr := &client.APIError{Method: "GET", Path: "/api/v1/x", StatusCode: 404, Body: `{"message":"not found"}`}
	writeError(&buf, fmt.Errorf("API error: %w", apiErr), "json")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%s)", err, buf.String())
	}
	errStr, _ := got["error"].(string)
	if !strings.Contains(errStr, "API error:") {
		t.Errorf("json error field lost its inherited prefix: %q", errStr)
	}
}

// TestWriteErrorTextAuthErrorKeepsPrefix is the *client.AuthError analog of
// TestWriteErrorTextAPIErrorKeepsPrefix.
func TestWriteErrorTextAuthErrorKeepsPrefix(t *testing.T) {
	var buf bytes.Buffer
	authErr := &client.AuthError{Err: errors.New("no creds")}
	writeError(&buf, fmt.Errorf("authentication failed: %w", authErr), "text")
	got := buf.String()
	if !strings.Contains(got, "authentication failed:") {
		t.Errorf("wrapped auth error lost its inherited prefix: %q", got)
	}
}

// TestWriteErrorJSONAuthErrorKeepsPrefix is the --error-format json twin of
// TestWriteErrorTextAuthErrorKeepsPrefix.
func TestWriteErrorJSONAuthErrorKeepsPrefix(t *testing.T) {
	var buf bytes.Buffer
	authErr := &client.AuthError{Err: errors.New("no creds")}
	writeError(&buf, fmt.Errorf("authentication failed: %w", authErr), "json")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v (%s)", err, buf.String())
	}
	errStr, _ := got["error"].(string)
	if !strings.Contains(errStr, "authentication failed:") {
		t.Errorf("json error field lost its inherited prefix: %q", errStr)
	}
}

func TestWriteErrorJSONNonJSONBody(t *testing.T) {
	var buf bytes.Buffer
	writeError(&buf, &client.APIError{Method: "GET", Path: "/x", StatusCode: 500, Body: "upstream exploded"}, "json")
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if got["body"] != "upstream exploded" {
		t.Errorf("non-JSON body should stay a string, got %#v", got["body"])
	}
}

func TestValidateErrorFormat(t *testing.T) {
	for _, ok := range []string{"", "text", "json", "JSON", "Text"} {
		if err := validateErrorFormat(ok); err != nil {
			t.Errorf("validateErrorFormat(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"xml", "yaml", "jsonn", "tex"} {
		err := validateErrorFormat(bad)
		if err == nil {
			t.Errorf("validateErrorFormat(%q) = nil, want error", bad)
			continue
		}
		var ue *usageError
		if !errors.As(err, &ue) {
			t.Errorf("validateErrorFormat(%q) = %T, want *usageError (exit 2)", bad, err)
		}
	}
}
