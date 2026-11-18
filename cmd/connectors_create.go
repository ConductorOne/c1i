package cmd

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var connectorsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a connector with an empty config (pretty JSON)",
	Long: `Create a connector on an app from a catalog entry. The connector is created
with an empty config (the API's "delegated" create); set the config afterwards
with "c1i connectors update <connector-id> --body-file <file> --update-mask
config".

--catalog-id is required. A malformed id is a 400 regex error (exit 2); a
well-formed id the catalog lacks can answer 500 "invalid connector catalog
id" (exit 6). An MCP catalog id is refused with a 400, "the MCP catalog is not
installable; register an MCP server through the MCP server API" (exit 2); use
"c1i mcp servers register". Omit --user-id and, in the run
checked, the caller became the connector's only user id.

  --body-file   the full CreateDelegated request body, sent verbatim, for
                fields with no flag (appEntitlementOwnerRefs,
                appManagedStateBindingRef); mutually exclusive with the
                other flags except --app-id

Prints the created connector as pretty JSON, unwrapped like "connectors get":
the id is .id.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, userFlag, userIDs, err := buildConnectorCreateBody(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		path := client.Path("/api/v1/apps/%s/connectors", appID)
		var opts []mutationOpt
		if len(userIDs) > 0 {
			opts = append(opts, withPreRead(existingUsersCheck(userFlag, userIDs)))
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body}, opts...)
	},
}

var connectorCreateFieldFlags = []string{"catalog-id", "display-name", "description", "user-id"}

// buildConnectorCreateBody returns the body plus the user ids to look up and
// the channel they came from, so a body file's userIds are checked as a
// --user-id flag's are.
func buildConnectorCreateBody(cmd *cobra.Command) (map[string]any, string, []string, error) {
	f := cmd.Flags()
	if flagGiven(cmd, "body-file") {
		for _, name := range connectorCreateFieldFlags {
			if flagGiven(cmd, name) {
				return nil, "", nil, &usageError{errors.New("--body-file is mutually exclusive with --catalog-id/--display-name/--description/--user-id")}
			}
		}
		path, _ := f.GetString("body-file")
		body, err := readJSONFile[map[string]any](cmd, "body-file", path)
		if err != nil {
			return nil, "", nil, err
		}
		ids, key, err := bodyUserIDs(body)
		return body, "body-file " + key, ids, err
	}
	if !flagGiven(cmd, "catalog-id") {
		return nil, "", nil, &usageError{errors.New("--catalog-id is required (or pass --body-file)")}
	}
	catalogID, _ := f.GetString("catalog-id")
	body := map[string]any{"catalogId": catalogID}
	for flag, key := range map[string]string{"display-name": "displayName", "description": "description"} {
		if flagGiven(cmd, flag) {
			v, _ := f.GetString(flag)
			body[key] = v
		}
	}
	ids, err := repeatableStringFlag(cmd, "user-id")
	if err != nil {
		return nil, "", nil, err
	}
	if len(ids) > 0 {
		body["userIds"] = ids
	}
	return body, "user-id", ids, nil
}

// bodyUserIDs reads a body file's userIds, which must be an array of strings,
// and the key they were under.
func bodyUserIDs(body map[string]any) ([]string, string, error) {
	raw, key, err := bodyField(body, "userIds")
	if err != nil || key == "" {
		return nil, "userIds", err
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, key, &usageError{fmt.Errorf("--body-file %s must be an array of user id strings", key)}
	}
	ids := make([]string, 0, len(arr))
	for _, v := range arr {
		s, ok := v.(string)
		if !ok || !hasVisibleRune(s) {
			return nil, key, &usageError{fmt.Errorf("--body-file %s must be an array of non-blank user id strings", key)}
		}
		ids = append(ids, s)
	}
	return ids, key, nil
}

func init() {
	f := connectorsCreateCmd.Flags()
	f.String("app-id", "", "Application ID to create the connector on")
	f.String("catalog-id", "", "Catalog entry ID the connector is an instance of (required unless --body-file)")
	f.String("display-name", "", "Display name ("+displayNameRule+")")
	f.String("description", "", "Description")
	addRepeatableStringFlag(connectorsCreateCmd, "user-id", "C1 user ID to set on the connector (repeatable)")
	f.String("body-file", "", "Full CreateDelegated request JSON, verbatim (file, or \"-\" for stdin); mutually exclusive with the flags above except --app-id")
	markRequired(connectorsCreateCmd, "app-id")
	markMutating(connectorsCreateCmd)
	markUserRefChecked(connectorsCreateCmd, "user-id")
	connectorsCmd.AddCommand(connectorsCreateCmd)
}
