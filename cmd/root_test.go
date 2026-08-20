package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestNoSubcommandDefinesOwnPersistentPostRunE guards an invariant the
// --fields zero-match-in-list check (checkFieldsMatchedAnyRow in
// cmd/fields.go) depends on: cobra runs only the NEAREST ancestor's
// PersistentPostRunE/PersistentPostRun (the same rule it applies to
// PersistentPreRunE), so if any subcommand ever defined its own, rootCmd's
// checkFieldsMatchedAnyRow would silently stop running for that subcommand
// and everything nested under it — the exact silent-miss failure class this
// whole design (a single central hook instead of a repeated per-call-site
// check) exists to prevent.
//
// This mirrors TestArgsUseConsistencyAcrossTree (cmd/args_positional_test.go)
// and attachSubcommandGuards's own tree-wide guarantee: walk the REAL tree
// rooted at rootCmd once, so the trap becomes a test-time guarantee instead
// of something every future contributor has to remember on their own.
func TestNoSubcommandDefinesOwnPersistentPostRunE(t *testing.T) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, c := range cmd.Commands() {
			if c.PersistentPostRunE != nil || c.PersistentPostRun != nil {
				t.Errorf("%s defines its own PersistentPostRunE/PersistentPostRun; only rootCmd may define one — a subcommand override silently disables checkFieldsMatchedAnyRow for itself and every command nested under it", c.CommandPath())
			}
			walk(c)
		}
	}
	walk(rootCmd)

	if rootCmd.PersistentPostRunE == nil {
		t.Fatal("rootCmd.PersistentPostRunE is nil; the --fields zero-match-in-list check (checkFieldsMatchedAnyRow) is not wired up at all")
	}
}

// TestNoSubcommandDefinesOwnPersistentPreRunE is the same guard for
// PersistentPreRunE/PersistentPreRun: withFieldsMatchState (cmd/fields.go)
// is attached to the context there, and a subcommand override would mean
// its whole subtree never gets a *fieldsMatchState at all, silently
// disabling the check for it in a different way (newEmitter would see a nil
// tracker and record nothing).
func TestNoSubcommandDefinesOwnPersistentPreRunE(t *testing.T) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, c := range cmd.Commands() {
			if c.PersistentPreRunE != nil || c.PersistentPreRun != nil {
				t.Errorf("%s defines its own PersistentPreRunE/PersistentPreRun; only rootCmd may — a subcommand override silently skips attaching the *fieldsMatchState for itself and every command nested under it", c.CommandPath())
			}
			walk(c)
		}
	}
	walk(rootCmd)

	if rootCmd.PersistentPreRunE == nil {
		t.Fatal("rootCmd.PersistentPreRunE is nil; withFieldsMatchState is not wired up at all")
	}
}

// --- Fix 6: a non-UTF-8 positional id is a client-side usage error ---
//
// An id containing a lone surrogate (invalid UTF-8, since surrogates are
// only meaningful inside UTF-16) used to reach the server unfiltered, which
// answered with a bare 500 -- a client mistake reporting as a remote
// failure. This is checked once, in rootCmd's PersistentPreRunE (the same
// central hook TestNoSubcommandDefinesOwnPersistentPreRunE just proved runs
// for every command), rather than per-command, and it runs before
// authentication -- newClient/GetBaseURL are never reached.

// invalidUTF8Arg is a lone UTF-16 surrogate half encoded as raw bytes: valid
// neither as UTF-8 nor as anything a JSON string could carry.
const invalidUTF8Arg = "\xed\xa0\x80"

func TestNonUTF8PositionalArgIsUsageError(t *testing.T) {
	// Isolate from any real credentials/network this machine happens to have
	// configured -- this check must fire before authentication or a request
	// is ever attempted, so a bogus, unreachable URL proves that.
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"users", "get", invalidUTF8Arg})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected an error for a non-UTF-8 positional argument, got nil")
	}
	if !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Errorf("error = %q, want it to say the argument isn't valid UTF-8", err.Error())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d (usage error, not a server-side 500)", got, want)
	}
}

