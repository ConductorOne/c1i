package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// dryRunActive reports whether --dry-run / C1I_DRY_RUN is set. Mutating commands
// check it just before their write call and, when true, preview the request via
// printDryRun instead of sending it.
func dryRunActive() bool {
	return viper.GetBool("dry_run")
}

// printDryRun writes a human-readable preview of a mutating request — method,
// path, and (when present) the pretty-printed JSON body — to stdout and returns
// nil. Callers use it as:
//
//	if dryRunActive() {
//		return printDryRun(cmd, "POST", path, body)
//	}
//
// A nil body is omitted (e.g. DELETE), matching what would actually be sent.
func printDryRun(cmd *cobra.Command, method, path string, body any) error {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "[dry-run] %s %s\n", method, path)
	if body != nil {
		b, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling dry-run body: %w", err)
		}
		_, _ = fmt.Fprintf(out, "%s\n", b)
	}
	return nil
}
