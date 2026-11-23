package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var decoysRotateCmd = &cobra.Command{
	Use:   "rotate <decoy-id>",
	Short: "Vend fresh secret material for a decoy (the secret is shown once)",
	Long: `Issue fresh secret material for an existing decoy, without changing its id,
kind, display name, description, or annotations.

Prints the decoy as pretty JSON, unwrapped like "decoys get": the id is .id,
beside .material shaped like "decoys create" — the new secret cannot be read
again after this response; run "decoys rotate" again for another. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: decoyRotatePath(args[0])},
			secretBearing(secretDecoy))
	},
}

func init() {
	markMutating(decoysRotateCmd)
	decoysCmd.AddCommand(decoysRotateCmd)
}
