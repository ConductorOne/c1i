package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

func xaaScopeBindingsPath(appID, profileID string) string {
	return client.Path("/api/v1/apps/%s/xaa/access_profiles/%s/scope_bindings", appID, profileID)
}

// xaaScopeBindingRow is the NDJSON row of "xaa access-profiles scopes".
func xaaScopeBindingRow(item map[string]any) map[string]any {
	return map[string]any{
		"access_profile_id":  optString(item, "accessProfileId"),
		"scope_id":           optString(item, "xaaScopeId"),
		"resource_server_id": optString(item, "xaaResourceServerId"),
		"app_id":             optString(item, "appId"),
		"created_at":         optString(item, "createdAt"),
		"updated_at":         optString(item, "updatedAt"),
	}
}

var xaaAccessProfilesScopesCmd = &cobra.Command{
	Use:   "scopes <access-profile-id>",
	Short: "List the scopes bound to an XAA access profile (NDJSON output)",
	Long: `List the scopes bound to a cross-app access profile (NDJSON output), one
row per binding: access_profile_id, scope_id, resource_server_id, app_id,
created_at, updated_at. Change them with "add-scopes" and "remove-scopes".

A well-formed but unknown profile returns zero rows and exit 0.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := xaaScopeBindingsPath(xaaAppID(cmd), args[0])
		return listMapPages(cmd, "GET "+path, "bindings", []string{"scope_id"}, xaaScopeBindingRow, getPage(cmd.Context(), path, nil))
	},
}

var xaaAccessProfilesAddScopesCmd = &cobra.Command{
	Use:   "add-scopes <access-profile-id>",
	Short: "Bind scopes to an XAA access profile (pretty JSON)",
	Long: `Bind each --scope-id to a cross-app access profile. Every scope must belong
to the profile's resource server; the server refuses one that does not
("belongs to a different resource server than access profile", exit 2). A
repeated --scope-id is sent once, and binding an already bound scope exits 0.
The server takes at most 100 ids per request.

Each scope is looked up first under --app-id: an unknown one exits 4 and a
deleted one 2, nothing sent (the server answers an unknown scope with 400).
--dry-run does the lookups too. An unknown profile exits 4.

Prints the API's response, {"bindings": [...]}, one object per bound scope.
Honors --dry-run.

Example:
  c1i xaa access-profiles add-scopes <access-profile-id> --app-id <app-id> \
    --scope-id <scope-id> --scope-id <scope-id>`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendScopeBindings(cmd, xaaScopeBindingsPath(xaaAppID(cmd), args[0]), true)
	},
}

var xaaAccessProfilesRemoveScopesCmd = &cobra.Command{
	Use:   "remove-scopes <access-profile-id>",
	Short: "Unbind scopes from an XAA access profile",
	Long: `Unbind each --scope-id from a cross-app access profile. A repeated
--scope-id is sent once, and removing a scope that is not bound exits 0.

Each scope is looked up first under --app-id: an unknown one exits 4, nothing
sent (the server answers it with a silent success). A deleted scope is
accepted. --dry-run does the lookups too. An unknown profile exits 4.

Prints the API's response, which is {}; exit 0 is the success signal. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendScopeBindings(cmd, xaaScopeBindingsPath(xaaAppID(cmd), args[0])+"/delete", false)
	},
}

// sendScopeBindings posts the --scope-id set to path after looking up each
// scope; add refuses a deleted scope, which remove accepts.
func sendScopeBindings(cmd *cobra.Command, path string, add bool) error {
	ids, err := repeatableStringFlag(cmd, "scope-id")
	if err != nil {
		return err
	}
	ids = dedupe(ids)
	appID := xaaAppID(cmd)
	opts := make([]mutationOpt, 0, len(ids))
	for _, id := range ids {
		opts = append(opts, withPreRead(xaaScopes.refCheck(appID, "scope-id", id, add)))
	}
	return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: map[string]any{"xaaScopeIds": ids}}, opts...)
}

func init() {
	addXAAAppFlag(xaaAccessProfilesScopesCmd)
	addPaginationFlags(xaaAccessProfilesScopesCmd)
	for _, c := range []*cobra.Command{xaaAccessProfilesAddScopesCmd, xaaAccessProfilesRemoveScopesCmd} {
		addXAAAppFlag(c)
		addRepeatableStringFlag(c, "scope-id", "ID of a scope of the profile's resource server (repeatable)")
		markRequired(c, "scope-id")
		markMutating(c)
	}
	xaaAccessProfilesCmd.AddCommand(xaaAccessProfilesScopesCmd, xaaAccessProfilesAddScopesCmd, xaaAccessProfilesRemoveScopesCmd)
}
