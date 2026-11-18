package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var functionsDeleteCmd = &cobra.Command{
	Use:   "delete <function-id>",
	Short: "Soft-delete a function",
	Long: `Soft-delete a function by ID. "functions list" stops listing it, while
"functions get" still returns it, with deletedAt set. Deleting it again exits
4 (404). The API refuses a function still referenced by a hook or workflow
template until that reference is removed or retargeted.

Prints the API's response, which is {}; exit 0 is the success signal.
Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: client.Path("/api/v1/functions/%s", args[0])})
	},
}

func init() {
	markMutating(functionsDeleteCmd)
	functionsCmd.AddCommand(functionsDeleteCmd)
}
