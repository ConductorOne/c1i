package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var connectorsUpdateCmd = &cobra.Command{
	Use:   "update <connector-id>",
	Short: "Update a connector from a JSON object (pretty JSON)",
	Long: `Update a connector: send a Connector object via --body-file plus an explicit
--update-mask (both required). The object is sent verbatim, with "id" set to
<connector-id>. Start from "connectors get" output without the keys it adds
beside the connector (expanded, appPath, capabilitiesPath, usersPath); the
server refuses those as unknown fields (400). This is how a connector made by
"connectors create" gets its config:

  c1i connectors get <connector-id> --app-id <app-id> \
    | jq 'del(.expanded, .appPath, .capabilitiesPath, .usersPath)' > conn.json
  # edit .config in conn.json
  c1i connectors update <connector-id> --app-id <app-id> --body-file conn.json --update-mask config

In the runs checked, the server required the object to carry "config" (else
400, "connector: config is required") and "catalogId" (else 500, "cannot
upgrade app connector version, catalog id is required") whatever the mask
named; it validated config against the catalog entry's schema; and a mask of
displayName or description returned 200 without changing either field.
Mask paths are camelCase; a snake_case path is a 400. A connector that backs
an MCP server is refused by the server (its config type is not one this
endpoint accepts); use "c1i mcp servers update" for those.

--justification is sent as the change's justification.reason.

Prints the updated connector, unwrapped like "connectors get" (id: .id).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mask, usingBody, err := bodyFileMask(cmd)
		if err != nil {
			return err
		}
		if !usingBody {
			return connectorUpdateSpec.nothingToUpdate()
		}
		path, _ := cmd.Flags().GetString("body-file")
		connector, err := readJSONFile[map[string]any](cmd, "body-file", path)
		if err != nil {
			return err
		}
		connector["id"] = args[0]
		body := wrapUpdate("connector", connector, mask)
		addConnectorJustification(cmd, body)

		appID, _ := cmd.Flags().GetString("app-id")
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: connectorPath(appID, args[0]), Body: body})
	},
}

// connectorUpdateSpec has no field flags: Update applied only what a full
// object carries, so the body file is the one way in.
var connectorUpdateSpec = &updateSpec{
	alt:   "--body-file with --update-mask",
	scope: []string{"app-id", "body-file", "update-mask", "justification"},
}

func init() {
	f := connectorsUpdateCmd.Flags()
	f.String("app-id", "", "Application ID")
	f.String("body-file", "", "Connector JSON object, sent verbatim (file, or \"-\" for stdin); requires --update-mask")
	f.String("update-mask", "", "Comma-separated camelCase field paths to apply (required with --body-file)")
	setBlankHint(connectorsUpdateCmd, "update-mask", updateMaskBlankHint)
	f.String("justification", "", connectorJustificationUsage)
	ownBodyKeys(connectorsUpdateCmd, "body-file", argBodyKeys(0, "<connector-id>", "id"))
	markRequired(connectorsUpdateCmd, "app-id")
	registerUpdateSpec(connectorsUpdateCmd, connectorUpdateSpec)
	markMutating(connectorsUpdateCmd)
	connectorsCmd.AddCommand(connectorsUpdateCmd)
}
