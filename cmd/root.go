package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "c1i",
	Short: "C1.ai CLI",
	Long: `c1i is a command-line interface for the C1.ai API.

If you are an AI agent, run "c1i docs agents" first — it covers conventions
this help text can't (output contracts, exit codes, when to prefer a
first-class command over raw API calls, and when to use --fields or jq). It
requires NO authentication.

For raw API exploration, also with no authentication required:

  c1i docs search "access reviews"     Search documentation by keyword
  c1i docs endpoints --filter task      List API endpoints matching a pattern
  c1i docs endpoint /api/v1/tasks/{id}  Show full request/response schema
  c1i docs page product/admin/campaigns Fetch a documentation page`,
	SilenceUsage:  true,
	SilenceErrors: true,
	// Validate global flag values once, before any command runs, attach a
	// fresh *fieldsMatchState (cmd/fields.go) to the command's context so
	// every emitter created during this invocation shares one tracker, and
	// reject a non-UTF-8 positional argument or flag value client-side (see
	// validateArgsUTF8 / validateFlagsUTF8) before it can reach a command's
	// RunE at all -- this runs for every command (see
	// TestNoSubcommandDefinesOwnPersistentPreRunE), so both an id from
	// args[0] and a parent-scope id passed as a flag (--app-id,
	// --connector-id, ...) are covered without a per-command check. Same for
	// validateCountFlags, which needs to see every list command's --limit.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := validateErrorFormat(viper.GetString("error_format")); err != nil {
			return err
		}
		if err := validateEnvFlags(cmd); err != nil {
			return err
		}
		if err := validateArgsUTF8(args); err != nil {
			return err
		}
		if err := validateFlagsUTF8(cmd); err != nil {
			return err
		}
		if err := validateBlankArgs(cmd, args); err != nil {
			return err
		}
		if err := validateBlankFlags(cmd); err != nil {
			return err
		}
		if err := validateDotSegmentIDFlags(cmd); err != nil {
			return err
		}
		if err := validateDotSegmentIDArgs(cmd, args); err != nil {
			return err
		}
		if err := validatePageToken(cmd); err != nil {
			return err
		}
		if err := validateCountFlags(cmd); err != nil {
			return err
		}
		cmd.SetContext(withFieldsMatchState(cmd.Context()))
		return nil
	},
	// The single central hook for the --fields zero-match-in-list check
	// — see checkFieldsMatchedAnyRow (cmd/fields.go) for what it
	// does and why it must live here alone, and
	// TestNoSubcommandDefinesOwnPersistentPostRunE (cmd/root_test.go) for the
	// tree-wide guard that keeps it that way.
	PersistentPostRunE: checkFieldsMatchedAnyRow,
}

// validateErrorFormat accepts "", "text", or "json" (case-insensitive, matching
// how writeError interprets the value) and rejects anything else as a usage
// error so a typo like --error-format=jsonn fails loudly instead of silently
// falling back to text.
// validateArgsUTF8 rejects a positional argument that isn't valid UTF-8 (a
// lone UTF-16 surrogate, raw invalid bytes, ...) client-side. Without this,
// an id built from such an argument (client.Path only URL-escapes; it
// doesn't validate encoding) reaches the server as malformed request data,
// which answers with a bare 500 (exit 6) instead of the 400 every other
// hostile-but-valid-UTF-8 id gets -- a client-side mistake reporting as a
// remote failure.
func validateArgsUTF8(args []string) error {
	for _, a := range args {
		if !utf8.ValidString(a) {
			return &usageError{fmt.Errorf("argument %q is not valid UTF-8", a)}
		}
	}
	return nil
}

