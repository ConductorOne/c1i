package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestNoSubcommandDefinesOwnPersistentPostRunE guards an invariant the
// --fields zero-match-in-list check (ledger C30, checkFieldsMatchedAnyRow in
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
