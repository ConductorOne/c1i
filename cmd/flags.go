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

// getIntFlag returns the int flag's value, ignoring the lookup error
// (cobra only errors when the flag isn't registered, which is a
// programming bug — there's nothing useful to do at runtime).
func getIntFlag(cmd *cobra.Command, name string) int {
	v, _ := cmd.Flags().GetInt(name)
	return v
}

// maxPageSize is the upper bound the C1 API enforces on `pageSize` /
// `page_size`. Sending a higher value gets a raw "must be inside [0, 100]"
// error from the gateway, which is hostile to first-time agents trying to
// self-correct.
const maxPageSize = 100

// clampPageSize silently caps a user-provided --page-size to maxPageSize,
// matching the API's hard limit. We choose silent-clamp over erroring
// because a list command with --page-size 500 is unambiguous about
// intent ("give me a lot") and the auto-paginate behavior will still
// fetch every result; only the per-page boundary changes.
func clampPageSize(n int) int {
	if n > maxPageSize {
		return maxPageSize
	}
	return n
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