// validateFlagsUTF8 is validateArgsUTF8's flag-shaped twin: a parent-scope
// id (--app-id, --connector-id, ...) is a FLAG per this repo's own id-argument
// convention (see CLAUDE.md), interpolated into the request path exactly
// like a positional id -- so the positional-only check missed it. Checks
// every flag actually set on cmd (which, by the time PersistentPreRunE
// runs, includes inherited persistent flags merged in from every ancestor,
// not just cmd's own local ones) via its string form; a non-string flag
// (bool, int, ...) can't fail its own parsing into invalid UTF-8, so
// checking all of them uniformly is safe, not just id-bearing ones by name.
func validateFlagsUTF8(cmd *cobra.Command) error {
	var err error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if err != nil || !f.Changed {
			return
		}
		if v := f.Value.String(); !utf8.ValidString(v) {
			err = &usageError{fmt.Errorf("--%s value %q is not valid UTF-8", f.Name, v)}
		}
	})
	return err
}

// validateBlankArgs rejects a positional with no visible character: ExactArgs
// accepts "" and " ", and an id built from one addresses the collection.
func validateBlankArgs(cmd *cobra.Command, args []string) error {
	if isCompletionRequest(cmd) {
		return nil
	}
	for _, a := range args {
		if !hasVisibleRune(a) {
			return &usageError{fmt.Errorf("argument %q is blank: it must contain a visible character", a)}
		}
	}
	return nil
}

// isCompletionRequest reports whether cmd is cobra's hidden __complete, whose
// args legitimately include the empty word being completed.
func isCompletionRequest(cmd *cobra.Command) bool {
	return cmd.Name() == cobra.ShellCompRequestCmd || cmd.Name() == cobra.ShellCompNoDescRequestCmd
}

// validateBlankFlags rejects a set string flag that is required or id-shaped
// (name ends in -id) and has no visible character. Without it `--app-id " "`
// drops the scope and over-fetches, or addresses the wrong object. A
// blankHintAnnotation on the flag is appended to the message.
func validateBlankFlags(cmd *cobra.Command) error {
	if isCompletionRequest(cmd) {
		return nil
	}
	var err error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if err != nil || !f.Changed || f.Value.Type() != "string" {
			return
		}
		if f.Annotations[requiredAnnotation] == nil && !strings.HasSuffix(f.Name, "-id") {
			return
		}
		if hasVisibleRune(f.Value.String()) {
			return
		}
		msg := fmt.Sprintf("flag --%s requires a non-empty value", f.Name)
		if h := f.Annotations[blankHintAnnotation]; len(h) > 0 {
			msg += "; " + h[0]
		}
		err = &usageError{errors.New(msg)}
	})
	return err
}

// globalEnvSetting is one env-backed global: its viper key, the env var, the
// persistent flag that overrides it, and the parser its env value must satisfy
// (nil for a string setting, which cannot be unparsable).
//
// The table drives both the viper binding and validateEnvFlags, so a new
// setting cannot be bound in one place and left unvalidated in another. viper
// (via cast) coerces an unparsable value to the zero value without an error, so
// C1I_DRY_RUN=yes silently read as false and sent the mutation it was meant to
// preview.
type globalEnvSetting struct {
	key, env, flag string
	parse          func(string) error
}

var globalEnvSettings = []globalEnvSetting{
	{key: "url", env: urlEnvVar, flag: "url"},
	{key: "fields", env: "C1I_FIELDS", flag: "fields"},
	{key: "max_retries", env: "C1I_MAX_RETRIES", flag: "max-retries", parse: parseIntValue},
	{key: "error_format", env: "C1I_ERROR_FORMAT", flag: "error-format"},
	{key: "debug", env: "C1I_DEBUG", flag: "debug", parse: parseBoolValue},
	{key: "dry_run", env: "C1I_DRY_RUN", flag: "dry-run", parse: parseBoolValue},
}

// bindGlobalEnvSettings wires every entry to its flag and env var.
func bindGlobalEnvSettings() {
	for _, s := range globalEnvSettings {
		_ = viper.BindPFlag(s.key, rootCmd.PersistentFlags().Lookup(s.flag))
		_ = viper.BindEnv(s.key, s.env)
	}
}

