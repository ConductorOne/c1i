package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var hooksDeleteCmd = &cobra.Command{
	Use:   "delete <hook-id>",
	Short: "Soft-delete a hook",
	Long: `Delete a hook by ID. It leaves "hooks list", but "hooks get" still returns it
at exit 0 (the Hook has no deletedAt, so nothing marks it deleted), and
deleting it again exits 0 again. The API says a hook referenced by a guardrail
rule cannot be deleted until the reference is removed (not observed). Prints
the API's response, which is {}; exit 0 is the success signal. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: hookPath(args[0])})
	},
}

func init() {
	markMutating(hooksDeleteCmd)
	hooksCmd.AddCommand(hooksDeleteCmd)
}
