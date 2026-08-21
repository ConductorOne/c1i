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

// annotateRequired appends "(required)" to each named flag's usage WITHOUT
// enabling cobra's pre-run required-flag validation. Use it when a command has
// an escape-hatch flag (e.g. register's --print-config-template) that is valid
// on its own without the otherwise-required flags: cobra validates required
// flags before RunE runs, so the escape hatch could never short-circuit if the
// flags were cobra-required. The command must enforce presence itself in RunE
// (via requireNonEmpty).
func annotateRequired(cmd *cobra.Command, names ...string) {
	for _, n := range names {
		if f := cmd.Flags().Lookup(n); f != nil && !strings.Contains(f.Usage, "(required)") {
			f.Usage = strings.TrimRight(f.Usage, ". ") + " (required)"
		}
	}
}

// limitReached reports whether `emitted` rows have hit the requested
// `limit`. A limit of 0 (or negative) means "no cap". Centralizing this
// check pins the semantics across every list command and gives the
// table tests one place to assert behavior.
func limitReached(emitted, limit int) bool {
	return limit > 0 && emitted >= limit
}

// effectivePageSize tightens the per-call page size toward `limit` when
// `limit` is smaller than the requested page size, so an ordinary
// `--limit 3` query doesn't fetch 50 items and discard 47 of them. This is
// only correct when every fetched row becomes one written row — that
// one-to-one assumption is what lets "3 remaining" mean "ask for 3 more".
//
// A caller whose rows can be dropped after fetching (a client-side filter,
// or a --fields projection that empties a row — see emitter.Filtered) must
// NOT call this at all while that filtering is active: `written` stays near
// zero while filtered rows keep coming in, so `limit - written` stays near
// `limit` for the whole scan and every page ends up asking for `limit` (or
// fewer) items instead of the real page size — turning what should be a
// handful of full pages into many tiny, mostly-wasted ones. Skip the call
// and pass `requested` straight through instead; see the callers that check
// `clientFilter`/`enc.Filtered()` before calling this.
//
// General rule this function keeps getting fed the wrong counter for: a
// counter driving a REQUEST-SHAPING decision (this function) must count
// rows fetched (or, in the safe 1:1 case, rows written — same number); a
// counter driving a STOP decision (limitReached) must count rows actually
// written. Conflating the two is the recurring bug.
//
// When `limit` is unset (<=0), the requested page-size is returned
// unchanged.
//
// Edge: if the remaining headroom (limit - written) is somehow zero or
// negative (the outer loop should have stopped already), we still return
// at least 1 — better to make a small wasteful call than to send
// pageSize=0 and get an undefined response from the API.
func effectivePageSize(requested, limit, written int) int {
	if limit <= 0 {
		return requested
	}
	remaining := limit - written
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