// validateEnvFlags rejects an unparsable value in a typed env-backed global
// before any request is sent. A flag set on the command line wins, so its
// presence suppresses the check (viper gives it the same precedence). Empty
// means unset. Negative C1I_MAX_RETRIES is accepted like `--max-retries -1`
// (transport clamps it).
func validateEnvFlags(cmd *cobra.Command) error {
	if isLocalOnlyRequest(cmd) {
		return nil
	}
	for _, s := range globalEnvSettings {
		if s.parse == nil {
			continue
		}
		if f := cmd.Flags().Lookup(s.flag); f != nil && f.Changed {
			continue
		}
		v, ok := os.LookupEnv(s.env)
		if !ok || v == "" {
			continue
		}
		if err := s.parse(v); err != nil {
			return &usageError{fmt.Errorf("invalid %s %q: %w", s.env, v, err)}
		}
	}
	return nil
}

// isLocalOnlyRequest reports whether cmd runs without touching the network:
// shell completion, either cobra's hidden __complete helper or the
// `completion` command tree. A bad global env value must not fail those.
func isLocalOnlyRequest(cmd *cobra.Command) bool {
	if isCompletionRequest(cmd) {
		return true
	}
	for p := cmd; p != nil; p = p.Parent() {
		if p.Name() == "completion" {
			return true
		}
	}
	return false
}

func parseBoolValue(s string) error {
	if _, err := strconv.ParseBool(s); err != nil {
		return errors.New("must be true or false")
	}
	return nil
}

// parseIntValue mirrors pflag's intValue.Set (base 0, so hexadecimal and octal
// are valid here exactly as they are on --max-retries) and its wording.
func parseIntValue(s string) error {
	if _, err := strconv.ParseInt(s, 0, 0); err != nil {
		return errors.New("must be an integer")
	}
	return nil
}

// validateDotSegmentIDFlags rejects a set *-id flag (or one occurrence of a
// repeatable one) whose value the server would normalize to the parent or
// collection: exactly "." or "..", with any ";params" suffix ignored, matching
// the transport path guard. That guard only sees a request path, so an id the
// command carries in a body -- --grant-policy-id, --owner-id, ... -- used to
// preview as if it were an id and only fail once a real send interpolated it
// into a URL; a multi-step create could apply its earlier steps first. Doing
// it here, before any pre-read or send, makes --dry-run and a real send agree.
// `c1i api` has no *-id flag and guards its own --path.
func validateDotSegmentIDFlags(cmd *cobra.Command) error {
	if isCompletionRequest(cmd) {
		return nil
	}
	var err error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if err != nil || !f.Changed || !strings.HasSuffix(f.Name, "-id") {
			return
		}
		switch f.Value.Type() {
		case "string":
			err = dotSegmentIDError(f.Name, f.Value.String())
		case "stringArray":
			// GetStringArray, not f.Value.String(), whose form is Go-syntax
			// "[a b]". A lone empty occurrence reads back as no elements; a
			// blank value is refused by the command's own flag reader.
			values, getErr := cmd.Flags().GetStringArray(f.Name)
			if getErr != nil {
				return
			}
			for _, v := range values {
				if err = dotSegmentIDError(f.Name, v); err != nil {
					return
				}
			}
		}
	})
	return err
}

// bodyIDArgAnnotation marks a command whose positional id at the given
// 0-based indexes is sent only in a request body, never in a path. The
// transport guard inspects paths, so validateDotSegmentIDArgs has to apply the
// same "."/".." rule to those arguments itself.
const bodyIDArgAnnotation = "c1i/body-id-arg"

// markBodyIDArgs records that the given 0-based positionals are body-carried
// ids (see bodyIDArgAnnotation).
func markBodyIDArgs(c *cobra.Command, indexes ...int) {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	vals := make([]string, len(indexes))
	for i, idx := range indexes {
		vals[i] = strconv.Itoa(idx)
	}
	c.Annotations[bodyIDArgAnnotation] = strings.Join(vals, ",")
}

