package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the c1i version",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), Version)
		return wrapOutputErr(err)
	},
}

func init() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}
