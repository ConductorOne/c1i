package cmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var xaaScopes = xaaKind{
	noun:     "scope",
	label:    "scope",
	segment:  "scopes",
	envelope: "scope",
	listKey:  "scopes",
	row:      xaaScopeRow,
	required: []string{"displayName", "scopeValue", "xaaResourceServerId"},
}

var xaaScopesCmd = &cobra.Command{
	Use:   "scopes",
	Short: "Manage XAA scopes",
	Long: `Manage the OAuth scopes of an app's cross-app access resource servers. A
scope is mintable only in state enabled; a discovered or newly declared scope
starts in pending-review. Each scope has a backing entitlement
(appEntitlementId).`,
}

var xaaScopeStateFlag = enumFlag{flag: "state", prefix: "XAA_SCOPE_STATE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "pending-review", api: "XAA_SCOPE_STATE_PENDING_REVIEW"},
	{name: "enabled", api: "XAA_SCOPE_STATE_ENABLED"},
	{name: "disabled", api: "XAA_SCOPE_STATE_DISABLED"},
	{name: "removed", api: "XAA_SCOPE_STATE_REMOVED"},
}}

var xaaScopeClassificationFlag = enumFlag{flag: "classification", prefix: "XAA_SCOPE_CLASSIFICATION_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "read", api: "XAA_SCOPE_CLASSIFICATION_READ"},
	{name: "write", api: "XAA_SCOPE_CLASSIFICATION_WRITE"},
	{name: "destructive", api: "XAA_SCOPE_CLASSIFICATION_DESTRUCTIVE"},
	{name: "sensitive", api: "XAA_SCOPE_CLASSIFICATION_SENSITIVE"},
	{name: "dangerous", api: "XAA_SCOPE_CLASSIFICATION_DANGEROUS"},
}}

// xaaScopeStateUsage omits removed, which refuseRemovedScopeState rejects.
var xaaScopeStateUsage = xaaScopeStateFlag.without("removed").usage()

// refuseRemovedScopeState rejects --state removed: create refuses it ("state
// must be PENDING_REVIEW, ENABLED or DISABLED") but update accepts it.
func refuseRemovedScopeState(state string) error {
	if state == "XAA_SCOPE_STATE_REMOVED" {
		return &usageError{fmt.Errorf("--state removed is reserved by the server (create answers \"state must be PENDING_REVIEW, ENABLED or DISABLED\"); use --state disabled to block a scope, or delete it")}
	}
	return nil
}

// xaaScopeRow is the NDJSON row of "xaa scopes list".
func xaaScopeRow(item map[string]any) map[string]any {
	row := xaaRowBase(item)
	row["resource_server_id"] = optString(item, "xaaResourceServerId")
	row["scope_value"] = optString(item, "scopeValue")
	row["state"] = optString(item, "state")
	row["classification"] = optString(item, "classification")
	row["source"] = optString(item, "source")
	row["app_entitlement_id"] = optString(item, "appEntitlementId")
	row["last_discovered_at"] = optString(item, "lastDiscoveredAt")
	return row
}

var xaaScopesListCmd = xaaScopes.listCmd("List an app's XAA scopes (NDJSON output)",
	`List the cross-app access scopes of an app, across its resource servers
(NDJSON output), one row per scope: id, app_id, display_name, description,
resource_server_id, scope_value, state, classification, source,
app_entitlement_id, last_discovered_at, created_at, updated_at. A deleted
scope is not listed.

A well-formed but unknown app returns zero rows and exit 0.`)

var xaaScopesGetCmd = xaaScopes.getCmd("Show one XAA scope (pretty JSON)",
	`Show one cross-app access scope, unwrapped. A deleted scope is still
returned, with deletedAt set. An unknown id exits 4.`)

var xaaScopesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Declare a scope on an XAA resource server (pretty JSON)",
	Long: `Declare an OAuth scope under a cross-app access resource server. It starts
in state pending-review unless --state says otherwise.

--scope-value is the literal OAuth scope string minted into grants; it is
immutable and unique per resource server (409, exit 2: "already exists for
this resource server"). The server provisions a backing entitlement
(appEntitlementId).

The resource server is looked up first under --app-id: if it does not exist
the command exits 4, and if it is deleted, 2 (the server would accept both).
--dry-run does the lookup too.

Prints the created scope, unwrapped like "xaa scopes get": the id is .id.
Honors --dry-run.

Example:
  c1i xaa scopes create --app-id <app-id> --resource-server-id <resource-server-id> \
    --scope-value orders.read --display-name "Read orders" --classification read`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		appID := xaaAppID(cmd)
		rsID := mustGetString(cmd, "resource-server-id")
		body := map[string]any{
			"xaaResourceServerId": rsID,
			"scopeValue":          mustGetString(cmd, "scope-value"),
			"displayName":         mustGetString(cmd, "display-name"),
		}
		if _, err := xaaScopeUpdateSpec.apply(cmd, body); err != nil {
			return err
		}
		if err := refuseRemovedScopeState(fmt.Sprint(body["state"])); err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: xaaScopes.collectionPath(appID), Body: body, Unwrap: named("scope")},
			withPreRead(xaaResourceServerCheck(appID, rsID)))
	},
}

var xaaScopeUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		enumUpdate("state", "state", &xaaScopeStateFlag),
		enumUpdate("classification", "classification", &xaaScopeClassificationFlag),
	},
	scope: []string{"app-id"},
}

var xaaScopesUpdateCmd = &cobra.Command{
	Use:   "update <scope-id>",
	Short: "Update or approve an XAA scope (pretty JSON)",
	Long: `Update a cross-app access scope. Only the flags you pass change. This is how
a scope is approved: --state enabled makes it mintable, --state disabled
blocks it. The scope value and resource server are immutable.

The server validates displayName, scopeValue and xaaResourceServerId on every
update, whatever the mask names, so c1i reads the scope first and fills in
the ones the request leaves unset; the mask still decides what changes. That
read makes an unknown id exit 4 before the update is sent, --dry-run included.

Prints the updated scope, unwrapped like "xaa scopes get".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID := xaaAppID(cmd)
		body, obj, err := buildXAAUpdate(cmd, xaaScopes, xaaScopeUpdateSpec)
		if err != nil {
			return err
		}
		if err := refuseRemovedScopeState(fmt.Sprint(obj["state"])); err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: xaaScopes.itemPath(appID, args[0]), Body: body, Unwrap: named("scope")},
			withPreRead(xaaScopes.xaaRequiredFill(appID, args[0], obj)))
	},
}

var xaaScopesDeleteCmd = xaaScopes.deleteCmd("Delete an XAA scope",
	`Delete a cross-app access scope. Honors --dry-run.

The delete is soft: "xaa scopes get" still returns the scope with deletedAt
set, and "list" stops listing it. Repeating the delete exits 0; an unknown id
exits 4. Its backing entitlement was deleted within seconds in the run
checked. The API spec says it also cascades to access-profile bindings that
reference it.

Prints the API's response, which is {}; exit 0 is the success signal.`)

var xaaScopesHistoryCmd = xaaScopes.historyCmd("List an XAA scope's change history (NDJSON output)",
	xaaHistoryLong("scope", "xaa scopes get"))

func init() {
	cr := xaaScopesCreateCmd
	addXAAAppFlag(cr)
	cr.Flags().String("resource-server-id", "", "ID of the resource server the scope belongs to")
	cr.Flags().String("scope-value", "", "Literal OAuth scope string, immutable")
	cr.Flags().String("display-name", "", "Display name")
	addXAAScopeFieldFlags(cr, "Description")
	markRequired(cr, "resource-server-id", "scope-value", "display-name")
	markMutating(cr)

	up := xaaScopesUpdateCmd
	addXAAAppFlag(up)
	up.Flags().String("display-name", "", "New display name")
	addXAAScopeFieldFlags(up, xaaDescriptionUpdateUsage)
	registerUpdateSpec(up, xaaScopeUpdateSpec)
	allowEmpty(up, emptyIsSent, "description")
	markMutating(up)

	xaaScopesCmd.AddCommand(xaaScopesListCmd, xaaScopesGetCmd, cr, up, xaaScopesDeleteCmd, xaaScopesHistoryCmd)
	xaaCmd.AddCommand(xaaScopesCmd)
}

// addXAAScopeFieldFlags registers the optional fields create and update share.
func addXAAScopeFieldFlags(c *cobra.Command, descUsage string) {
	f := c.Flags()
	f.String("description", "", descUsage)
	f.String("classification", "", "Risk classification: "+xaaScopeClassificationFlag.usage())
	f.String("state", "", "Approval state: "+xaaScopeStateUsage)
}
