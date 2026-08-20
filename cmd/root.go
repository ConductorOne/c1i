package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "c1i",
	Short: "C1 (formerly ConductorOne) CLI",
	Long: `c1i is a command-line interface for the C1 (formerly ConductorOne) API.

If you are an AI agent, run "c1i docs agents" first — it covers conventions
this help text can't (output contracts, exit codes, when to prefer a
first-class command over raw API calls). It requires NO authentication.

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
	// --connector-id, ...) are covered without a per-command check.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := validateErrorFormat(viper.GetString("error_format")); err != nil {
			return err
		}
		if err := validateArgsUTF8(args); err != nil {
			return err
		}
		if err := validateFlagsUTF8(cmd); err != nil {
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

func validateErrorFormat(f string) error {
	switch strings.ToLower(f) {
	case "", "text", "json":
		return nil
	default:
		return &usageError{fmt.Errorf("invalid --error-format %q: must be \"text\" or \"json\"", f)}
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	// Tag flag-parse failures so Run() can map them to the usage exit code.
	rootCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err}
	})
	rootCmd.PersistentFlags().String("url", "", "C1 URL; https required (e.g. https://mycompany.conductor.one or https://mycompany.c1eu.ai)")
	_ = viper.BindPFlag("url", rootCmd.PersistentFlags().Lookup("url"))
	_ = viper.BindEnv("url", "C1I_URL")

	rootCmd.PersistentFlags().String("fields", "", "Comma-separated fields to keep in JSON output (dot-paths for nested, e.g. id,user.email)")
	_ = viper.BindPFlag("fields", rootCmd.PersistentFlags().Lookup("fields"))
	_ = viper.BindEnv("fields", "C1I_FIELDS")

	rootCmd.PersistentFlags().Int("max-retries", client.DefaultMaxRetries, "Retries for transient API failures (429/5xx); 0 disables")
	_ = viper.BindPFlag("max_retries", rootCmd.PersistentFlags().Lookup("max-retries"))
	_ = viper.BindEnv("max_retries", "C1I_MAX_RETRIES")

	rootCmd.PersistentFlags().String("error-format", "text", "Error output format: text or json")
	_ = viper.BindPFlag("error_format", rootCmd.PersistentFlags().Lookup("error-format"))
	_ = viper.BindEnv("error_format", "C1I_ERROR_FORMAT")

	rootCmd.PersistentFlags().Bool("debug", false, "Trace HTTP requests (method, URL, status, timing) to stderr")
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindEnv("debug", "C1I_DEBUG")

	rootCmd.PersistentFlags().Bool("dry-run", false, "Preview mutating requests (method, path, body) without sending them")
	_ = viper.BindPFlag("dry_run", rootCmd.PersistentFlags().Lookup("dry-run"))
	_ = viper.BindEnv("dry_run", "C1I_DRY_RUN")
}

func initConfig() {
	home, err := os.UserHomeDir()
	if err == nil {
		viper.AddConfigPath(home)
	}
	viper.SetConfigName(".c1i")
	viper.SetConfigType("yaml")
	_ = viper.ReadInConfig()
}

// GetBaseURL returns the configured base URL or exits with an error. Embedded
// credentials are dropped with a warning to stderr rather than an error; a
// non-https scheme is an error. Delegates to GetBaseURLWithSource so a ParseURL
// error (e.g. a retired bare short name) is reported with the same
// source-naming used everywhere else.
func GetBaseURL() (string, error) {
	baseURL, source, err := GetBaseURLWithSource()
	if err != nil {
		return "", err
	}
	if source == URLSourceNone {
		return "", fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~%s.c1i.yaml", string(filepath.Separator))
	}
	return baseURL, nil
}

// warnAboutURL prints any ParseURL warnings to stderr, one per line.
func warnAboutURL(warnings []string) {
	for _, w := range warnings {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
	}
}

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
		return url, source, nil
	}
	if f := rootCmd.PersistentFlags().Lookup("url"); f != nil && f.Changed {
		return parse(f.Value.String(), URLSourceFlag)
	}
	if v := os.Getenv("C1I_URL"); v != "" {
		return parse(v, URLSourceEnv)
	}
	if v := viper.GetString("url"); v != "" {
		return parse(v, URLSourceConfig)
	}
	return "", URLSourceNone, nil
}
