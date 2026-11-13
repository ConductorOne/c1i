package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
)

// markRequired marks a flag as required AND appends "(required)" to its
// help description. Cobra's MarkFlagRequired enforces presence at runtime
// but leaves the help output unchanged, so users have no upfront signal
// from `--help` that a flag must be set. Use this everywhere instead of
// calling MarkFlagRequired directly.
func markRequired(cmd *cobra.Command, names ...string) {
	annotateRequired(cmd, names...)
	for _, n := range names {
		_ = cmd.MarkFlagRequired(n)
	}
}

// requiredAnnotation tags every flag documented "(required)", whether cobra
// enforces its presence (markRequired) or the command does (annotateRequired).
// validateBlankFlags reads it, so a required flag is detected one way.
const requiredAnnotation = "c1i_required"

// blankHintAnnotation holds text validateBlankFlags appends to a flag's
// blank-value error.
const blankHintAnnotation = "c1i_blank_hint"

// updateMaskBlankHint is the blank-value hint for a --body-file update mask.
const updateMaskBlankHint = "name the changed field paths, comma-separated"

// setBlankHint appends hint to flag name's blank-value error.
func setBlankHint(cmd *cobra.Command, name, hint string) {
	if err := cmd.Flags().SetAnnotation(name, blankHintAnnotation, []string{hint}); err != nil {
		panic(err)
	}
}

// emptyAllowedAnnotation lets a string flag take an exact "" past
// validateBlankFlags; whitespace or invisible-only is still refused. Its value
// is the reason; set it with allowEmpty.
const emptyAllowedAnnotation = "c1i_empty_allowed"

// allowEmpty reasons. Filters never take one: an empty filter is refused, so
// `--state "$UNSET"` cannot silently widen a list.
const (
	emptyClears       = "an empty value is sent and clears the field"
	emptyIsOmitted    = "an empty value is the same as omitting the flag"
	emptyOverridesEnv = "an empty value overrides the environment variable and config file"
)

