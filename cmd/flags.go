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

// limitReached reports whether `emitted` rows have hit the requested
// `limit`. A limit of 0 (or negative) means "no cap". Centralizing this
// check pins the semantics across every list command and gives the
// table tests one place to assert behavior.
func limitReached(emitted, limit int) bool {
	return limit > 0 && emitted >= limit
}

// effectivePageSize tightens the per-call page size when --limit is
// smaller than --page-size, so a `--limit 3` query doesn't fetch 50
// items and discard 47 of them. When `limit` is unset (<=0), the
// requested page-size is returned unchanged.
//
// Edge: if the remaining headroom (limit - emitted) is somehow zero or
// negative (the outer loop should have stopped already), we still return
// at least 1 — better to make a small wasteful call than to send
// pageSize=0 and get an undefined response from the API.
func effectivePageSize(requested, limit, emitted int) int {
	if limit <= 0 {
		return requested
	}
	remaining := limit - emitted
	if remaining < 1 {
		return 1
	}
	if remaining < requested {
		return remaining
	}
	return requested
}

// addLimitFlag adds the standard --limit flag to a list-style command.
// 0 means "no cap" (the default). When set, the command stops emitting
// rows after `limit` items have been written AND breaks out of the
// auto-pagination loop early so no extra API pages are fetched. Use
// alongside --page-size: --page-size controls per-call batch size,
// --limit caps the total output.
func addLimitFlag(cmd *cobra.Command) {
	cmd.Flags().Int("limit", 0, "Cap total number of results (0 = unlimited)")
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
//
// The returned error is a *usageError, so an empty required value maps to the
// usage exit code (2) — the same as a missing required flag — consistently
// across every call site. Callers just `return err`; they must not re-wrap it.
//
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
		return &usageError{fmt.Errorf("flag %s requires a non-empty value", missing[0])}
	default:
		return &usageError{fmt.Errorf("flags %s require non-empty values", strings.Join(missing, ", "))}
	}
}
