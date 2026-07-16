package cmd

import "github.com/spf13/cobra"

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export bulk data from C1",
}

func init() {
	rootCmd.AddCommand(exportCmd)
}
