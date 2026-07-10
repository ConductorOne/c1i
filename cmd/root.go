package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "c1i",
	Short: "C1 (formerly ConductorOne) CLI",
	Long: `c1i is a command-line interface for the C1 (formerly ConductorOne) API.

If you are an AI agent or unfamiliar with the C1 API, start with the docs
commands — they require NO authentication and let you explore every available endpoint:

  c1i docs search "access reviews"     Search documentation by keyword
  c1i docs endpoints --filter task      List API endpoints matching a pattern
  c1i docs endpoint /api/v1/tasks/{id}  Show full request/response schema
  c1i docs page product/admin/campaigns Fetch a documentation page

Use these to discover endpoints, understand request/response shapes, and find the
right API calls before making authenticated requests.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().String("url", "", "C1 URL (e.g. https://mycompany.conductor.one)")
	_ = viper.BindPFlag("url", rootCmd.PersistentFlags().Lookup("url"))
	_ = viper.BindEnv("url", "C1I_URL")

	rootCmd.PersistentFlags().String("fields", "", "Comma-separated fields to keep in JSON output (dot-paths for nested, e.g. id,user.email)")
	_ = viper.BindPFlag("fields", rootCmd.PersistentFlags().Lookup("fields"))
	_ = viper.BindEnv("fields", "C1I_FIELDS")

	rootCmd.PersistentFlags().Int("max-retries", client.DefaultMaxRetries, "Retries for transient API failures (429/5xx); 0 disables")
	_ = viper.BindPFlag("max_retries", rootCmd.PersistentFlags().Lookup("max-retries"))
	_ = viper.BindEnv("max_retries", "C1I_MAX_RETRIES")
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

func Execute() error {
	return rootCmd.Execute()
}
