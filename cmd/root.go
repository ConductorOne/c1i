package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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
	rootCmd.PersistentFlags().String("tenant", "", "ConductorOne tenant name (e.g. mycompany)")
	_ = viper.BindPFlag("tenant", rootCmd.PersistentFlags().Lookup("tenant"))
	_ = viper.BindEnv("tenant", "C1I_TENANT")
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

// GetTenant returns the configured tenant or exits with an error.
func GetTenant() (string, error) {
	tenant := viper.GetString("tenant")
	if tenant == "" {
		return "", fmt.Errorf("tenant is required: set --tenant flag, C1I_TENANT env var, or tenant in ~%s.c1i.yaml", string(filepath.Separator))
	}
	return tenant, nil
}

func Execute() error {
	return rootCmd.Execute()
}
