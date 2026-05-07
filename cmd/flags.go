package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// markRequired marks a flag as required AND appends "(required)" to its
// help description. Cobra's MarkFlagRequired enforces presence at runtime
// but leaves the help output unchanged, so users have no upfront signal
// from `--help` that a flag must be set. Use this everywhere instead of
// calling MarkFlagRequired directly.
func markRequired(cmd *cobra.Command, names ...string) {
	for _, n := range names {
		if f := cmd.Flags().Lookup(n); f != nil && !strings.Contains(f.Usage, "(required)") {
			f.Usage = strings.TrimRight(f.Usage, ". ") + " (required)"
		}
		_ = cmd.MarkFlagRequired(n)
	}
}

// requireNonEmpty errors when any of the named string flags has an empty
// value. Cobra's required-flag enforcement only checks that the flag was
// set, not that the value is non-empty — so `--app-id ""` silently passes.
// On commands like `accounts list`, that bypass causes the request to be
// sent without any app filter, which pulls every account in the tenant.
// Call this at the top of RunE for any required string flag whose empty
// value would over-fetch or otherwise misbehave.
func requireNonEmpty(cmd *cobra.Command, names ...string) error {
	var missing []string
	for _, n := range names {
		v, _ := cmd.Flags().GetString(n)
		if v == "" {
			missing = append(missing, "--"+n)
		}
	}
	switch len(missing) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("flag %s requires a non-empty value", missing[0])
	default:
		return fmt.Errorf("flags %s require non-empty values", strings.Join(missing, ", "))
	}
}
