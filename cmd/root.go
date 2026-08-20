package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/spf13/cobra"
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
	// Validate global flag values once, before any command runs, and attach a
	// fresh *fieldsMatchState (cmd/fields.go) to the command's context so
	// every emitter created during this invocation shares one tracker.
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		if err := validateErrorFormat(viper.GetString("error_format")); err != nil {
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
	rootCmd.PersistentFlags().String("url", "", "C1 URL (e.g. https://mycompany.conductor.one)")
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

// GetBaseURL returns the configured base URL or exits with an error.
func GetBaseURL() (string, error) {
	raw := viper.GetString("url")
	if raw == "" {
		return "", fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~%s.c1i.yaml", string(filepath.Separator))
	}
	return config.ParseURL(raw), nil
}

// URLSource indicates where the URL was resolved from.
type URLSource int

const (
	URLSourceNone URLSource = iota
	URLSourceFlag
	URLSourceEnv
	URLSourceConfig
)

// GetBaseURLWithSource returns the configured base URL and where it came from.
func GetBaseURLWithSource(cmd *cobra.Command) (string, URLSource) {
	if f := cmd.Flags().Lookup("url"); f != nil && f.Changed {
		return config.ParseURL(f.Value.String()), URLSourceFlag
	}
	if v := os.Getenv("C1I_URL"); v != "" {
		return config.ParseURL(v), URLSourceEnv
	}
	if v := viper.GetString("url"); v != "" {
		return config.ParseURL(v), URLSourceConfig
	}
	return "", URLSourceNone
}