// validateDotSegmentIDArgs applies validateDotSegmentIDFlags' rule to the
// positionals a command annotated with markBodyIDArgs. An argument past the
// end is left alone: args validation belongs to cobra and the command.
func validateDotSegmentIDArgs(cmd *cobra.Command, args []string) error {
	if isCompletionRequest(cmd) {
		return nil
	}
	v, ok := cmd.Annotations[bodyIDArgAnnotation]
	if !ok {
		return nil
	}
	for _, part := range strings.Split(v, ",") {
		idx, convErr := strconv.Atoi(part)
		if convErr != nil || idx < 0 || idx >= len(args) {
			continue
		}
		if err := dotSegmentIDValueError("argument", args[idx]); err != nil {
			return err
		}
	}
	return nil
}

// dotSegmentIDError returns the usage error for a *-id flag value the server
// would resolve to something other than the named object, or nil.
func dotSegmentIDError(flagName, value string) error {
	return dotSegmentIDValueError("flag --"+flagName, value)
}

// dotSegmentIDValueError is the one wording for a body-carried id (a flag or a
// positional) that the server would normalize away. A padded value counts
// too: policyIDFlag trims before use, so " . " reaches a path as ".".
func dotSegmentIDValueError(subject, value string) error {
	if !transport.IsDotSegmentID(value) && !transport.IsDotSegmentID(strings.TrimSpace(value)) {
		return nil
	}
	return &usageError{fmt.Errorf(`%s %q is not an id: "." and ".." (with an optional ";params" suffix) address the parent or collection, not the object`, subject, value)}
}

func validateErrorFormat(f string) error {
	if f == "" {
		return nil
	}
	_, err := errorFormatFlag.parse(f)
	return err
}

var errorFormatFlag = enumFlag{flag: "error-format", unknown: enumReject, values: []enumValue{
	{name: "text", api: "text"},
	{name: "json", api: "json"},
}}

func init() {
	cobra.OnInitialize(initConfig)
	// Tag flag-parse failures so Run() can map them to the usage exit code.
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err}
	})
	pf := rootCmd.PersistentFlags()
	pf.String("url", "", "C1 tenant URL; https required. Precedence: --url, C1I_URL, then ~/.c1i.yaml")
	pf.String("fields", "", "Comma-separated JSON fields to keep in read output (dot-paths); prefer over jq for simple projection")
	pf.Int("max-retries", client.DefaultMaxRetries, "Retries for transient API failures (429/5xx); 0 disables")
	pf.String("error-format", "text", "Error output format: "+errorFormatFlag.usage())
	pf.Bool("debug", false, "Trace HTTP requests (method, URL, status, timing) to stderr")
	pf.Bool("dry-run", false, "Preview C1 REST mutations (method, path, body) without sending; mcp gateway call and auth login reject it")
	bindGlobalEnvSettings()
}

func initConfig() { loadConfigFile(viper.GetViper()) }

// loadConfigFile reads ~/.c1i.yaml into v, or nothing without a usable home.
func loadConfigFile(v *viper.Viper) {
	home, err := config.HomeDir()
	if err != nil {
		return
	}
	v.AddConfigPath(home)
	v.SetConfigName(".c1i")
	v.SetConfigType("yaml")
	_ = v.ReadInConfig()
}

// GetBaseURL returns the configured base URL or exits with an error. Embedded
// credentials are dropped with a warning to stderr rather than an error; a
// non-https scheme is an error. Delegates to requireBaseURL so a ParseURL
// error (e.g. a retired bare short name) is reported with the same
// source-naming used everywhere else.
func GetBaseURL() (string, error) {
	baseURL, _, err := requireBaseURL()
	return baseURL, err
}

