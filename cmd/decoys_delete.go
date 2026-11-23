package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var decoysDeleteCmd = &cobra.Command{
	Use:   "delete <decoy-id>",
	Short: "Delete a decoy",
	Long: `Delete a decoy by ID. Prints the API's response, which is {}; exit 0 is the
success signal. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: decoyPath(args[0])})
	},
}

func init() {
	markMutating(decoysDeleteCmd)
	decoysCmd.AddCommand(decoysDeleteCmd)
}
