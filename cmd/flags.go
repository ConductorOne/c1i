package cmd

import (
	"fmt"
	"strconv"
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
// `limit`. A limit of 0 means "no cap"; validateCountFlags rejects a negative
// one before any command runs. Centralizing this check pins the semantics
// across every list command and gives the table tests one place to assert it.
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

const (
	// defaultPageSize is the --page-size default every list command starts
	// from unless the endpoint documents its own.
	defaultPageSize = 50

	// maxPageSize is the upper bound the C1 API enforces on `pageSize` /
	// `page_size`. Sending a higher value gets a raw "value must be inside
	// range [0, 100]" 400 from the gateway, which is hostile to first-time
	// agents trying to self-correct.
	maxPageSize = 100

	// maxHistoryPageSize is the higher bound the two MCP history endpoints
	// enforce instead ("value must be inside range [0, 200]"). Verified
	// live: page_size=200 passes validation there, 201 400s.
	maxHistoryPageSize = 200
)

// pageSizeMaxAnnotation records, on the --page-size flag itself, the ceiling
// its help text printed. pageSizeFlag clamps to that same number, so the
// documented max and the enforced max can no longer be edited apart: before
// this, the "max 200" in two commands' help and the 200 in the clamp they
// called were unrelated literals in unrelated files.
const pageSizeMaxAnnotation = "c1i_page_size_max"

// pageSizeUsage is the single source of the --page-size caveat, and the caveat
// is load-bearing: --page-size 10 returned 23 rows from `apps list`, 12 from
// `policies list` and exactly 10 from `users list`. The overshoot is server-side
// and varies by endpoint and size; --limit is exact regardless. pageSizeFlag
// clamps the upper bound and validateCountFlags rejects a negative, so a
// command only reaches the server's range check ("value must be inside range
// [0, N]") if an endpoint's real bound is below the one we advertise.
func pageSizeUsage(maxSize int) string {
	return fmt.Sprintf("Rows to request per API page (max %d); the server may return more than asked, so use --limit for an exact count", maxSize)
}

// pageTokenUsage is the shared --page-token description. Supplying the flag
// (even empty) switches the command to a single request; see the
// `manualPaging` check in every list command's RunE.
const pageTokenUsage = "Pagination cursor; supplying it fetches exactly one page (disables auto-pagination)" // #nosec G101 -- flag help text; G101 fires on the "Token" in the name

// countFlags is a slice, not a map: with both flags negative, map iteration
// order would pick which one the error names at random.
var countFlags = []struct{ name, zeroMeans string }{
	{"limit", "unlimited"},
	{"page-size", "the server's default"},
}

// validateCountFlags rejects a negative --limit or --page-size before any
// request. The asymmetry is why it lives here: --limit never reaches the API,
// so nothing but c1i could catch it. Reads pflag, so binding either flag to
// viper later would need this revisited.
func validateCountFlags(cmd *cobra.Command) error {
	for _, cf := range countFlags {
		f := cmd.Flags().Lookup(cf.name)
		if f == nil || !f.Changed {
			continue
		}
		if n := getIntFlag(cmd, cf.name); n < 0 {
			return &usageError{fmt.Errorf("--%s cannot be negative (got %d); 0 means %s", cf.name, n, cf.zeroMeans)}
		}
	}
	return nil
}

// addPaginationFlags registers the --page-size/--page-token/--limit trio on a
// list-style command. Every list command must go through this (or
// addPaginationFlagsWithMax) rather than calling Flags().Int/String itself:
// 27 hand-registrations had already drifted into five different --page-size
// wordings, none of which mentioned that a page can overrun the requested
// size. TestPaginationFlagsGoThroughSharedRegistrar enforces it.
func addPaginationFlags(cmd *cobra.Command) {
	addPaginationFlagsWithMax(cmd, defaultPageSize, maxPageSize)
}

// addPaginationFlagsWithMax is addPaginationFlags for an endpoint whose
// default page size or server-enforced ceiling genuinely differs (the MCP
// history endpoints allow 200; `policies search` defaults to 25).
func addPaginationFlagsWithMax(cmd *cobra.Command, def, maxSize int) {
	cmd.Flags().Int("page-size", def, pageSizeUsage(maxSize))
	// SetAnnotation only errors on an unregistered flag name, which the line
	// above rules out.
	_ = cmd.Flags().SetAnnotation("page-size", pageSizeMaxAnnotation, []string{strconv.Itoa(maxSize)})
	cmd.Flags().String("page-token", "", pageTokenUsage)
	addLimitFlag(cmd)
}

// pageSizeFlag returns --page-size silently capped to the ceiling that was
// registered for this command. Silent-clamp over erroring: `--page-size 500`
// is unambiguous about intent ("give me a lot") and auto-pagination still
// fetches every result — only the per-page boundary changes.
//
// A command with no registered ceiling falls back to maxPageSize; that is the
// conservative direction (every endpoint accepts 100), and the registrar test
// makes the case unreachable in practice.
func pageSizeFlag(cmd *cobra.Command) int {
	maxSize := maxPageSize
	if f := cmd.Flags().Lookup("page-size"); f != nil {
		if vals := f.Annotations[pageSizeMaxAnnotation]; len(vals) == 1 {
			if parsed, err := strconv.Atoi(vals[0]); err == nil {
				maxSize = parsed
			}
		}
	}
	if n := getIntFlag(cmd, "page-size"); n <= maxSize {
		return n
	}
	return maxSize
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

// addRepeatableStringFlag registers a repeatable string flag. It always uses
// StringArray, never StringSlice: StringSlice CSV-splits every occurrence, so
// `--user-id "" --user-id REAL` reaches the command as ["REAL"] — the empty
// occurrence is destroyed during parsing, before any command-level check can
// see it. On `apps set-owners`, which REPLACES the owner list, that set one
// owner and exited 0 while the caller had asked for two.
//
// The deliberate trade: `--flag a,b` is now ONE value, not two. No repeatable
// flag ever documented comma-splitting; see CHANGELOG.
//
// TestRepeatableStringFlagsGoThroughSharedRegistrar keeps this the only place
// such a flag is created.
func addRepeatableStringFlag(cmd *cobra.Command, name, usage string) {
	cmd.Flags().StringArray(name, nil, usage)
}

// repeatableStringFlagError is the one wording for a repeatable flag given an
// empty value, defined once so its callers cannot drift apart.
func repeatableStringFlagError(name string) error {
	return &usageError{fmt.Errorf("flag --%s requires a non-empty value for every occurrence", name)}
}

// repeatableStringFlag reads a flag registered by addRepeatableStringFlag and
// rejects an empty or whitespace-only occurrence with a *usageError (exit 2).
//
// The two rejected shapes need separate checks. A blank inside a repetition
// survives as an element (`--x "" --x REAL` -> ["", "REAL"]), but a lone
// `--x ""` reads back as an EMPTY slice: GetStringArray round-trips the value
// through a CSV string, and a single empty element serializes to "" which
// parses back as no elements at all. Only Changed distinguishes that from
// "flag never passed". Both shapes measured against pflag v1.0.10.
//
// Not passing the flag at all is not an error here: whether the flag is
// required is the command's business, and several callers treat it as optional.
func repeatableStringFlag(cmd *cobra.Command, name string) ([]string, error) {
	values, err := cmd.Flags().GetStringArray(name)
	if err != nil {
		// Wrong flag type, not user input: reporting it as an empty value would
		// send the reader to fix their command line instead of the code.
		return nil, fmt.Errorf("--%s is not a repeatable string flag: %w", name, err)
	}
	f := cmd.Flags().Lookup(name)
	if f == nil || !f.Changed {
		return values, nil
	}
	if len(values) == 0 {
		return nil, repeatableStringFlagError(name)
	}
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return nil, repeatableStringFlagError(name)
		}
	}
	return values, nil
}
