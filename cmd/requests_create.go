package cmd

import "github.com/spf13/cobra"

var requestsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new access request",
}

func init() {
	requestsCmd.AddCommand(requestsCreateCmd)
}