// allowEmpty lets each named flag take an exact "", where that clears a field,
// overrides an env default, or stands for "not given". Every other string flag
// refuses any blank value.
func allowEmpty(cmd *cobra.Command, reason string, names ...string) {
	if !hasVisibleRune(reason) || len(names) == 0 {
		panic("allowEmpty: a reason and at least one flag are required")
	}
	for _, n := range names {
		fs := cmd.Flags()
		if fs.Lookup(n) == nil {
			fs = cmd.PersistentFlags()
		}
		if err := fs.SetAnnotation(n, emptyAllowedAnnotation, []string{reason}); err != nil {
			panic(err)
		}
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
		if f := cmd.Flags().Lookup(n); f != nil {
			if !strings.Contains(f.Usage, "(required)") {
				f.Usage = strings.TrimRight(f.Usage, ". ") + " (required)"
			}
			_ = cmd.Flags().SetAnnotation(n, requiredAnnotation, []string{"true"})
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

// pageTokenUsage is the shared --page-token description. A non-empty value
// switches the command to a single request; see the `manualPaging` check in
// every list command's RunE. A blank value is refused before the command runs
// (validatePageToken): supplying the flag at all turns auto-pagination off, so
// a blank token silently truncated the list.
const pageTokenUsage = "Pagination cursor; a non-empty value fetches exactly one page (disables auto-pagination); a blank value is refused, so omit it to auto-paginate" // #nosec G101 -- flag help text; G101 fires on the "Token" in the name

// validatePageToken rejects a --page-token that is set but blank (empty,
// whitespace-only, or invisible-only). Every list command reads the cursor
// with `manualPaging := cmd.Flags().Changed("page-token")`, so an empty value
// from an unset shell variable fetched only the first page and exited 0 --
// a silently truncated list that looks exactly like a filtered one. Omitting
// the flag is how you ask for every page.
func validatePageToken(cmd *cobra.Command) error {
	f := cmd.Flags().Lookup("page-token")
	if f == nil || !f.Changed || hasVisibleRune(f.Value.String()) {
		return nil
	}
	return &usageError{errors.New("--page-token is blank: omit the flag to auto-paginate, or pass a non-empty cursor to fetch exactly one page")}
}

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

// stuckPageRepeats and stuckStaleRows together mark a cursor as stuck: that
// many consecutive stale pages (every item already seen) holding at least
// that many rows in all. One stale page can be a healthy offset-paginated
// server that gained a page of rows between requests; the row floor keeps a
// tiny --page-size on a busy newest-first feed from tripping on a few inserts.
const (
	stuckPageRepeats = 2
	stuckStaleRows   = 25
)

// maxIdlePages backstops a cursor that keeps changing without bringing a new
// row (or, for id-less rows, any cursor that never ends): it counts requests
// since the last page with an unseen id. No healthy list sends 100k pages
// without one; the audit log, whose rows carry no id, opts out
// (newUncappedPaginationCursorGuard).
const maxIdlePages = 100_000

// maxEmptyPages bounds a run of consecutive empty pages under fresh tokens.
// A healthy server sends at most a few before its last page; 100 leaves room
// for sparse server-side filtering.
const maxEmptyPages = 100

// paginationCursorGuard detects auto-pagination that stopped making progress:
// a page_token sent twice, a run of stale or empty pages under fresh-looking
// tokens, or maxPages requests without a new id. Construct with
// newPaginationCursorGuard.
type paginationCursorGuard struct {
	seenTokens   map[string]bool
	seenIDs      map[string]bool
	stale        int
	staleRows    int
	empty        int
	idle         int
	pages        int
	maxPages     int    // 0 = no idle or empty-page cap
	compositeIDs bool   // see withCompositeIDs
	stopReason   string // the cap that ended the run; "" for a repeat
}

func newPaginationCursorGuard() *paginationCursorGuard {
	return &paginationCursorGuard{seenTokens: map[string]bool{}, seenIDs: map[string]bool{}, maxPages: maxIdlePages}
}

// newUncappedPaginationCursorGuard is for a stream with no natural size bound
// (the audit log): only the repeated-token and stale-page checks apply, since
// a quiet stretch of the log may page through many empty results.
func newUncappedPaginationCursorGuard() *paginationCursorGuard {
	g := newPaginationCursorGuard()
	g.maxPages = 0
	return g
}

// tokenAlreadyUsed reports whether token was already sent, or the idle-page
// cap is spent. Call it once before each request, so a repeat is caught
// without re-fetching the page.
func (g *paginationCursorGuard) tokenAlreadyUsed(token string) bool {
	g.idle++
	if g.maxPages > 0 && g.idle > g.maxPages {
		g.stopReason = fmt.Sprintf("no new rows in %d pages", g.maxPages)
		return true
	}
	return token != "" && g.seenTokens[token]
}

// markTokenUsed records the token the request just sent.
func (g *paginationCursorGuard) markTokenUsed(token string) {
	if token != "" {
		g.seenTokens[token] = true
	}
}

// withCompositeIDs marks ids built from several fields (or expanded objects),
// where a row missing one is plausible: a page of only id-less rows is then
// emitted rather than rejected as an unrecognized shape.
func (g *paginationCursorGuard) withCompositeIDs() *paginationCursorGuard {
	g.compositeIDs = true
	return g
}

// checkPage takes a page's row ids ("" for a row without one) and returns
// which rows to emit. A page with any unseen id is emitted whole, since ids
// need not be unique. A page whose ids were all seen is stale: only its
// id-less rows, which can't be shown to be repeats, are emitted. The listing
// ends once a stale run meets stuckPageRepeats and stuckStaleRows, or after
// maxEmptyPages empty pages in a row. Unless withCompositeIDs, a non-empty
// page with no id at all means the command no longer finds the id it reads,
// so it fails rather than print unchecked rows. A nil cmd skips the stderr
// warning.
func (g *paginationCursorGuard) checkPage(cmd *cobra.Command, route string, ids []string) ([]bool, error) {
	g.pages++
	keep := make([]bool, len(ids))
	fresh, known := false, 0
	for i, id := range ids {
		if id == "" {
			keep[i] = true
			continue
		}
		known++
		if !g.seenIDs[id] {
			fresh = true
			g.seenIDs[id] = true
		}
	}
	switch {
	case len(ids) == 0:
		return keep, g.countEmpty(cmd, route)
	case known == 0 && g.compositeIDs:
		g.empty = 0
		return keep, nil
	case known == 0:
		return nil, g.shapeError(cmd, route, len(ids))
	}
	g.empty = 0
	if !fresh {
		g.stale++
		g.staleRows += known
		if g.stale >= stuckPageRepeats && g.staleRows >= stuckStaleRows {
			return nil, g.warnStuck(cmd, route)
		}
		return keep, nil
	}
	g.stale, g.staleRows, g.idle = 0, 0, 0
	for i := range keep {
		keep[i] = true
	}
	return keep, nil
}

// inStaleRun reports whether no page since the last new id brought one.
func (g *paginationCursorGuard) inStaleRun() bool {
	return g.stale > 0
}

// shapeError is the exit-6 error for a page with rows but no ids; past the
// first page it also reports the result as incomplete.
func (g *paginationCursorGuard) shapeError(cmd *cobra.Command, route string, rows int) error {
	problem := fmt.Sprintf("%d rows came back without the id c1i reads; the response shape is not one this command recognizes", rows)
	if g.pages == 1 {
		return &nonJSONResponseError{fmt.Errorf("%s: %s", route, problem)}
	}
	if cmd != nil {
		notice(cmd, noticeInfo, fmt.Sprintf(
			"Warning: results from %s are INCOMPLETE: %s. Rows already printed are kept.", route, problem), nil)
	}
	return &nonJSONResponseError{fmt.Errorf("results from %s are INCOMPLETE: %s", route, problem)}
}

// checkIDlessPage is checkPage for a command whose rows carry no id: only the
// empty-page bound applies.
func (g *paginationCursorGuard) checkIDlessPage(cmd *cobra.Command, route string, rows int) error {
	if rows > 0 {
		g.empty = 0
		return nil
	}
	return g.countEmpty(cmd, route)
}

func (g *paginationCursorGuard) countEmpty(cmd *cobra.Command, route string) error {
	g.empty++
	if g.maxPages == 0 || g.empty < maxEmptyPages {
		return nil
	}
	g.stopReason = fmt.Sprintf("%d empty pages in a row", g.empty)
	return g.warnStuck(cmd, route)
}

// keepRows returns the rows checkPage marked to emit.
func keepRows[T any](rows []T, keep []bool) []T {
	out := make([]T, 0, len(rows))
	for i, r := range rows {
		if keep[i] {
			out = append(out, r)
		}
	}
	return out
}

// stuckError is the exit-6 error for this guard's stop, naming the cap when
// that is what tripped.
func (g *paginationCursorGuard) stuckError(route string) error {
	return &stuckPaginationCursorError{route: route, reason: g.stopReason}
}

// warnStuck prints the stuck-cursor warning (unless cmd is nil) and returns
// stuckError.
func (g *paginationCursorGuard) warnStuck(cmd *cobra.Command, route string) error {
	if cmd != nil {
		reason := "the server's pagination cursor stopped advancing (a repeated nextPageToken, or repeated pages of rows already seen)"
		if g.stopReason != "" {
			reason = g.stopReason
		}
		notice(cmd, noticeInfo, fmt.Sprintf(
			"Warning: results from %s are INCOMPLETE: %s. Rows already printed are kept.", route, reason), nil)
	}
	return g.stuckError(route)
}

// rawItemID returns a raw JSON row's top-level string "id", or "" (which
// checkPage treats as id-less) when it has none.
func rawItemID(item []byte) string {
	var withID struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(item, &withID)
	return withID.ID
}

// scopedRowID keys a row whose id is unique only in combination: the parts
// joined by NUL, or "" (id-less to checkPage) if any part is empty.
func scopedRowID(parts ...string) string {
	for _, p := range parts {
		if p == "" {
			return ""
		}
	}
	return strings.Join(parts, "\x00")
}

// requireNonEmpty errors when any named string flag is unset or blank. Use it
// only for annotateRequired flags, which cobra does not presence-check; a SET
// blank required or id-shaped flag is already refused by validateBlankFlags.
// The error is a *usageError (exit 2); callers just `return err`.
func requireNonEmpty(cmd *cobra.Command, names ...string) error {
	var missing []string
	for _, n := range names {
		v, _ := cmd.Flags().GetString(n)
		if !hasVisibleRune(v) {
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

// displayNameRule is the one wording for every --display-name: blank exits 2
// before sending, via validateBlankFlags.
const displayNameRule = "cannot be blank: empty, whitespace or invisible-only"

// hasVisibleRune reports whether s has a rune that renders. TrimSpace alone
// lets through zero-width/format runes (U+200B, U+FEFF), i.e. an invisible name.
func hasVisibleRune(s string) bool {
	for _, r := range s {
		if unicode.IsGraphic(r) && !unicode.IsSpace(r) && !unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}

// flagOrDefault returns a string flag's value, falling back to def when unset.
func flagOrDefault(cmd *cobra.Command, name, def string) string {
	if v, _ := cmd.Flags().GetString(name); v != "" {
		return v
	}
	return def
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

// onceStringValue is a string flag value that refuses a second occurrence,
// which pflag would otherwise resolve silently to the last one.
type onceStringValue struct {
	val, hint string
	set       bool
}

func (o *onceStringValue) String() string { return o.val }
func (o *onceStringValue) Type() string   { return "string" }

// Reset exists for test isolation: cmd flags are package-level, so the harness
// resets them between runs.
func (o *onceStringValue) Reset() { o.val, o.set = "", false }

func (o *onceStringValue) Set(v string) error {
	if o.set {
		return errors.New("can be given once; " + o.hint)
	}
	o.val, o.set = v, true
	return nil
}

// addOnceStringFlag registers a string flag that exits 2 when repeated; hint
// says what to do instead.
func addOnceStringFlag(cmd *cobra.Command, name, usage, hint string) {
	cmd.Flags().Var(&onceStringValue{hint: hint}, name, usage)
}

// repeatableStringFlagError is the one wording for a repeatable flag given an
// empty value, defined once so its callers cannot drift apart.
func repeatableStringFlagError(name string) error {
	return &usageError{fmt.Errorf("flag --%s requires a non-empty value for every occurrence", name)}
}

// repeatableStringFlag reads a flag registered by addRepeatableStringFlag and
// rejects a blank (see hasVisibleRune) occurrence with a *usageError (exit 2).
//
// The two rejected shapes need separate checks. A blank inside a repetition
// survives as an element (`--x "" --x REAL` -> ["", "REAL"]), but a lone
// `--x ""` reads back as an EMPTY slice: GetStringArray round-trips the value
// through a CSV string, and a single empty element serializes to "" which
// parses back as no elements at all. Only Changed distinguishes that from
// "flag never passed". Both shapes hold on pflag v1.0.10 and v1.1.0.
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
		if !hasVisibleRune(v) {
			return nil, repeatableStringFlagError(name)
		}
	}
	return values, nil
}

// protoDurationAnnotation marks a flag registered by addProtoDurationFlag;
// protoDurationFlag refuses to read any other, so a walk of the command tree
// finds every such flag.
const protoDurationAnnotation = "c1i_proto_duration"

// addProtoDurationFlag registers a string flag the API parses as a
// google.protobuf.Duration. Read it with protoDurationFlag.
func addProtoDurationFlag(cmd *cobra.Command, name, usage string) {
	cmd.Flags().String(name, "", usage)
	_ = cmd.Flags().SetAnnotation(name, protoDurationAnnotation, []string{"true"})
}

// wholeDuration is Go's duration syntax restricted to unsigned whole-number
// components without leading zeros, so ParseDuration is exact and never
// negative.
var wholeDuration = regexp.MustCompile(`^(0|((0|[1-9][0-9]*)(ns|us|µs|μs|ms|s|m|h))+)$`)

// protoDurationFlag reads a flag registered by addProtoDurationFlag. It
// accepts a whole-number Go ("8h", "1h30m") or protobuf ("3600s") duration and
// returns it in seconds, the only form the API parses; set is false when the
// flag was not passed. Zero is sent: range limits are the server's call.
// A sign, a decimal or a leading zero is a usage error.
func protoDurationFlag(cmd *cobra.Command, name string) (value string, set bool, err error) {
	f := cmd.Flags().Lookup(name)
	if f == nil || f.Annotations[protoDurationAnnotation] == nil {
		return "", false, fmt.Errorf("--%s was not registered with addProtoDurationFlag", name)
	}
	if !f.Changed {
		return "", false, nil
	}
	raw := f.Value.String()
	trimmed := strings.TrimSpace(raw)
	if !wholeDuration.MatchString(trimmed) {
		return "", false, &usageError{fmt.Errorf("--%s must be a whole-number duration such as 8h or 1h30m (no sign, no decimals), got %q", name, raw)}
	}
	d, err := time.ParseDuration(trimmed)
	if err != nil {
		return "", false, &usageError{fmt.Errorf("invalid --%s %q: %w", name, raw, err)}
	}
	return formatProtoDuration(d), true, nil
}

// formatProtoDuration renders d in protobuf JSON form: integer seconds plus
// any nanosecond fraction, trailing zeros trimmed.
func formatProtoDuration(d time.Duration) string {
	// Integer arithmetic: float64 seconds drops nanoseconds on large values.
	secs := strconv.FormatInt(int64(d/time.Second), 10)
	if ns := int64(d % time.Second); ns != 0 {
		secs += "." + strings.TrimRight(fmt.Sprintf("%09d", ns), "0")
	}
	return secs + "s"
}

// bodyFileMaskUsage is the --update-mask help for commands that derive the
// mask when no --body-file is given.
const bodyFileMaskUsage = "Comma-separated field paths (required with --body-file; rejected without it; derived otherwise)"

// bodyFileMask applies the --body-file/--update-mask rules every masked update
// shares. usingBody is true when --body-file was passed (even as ""); the
// caller then reads the file. exclusive names the flags --body-file cannot be
// combined with. The mask comes back trimmed per path, as the server rejects
// " description" as an invalid path.
func bodyFileMask(cmd *cobra.Command, exclusive ...string) (mask string, usingBody bool, err error) {
	f := cmd.Flags()
	mask, _ = f.GetString("update-mask")
	if !f.Changed("body-file") {
		if f.Changed("update-mask") {
			return "", false, &usageError{fmt.Errorf("--update-mask requires --body-file; the mask is derived from the other flags")}
		}
		return "", false, nil
	}
	for _, name := range exclusive {
		if f.Changed(name) {
			return "", true, &usageError{fmt.Errorf("--body-file is mutually exclusive with --%s", strings.Join(exclusive, "/--"))}
		}
	}
	if !f.Changed("update-mask") {
		return "", true, &usageError{fmt.Errorf("--update-mask is required together with --body-file")}
	}
	if !hasVisibleRune(mask) {
		return "", true, &usageError{errors.New("--update-mask is blank; " + updateMaskBlankHint)}
	}
	paths := strings.Split(mask, ",")
	for i, p := range paths {
		paths[i] = strings.TrimSpace(p)
		if !hasVisibleRune(paths[i]) {
			return "", true, &usageError{fmt.Errorf("--update-mask %q has an empty path", mask)}
		}
	}
	return strings.Join(paths, ","), true, nil
}
