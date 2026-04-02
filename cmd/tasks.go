package cmd

import "github.com/spf13/cobra"

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "Manage access request tasks",
}

func init() {
	rootCmd.AddCommand(tasksCmd)
}
