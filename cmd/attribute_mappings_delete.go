package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var attributeMappingsDeleteCmd = &cobra.Command{
	Use:   "delete <mapping-id>",
	Short: "Delete a user-attribute mapping",
	Long: `Delete a mapping by id; "attribute-mappings get" then exits 4, and an unknown
id exits 4, but deleting the same id again exits 0. Prints the API's response,
which is {}; exit 0 is the success signal. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: attributeMappingPath(args[0])})
	},
}

func init() {
	markMutating(attributeMappingsDeleteCmd)
	attributeMappingsCmd.AddCommand(attributeMappingsDeleteCmd)
}