// TestValidUTF8PositionalArgUnaffected is the regression guard: an ordinary
// (well-formed, just nonexistent) id must not be rejected by this check --
// it should reach as far as authentication, which is where this fake
// invocation is expected to fail instead (no credentials configured for
// example.invalid).
func TestValidUTF8PositionalArgUnaffected(t *testing.T) {
	t.Setenv("C1I_URL", "https://example.invalid")
	t.Setenv("C1I_CLIENT_ID", "")
	t.Setenv("C1I_CLIENT_SECRET", "")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"users", "get", "a-perfectly-normal-id"})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected an error (no credentials configured), got nil")
	}
	if strings.Contains(err.Error(), "not valid UTF-8") {
		t.Errorf("a well-formed id must not be rejected by the UTF-8 check, got: %v", err)
	}
}

// --- Fix 6, closing GAP 2: a non-UTF-8 FLAG value, not just a positional ---
//
// Per CLAUDE.md's own convention, a parent-scope id (--app-id,
// --connector-id, ...) is a FLAG, interpolated into the request path exactly
// like a positional id -- so the positional-only check above left this
// argument shape uncovered. Reproduced live:
// "mcp servers get someid --app-id $'\xff\xfe'" returned a bare 500 (exit
// 6) before this fix, the same failure class Fix 6 closed for positionals.

// resetMcpServersGetCmdAppIDFlag restores mcpServersGetCmd's --app-id flag to
// unset after a test drives it through the package-level singleton, so
// state can't leak into another test.
func resetMcpServersGetCmdAppIDFlag(t *testing.T) {
	t.Helper()
	reset := func() {
		f := mcpServersGetCmd.Flags().Lookup("app-id")
		_ = f.Value.Set("")
		f.Changed = false
	}
	t.Cleanup(reset)
}

func TestNonUTF8FlagValueIsUsageError(t *testing.T) {
	resetMcpServersGetCmdAppIDFlag(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"mcp", "servers", "get", "someid", "--app-id", invalidUTF8Arg})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected an error for a non-UTF-8 flag value, got nil")
	}
	if !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Errorf("error = %q, want it to say the value isn't valid UTF-8", err.Error())
	}
	if !strings.Contains(err.Error(), "app-id") {
		t.Errorf("error = %q, want it to name --app-id", err.Error())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d (usage error, not a server-side 500)", got, want)
	}
}

// TestValidUTF8FlagValueUnaffected is the regression guard: an ordinary flag
// value must not be rejected -- it should reach as far as authentication.
func TestValidUTF8FlagValueUnaffected(t *testing.T) {
	resetMcpServersGetCmdAppIDFlag(t)
	t.Setenv("C1I_URL", "https://example.invalid")
	t.Setenv("C1I_CLIENT_ID", "")
	t.Setenv("C1I_CLIENT_SECRET", "")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"mcp", "servers", "get", "someid", "--app-id", "a-perfectly-normal-app-id"})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected an error (no credentials configured), got nil")
	}
	if strings.Contains(err.Error(), "not valid UTF-8") {
		t.Errorf("a well-formed flag value must not be rejected by the UTF-8 check, got: %v", err)
	}
}

// TestValidateFlagsUTF8IgnoresUnchangedFlagWithInvalidDefaultValue pins the
// !f.Changed guard's actual intent: a caller must never be blamed for a
// value they did not supply. A throwaway command registers a string flag
// whose DEFAULT is deliberately invalid UTF-8; since it's never Set, Changed
// stays false, and validateFlagsUTF8 must not fire on it.
func TestValidateFlagsUTF8IgnoresUnchangedFlagWithInvalidDefaultValue(t *testing.T) {
	cmd := &cobra.Command{Use: "throwaway"}
	cmd.Flags().String("bogus", invalidUTF8Arg, "test-only flag with an invalid UTF-8 default")

	if err := validateFlagsUTF8(cmd); err != nil {
		t.Errorf("validateFlagsUTF8 = %v, want nil (the flag was never set by the caller)", err)
	}
}
