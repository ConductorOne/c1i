package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// kvOpt tunes parseKeyValueFlag.
type kvOpt int

// jsonKeyValues marks a flag whose pairs become JSON object entries, not HTTP
// headers: values are unchecked and a key is refused only for control characters.
const jsonKeyValues kvOpt = 1

func isASCIIControl(r rune) bool { return r < 0x20 || r == 0x7f }

// checkNoControl refuses a control character in text (tab too unless
// allowTab). what is "name", "value" or "key"; subject, if set, is quoted. The
// error gives the character and byte offset, never the text: it may be a secret.
func checkNoControl(where, subject, what, text string, allowTab bool) error {
	for at, r := range text {
		if isASCIIControl(r) && (!allowTab || r != '\t') {
			if subject != "" {
				subject = fmt.Sprintf(" %q", subject)
			}
			return &usageError{fmt.Errorf("invalid %s%s: %s has control character U+%04X at byte %d", where, subject, what, r, at)}
		}
	}
	return nil
}

// checkHeaderText is the one rule for text that becomes an HTTP header. A name
// must be an RFC 9110 token (tchar only); a value refuses control characters
// except tab (legal per RFC 9110). where names the source ("--header", or a
// JSON path). Either may be a secret (`--header "Authorization: Bearer x"`), so
// errors never echo the text; key, a name already checked, is quoted for a value.
func checkHeaderText(where, key, text string, isName bool) error {
	if !isName {
		return checkNoControl(where, key, "value", text, true)
	}
	if err := checkNoControl(where, "", "name", text, false); err != nil {
		return err
	}
	for at, r := range text {
		if !isTokenChar(r) {
			return &usageError{fmt.Errorf("invalid %s: name has invalid character U+%04X at byte %d (a header name is letters, digits and !#$%%&'*+-.^_`|~)", where, r, at)}
		}
	}
	return nil
}

// isTokenChar reports whether r is an RFC 9110 tchar.
func isTokenChar(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r < 0x80 && strings.ContainsRune("!#$%&'*+-.^_`|~", r)
}

// headerTextAnnotation marks a single-value flag that becomes an HTTP header
// name ("name") or value ("value"); validateHeaderFlags checks it centrally.
const headerTextAnnotation = "c1i_header_text"

// validateHeaderFlags applies checkHeaderText to every set flag annotated as a
// header name or value, before any request or --dry-run preview.
func validateHeaderFlags(cmd *cobra.Command) error {
	var err error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		a := f.Annotations[headerTextAnnotation]
		if err != nil || !f.Changed || len(a) == 0 {
			return
		}
		v := f.Value.String()
		err = checkHeaderText("--"+f.Name, "", v, a[0] == "name")
	})
	return err
}

// pathFlagNames are the string flags that name a file or directory without the
// "-file" suffix. TestEveryPathLikeFlagIsCovered fails on a path flag this misses.
var pathFlagNames = map[string]bool{"output": true, "out-dir": true, "file": true}

// isPathFlag reports whether f names a file or directory ("-" is stdin/stdout).
// Repeatable path flags are StringArray/StringSlice values, so the guard must
// inspect every element, not just scalar string flags.
func isPathFlag(f *pflag.Flag) bool {
	if !pathFlagNames[f.Name] && !strings.HasSuffix(f.Name, "-file") {
		return false
	}
	switch f.Value.Type() {
	case "string", "stringArray", "stringSlice":
		return true
	default:
		return false
	}
}

// validatePathFlags refuses a set path flag holding a control character: it
// would create or read a file whose name carries a newline, silently.
func validatePathFlags(cmd *cobra.Command) error {
	var err error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if err != nil || !f.Changed || !isPathFlag(f) {
			return
		}
		values := []string{f.Value.String()}
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			values = sv.GetSlice()
		}
		for _, value := range values {
			if err = checkNoControl("--"+f.Name, "", "path", value, false); err != nil {
				return
			}
		}
	})
	return err
}

// cutKeyValue splits one "key=value" occurrence of flagName. The error never
// echoes the input: a mistyped pair can be a secret (a token missing its "=").
func cutKeyValue(flagName, p string) (string, string, error) {
	k, v, ok := strings.Cut(p, "=")
	switch {
	case !ok:
		return "", "", &usageError{fmt.Errorf(`invalid --%s: expected key=value, but a value has no "="`, flagName)}
	case k == "":
		return "", "", &usageError{fmt.Errorf("invalid --%s: expected key=value, but a key is empty", flagName)}
	}
	return k, v, nil
}

// parseKeyValueFlag parses repeated "key=value" flag values into a map. flagName
// only makes errors point at the offending flag.
func parseKeyValueFlag(pairs []string, flagName string, opts ...kvOpt) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	m := make(map[string]string, len(pairs))
	jsonPairs := slices.Contains(opts, jsonKeyValues)
	for _, p := range pairs {
		k, v, err := cutKeyValue(flagName, p)
		if err != nil {
			return nil, err
		}
		if jsonPairs {
			if err := checkNoControl("--"+flagName, "", "key", k, false); err != nil {
				return nil, err
			}
			m[k] = v
			continue
		}
		if err := checkHeaderText("--"+flagName, "", k, true); err != nil {
			return nil, err
		}
		if err := checkHeaderText("--"+flagName, k, v, false); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}

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
// Only the tree tests read it, to hold usage text and enforcement together.
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
	emptyIsSent       = "an empty value is sent as given (an update's clears the field where --help says so)"
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

