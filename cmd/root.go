package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ductone/c1i/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "c1i",
	Short: "ConductorOne CLI",
	Long: `c1i is a command-line interface for the ConductorOne API.

If you are an AI agent or unfamiliar with the ConductorOne API, start with the docs
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
	rootCmd.PersistentFlags().String("url", "", "ConductorOne URL (e.g. https://mycompany.conductor.one)")
	_ = viper.BindPFlag("url", rootCmd.PersistentFlags().Lookup("url"))
	_ = viper.BindEnv("url", "C1I_URL")
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

func Execute() error {
	return rootCmd.Execute()
}