// requireBaseURL is GetBaseURL plus where the URL came from, for commands that
// report the tenant they resolved instead of only using it.
func requireBaseURL() (string, URLSource, error) {
	baseURL, source, err := GetBaseURLWithSource()
	if err != nil {
		return "", source, err
	}
	if source == URLSourceNone {
		return "", source, &usageError{fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~%s.c1i.yaml", string(filepath.Separator))}
	}
	return baseURL, source, nil
}

// warnAboutURL prints any ParseURL warnings to stderr, one per line.
func warnAboutURL(warnings []string) {
	for _, w := range warnings {
		noticeTo(os.Stderr, noticeURLWarning, "Warning: "+w, nil)
	}
}

// warnAboutURLSource flags an ambiguous tenant resolution to stderr: only
// ~/.c1i.yaml. It is not mentioned anywhere on the command line, unlike
// --url or C1I_URL — the incident this exists for was an agent losing an
// exported C1I_URL between shell calls and falling through to the config
// file's tenant; the harmful step was the config-file read, not the earlier
// C1I_URL calls, which were correct. Warning on C1I_URL too would fire on
// every normal invocation of a legitimate workflow, which trains people to
// stop reading it — weakening the one warning that actually matters.
func warnAboutURLSource(baseURL string, source URLSource) {
	if source != URLSourceConfig {
		return
	}
	noticeTo(os.Stderr, noticeURLDefault, fmt.Sprintf(urlDefaultWarningFmt, baseURL, urlSourceLabel(source)),
		map[string]any{"url": baseURL, "source": urlSourceToken(source)})
}

// urlEnvVar and urlDefaultWarningFmt are quoted by cmd/agents.md; a test pins them.
const (
	urlEnvVar            = "C1I_URL"
	urlDefaultWarningFmt = "Warning: no --url flag given; targeting %s (from %s)"
)

// URLSource indicates where the URL was resolved from.
type URLSource int

const (
	URLSourceNone URLSource = iota
	URLSourceFlag
	URLSourceEnv
	URLSourceConfig
)

// urlSourceLabel names where a URL value came from, for wrapping a ParseURL
// error: a stale bare name sitting in ~/.c1i.yaml is the genuinely confusing
// case, much less so once the message names it.
func urlSourceLabel(source URLSource) string {
	switch source {
	case URLSourceFlag:
		return "--url flag"
	case URLSourceEnv:
		return "C1I_URL environment variable"
	case URLSourceConfig:
		return "~/.c1i.yaml"
	default:
		return "unknown source"
	}
}

// urlSourceToken is urlSourceLabel's machine-readable twin: stable identifiers
// for JSON output (auth whoami's tenantSource), not prose that may be reworded.
func urlSourceToken(source URLSource) string {
	switch source {
	case URLSourceFlag:
		return "flag"
	case URLSourceEnv:
		return "env"
	case URLSourceConfig:
		return "config"
	default:
		return "unknown"
	}
}

// GetBaseURLWithSource returns the configured base URL and where it came
// from, warning to stderr about anything ParseURL dropped. Looks up
// the "url" flag on rootCmd.PersistentFlags() directly (not a passed-in
// *cobra.Command's merged Flags()) so it gives the right answer regardless
// of which subcommand is actually executing -- GetBaseURL calls this from
// deep inside arbitrary subcommands' RunE.
func GetBaseURLWithSource() (string, URLSource, error) {
	parse := func(raw string, source URLSource) (string, URLSource, error) {
		url, warnings, err := config.ParseURL(raw)
		if err != nil {
			return "", source, &usageError{fmt.Errorf("%w (from %s)", err, urlSourceLabel(source))}
		}
		warnAboutURL(warnings)
		warnAboutURLSource(url, source)
		return url, source, nil
	}
	if f := rootCmd.PersistentFlags().Lookup("url"); f != nil && f.Changed {
		return parse(f.Value.String(), URLSourceFlag)
	}
	if v := os.Getenv(urlEnvVar); v != "" {
		return parse(v, URLSourceEnv)
	}
	if v := viper.GetString("url"); v != "" {
		return parse(v, URLSourceConfig)
	}
	return "", URLSourceNone, nil
}