// flagGiven reports whether the user passed a flag meaning something: Changed,
// except that an exact "" on an emptyIsOmitted flag is the same as omitting it,
// as --help promises. Use it for mode and exclusivity checks, not Changed.
func flagGiven(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	if f == nil {
		f = cmd.PersistentFlags().Lookup(name)
	}
	if f == nil || !f.Changed {
		return false
	}
	if r := f.Annotations[emptyAllowedAnnotation]; len(r) == 1 && r[0] == emptyIsOmitted && f.Value.String() == "" {
		return false
	}
	return true
}

// annotateRequired appends "(required)" to each named flag's usage WITHOUT
// enabling cobra's pre-run required-flag validation. Use it when the command,
// not cobra, enforces presence in RunE (via requireNonEmpty or its own check),
// e.g. register, whose --print-config-template escape hatch must run without
// the otherwise-required flags.
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
// every list command's RunE. A blank value is refused by validateBlankFlags:
// supplying the flag at all turns auto-pagination off, so a blank token
// silently truncated the list.
const pageTokenUsage = "Pagination cursor; a non-empty value fetches exactly one page (disables auto-pagination); a blank value is refused, so omit it to auto-paginate" // #nosec G101 -- flag help text; G101 fires on the "Token" in the name

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
	setBlankHint(cmd, "page-token", "omit the flag to auto-paginate, or pass a non-empty cursor to fetch exactly one page")
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
// only for flags the command itself requires (annotateRequired, or required in
// one mode), which cobra does not presence-check; a SET blank required or
// id-shaped flag is already refused by validateBlankFlags. An unset flag reads
// like cobra's own missing-required-flag error; a given-but-blank one says so.
// The error is a *usageError (exit 2); callers just `return err`.
func requireNonEmpty(cmd *cobra.Command, names ...string) error {
	var unset, blank []string
	for _, n := range names {
		v, _ := cmd.Flags().GetString(n)
		if hasVisibleRune(v) {
			continue
		}
		if cmd.Flags().Changed(n) {
			blank = append(blank, "--"+n)
		} else {
			unset = append(unset, strconv.Quote(n))
		}
	}
	var msgs []string
	if len(unset) > 0 {
		msgs = append(msgs, fmt.Sprintf("required flag(s) %s not set", strings.Join(unset, ", ")))
	}
	switch len(blank) {
	case 0:
	case 1:
		msgs = append(msgs, fmt.Sprintf("flag %s requires a non-empty value", blank[0]))
	default:
		msgs = append(msgs, fmt.Sprintf("flags %s require non-empty values", strings.Join(blank, ", ")))
	}
	if len(msgs) == 0 {
		return nil
	}
	return &usageError{errors.New(strings.Join(msgs, "; "))}
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
			// With no field flags there is nothing to derive a mask from.
			if len(exclusive) == 0 {
				return "", false, &usageError{fmt.Errorf("--update-mask requires --body-file; pass --body-file with --update-mask")}
			}
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
	mask, err = normalizeUpdateMask(mask)
	return mask, true, err
}

// normalizeUpdateMask trims each comma-separated path (the server rejects
// " description" as an invalid path) and refuses an empty path or an empty
// dot-segment, naming the position; spaces inside a segment go to the server
// as given. It is the one check every --update-mask
// value goes through. Unlike --fields, a doubled or trailing comma is an error:
// a mask scopes a write. Names are sent as given; the server judges them.
func normalizeUpdateMask(mask string) (string, error) {
	paths := strings.Split(mask, ",")
	for i, p := range paths {
		p = strings.TrimSpace(p)
		if !hasVisibleRune(p) {
			return "", &usageError{fmt.Errorf("--update-mask %q: path %d is empty (paths are comma-separated, e.g. a,b)", mask, i+1)}
		}
		if _, bad := splitPathSegments(p); bad != 0 {
			return "", &usageError{fmt.Errorf("--update-mask %q: path %d, segment %d is empty (segments are dot-separated, e.g. a.b)", mask, i+1, bad)}
		}
		paths[i] = p
	}
	return strings.Join(paths, ","), nil
}

// requireMaskCoversConfig refuses an explicit mask that leaves out a top-level
// key of the config c1i is sending, which the server would silently not apply.
// A path covers a key when it is the key or nested under it, compared without
// case or underscores so camelCase and snake_case spellings match (the mask
// itself is sent as given). The catalog id is required by the API but never a
// field being changed.
func requireMaskCoversConfig(mask string, cfg map[string]any) error {
	var uncovered []string
	for key := range cfg {
		if key == "mcpServerCatalogId" {
			continue
		}
		if !hasVisibleRune(key) {
			return &usageError{fmt.Errorf("a config key is empty; it cannot be named in --update-mask")}
		}
		covered := false
		for _, p := range strings.Split(mask, ",") {
			p, k := foldMaskName(p), foldMaskName(key)
			if p == k || strings.HasPrefix(p, k+".") {
				covered = true
				break
			}
		}
		if !covered {
			uncovered = append(uncovered, key)
		}
	}
	if len(uncovered) == 0 {
		return nil
	}
	sort.Strings(uncovered)
	return &usageError{fmt.Errorf("--update-mask %q does not name %s, which this command would send and the server would not apply; add it to the mask or drop it", mask, strings.Join(uncovered, ", "))}
}

func foldMaskName(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", ""))
}

// optionalUpdateMask reads an --update-mask that may be left out ("" means
// omitted), normalized by normalizeUpdateMask.
func optionalUpdateMask(cmd *cobra.Command) (string, error) {
	mask, _ := cmd.Flags().GetString("update-mask")
	if mask == "" {
		return "", nil
	}
	return normalizeUpdateMask(mask)
}
