package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		{"api 400", &client.APIError{StatusCode: 400}, exitError},
		{"auth", &client.AuthError{Err: errors.New("no creds")}, exitAuth},
		// Ledger C63: the keyring-unavailable diagnosis (internal/keychain.Load,
		// see keychain_test.go) still surfaces through loadCredentials as an
		// *AuthError like any other credential failure, so it must still map
		// to exitAuth despite its longer, more specific message.
		{"auth: keyring unavailable diagnosis", &client.AuthError{Err: fmt.Errorf("loading credentials: %w", errors.New(
			"no credentials found for c1i/example.test in the file store, and the OS keyring is currently "+
				"unavailable (unsupported platform: linux) — if a credential was saved to the keyring earlier, "+
				"it is unreachable until the keyring is available again; running 'c1i auth login' now stores a "+
				"new credential in the file store, not the keyring"))}, exitAuth},
		{"usage", &usageError{errors.New("bad flag")}, exitUsage},
		{"tool execution", &toolExecutionError{errors.New("isError")}, exitToolError},
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
