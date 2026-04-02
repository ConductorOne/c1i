package cmd

import "github.com/spf13/cobra"

var entitlementsCmd = &cobra.Command{
	Use:   "entitlements",
	Short: "Manage application entitlements",
}

func init() {
	rootCmd.AddCommand(entitlementsCmd)
}
