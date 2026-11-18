package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const xaaClientMappingsSearchPath = "/api/v1/search/xaa/client_audience_mappings"

// xaaClientMappingsPath is a resource server's mapping collection. A mapping's
// own id, its client key, may be a URL, so it travels in the query or body,
// never the path.
func xaaClientMappingsPath(rsID string) string {
	return client.Path("/api/v1/xaa/resource_servers/%s/client_audience_mappings", rsID)
}

func xaaResourceServerID(cmd *cobra.Command) string {
	return mustGetString(cmd, "resource-server-id")
}

func clientKeyQuery(key string) map[string]string {
	return map[string]string{"client_key": key}
}

var xaaClientMappingsCmd = &cobra.Command{
	Use:   "client-audience-mappings",
	Short: "Manage XAA client audience mappings",
	Long: `Manage a cross-app access resource server's client audience mappings. Each
maps a client, named by its client key, to its client id at that server,
which the API spec says is stamped into the client_id claim of grants C1
mints for it. --disabled rejects the client's exchanges for the server
without removing the mapping.

Every command takes --resource-server-id, not --app-id. A mapping's id is its
client key, the positional of get, update, delete and history. The server
accepted a CIMD client id URL and a dcr:// key, and refused other forms with
"client_key is not a recognized form (expected a CIMD url, dcr://, c1://, or
a /dcc, /mdc, or /sso cutename)".`,
}

// xaaClientMappingRow is the NDJSON row of "list" and "search".
func xaaClientMappingRow(item map[string]any) map[string]any {
	return map[string]any{
		"resource_server_id": optString(item, "xaaResourceServerId"),
		"client_key":         optString(item, "clientKey"),
		"audience_client_id": optString(item, "audienceClientId"),
		"disabled":           boolField(item, "disabled"),
		"created_at":         optString(item, "createdAt"),
		"updated_at":         optString(item, "updatedAt"),
	}
}

var xaaClientMappingRowKeys = []string{"resource_server_id", "client_key"}

var xaaClientMappingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List a resource server's XAA client audience mappings (NDJSON output)",
	Long: `List the client audience mappings of a cross-app access resource server
(NDJSON output), one row per mapping: resource_server_id, client_key,
audience_client_id, disabled, created_at, updated_at. A deleted mapping is not
listed.

A well-formed but unknown resource server returns zero rows and exit 0.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		path := xaaClientMappingsPath(xaaResourceServerID(cmd))
		return listMapPages(cmd, "GET "+path, "clientAudienceMappings", xaaClientMappingRowKeys, xaaClientMappingRow, getPage(cmd.Context(), path, nil))
	},
}

var xaaClientMappingsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search XAA client audience mappings across the tenant (NDJSON output)",
	Long: `Search client audience mappings across the tenant (NDJSON output; rows as in
"xaa client-audience-mappings list"). With no filter, every mapping is listed;
a deleted one is not.

--resource-server-id (repeatable) narrows to those servers; an unknown one
matches nothing. --query matched part of the client key and of the audience
client id in the run checked. --disabled=true lists only disabled mappings,
--disabled=false only enabled ones; omit it for both. A mapping created
seconds ago may not be found yet.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body := map[string]any{}
		addStringsToBody(cmd, body, [][2]string{{"query", "query"}})
		if err := addRepeatableToBody(cmd, body, [][2]string{{"resource-server-id", "xaaResourceServerIds"}}); err != nil {
			return err
		}
		if flagGiven(cmd, "disabled") {
			body["disabled"], _ = cmd.Flags().GetBool("disabled")
		}
		return listMapPages(cmd, "POST "+xaaClientMappingsSearchPath, "list", xaaClientMappingRowKeys, xaaClientMappingRow, postPage(cmd.Context(), xaaClientMappingsSearchPath, body))
	},
}

var xaaClientMappingsGetCmd = &cobra.Command{
	Use:   "get <client-key>",
	Short: "Show one XAA client audience mapping (pretty JSON)",
	Long: `Show the client audience mapping of <client-key> under --resource-server-id,
unwrapped. A deleted mapping is still returned, with deletedAt set. An unknown
key exits 4.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getNamedObject(cmd, xaaClientMappingsPath(xaaResourceServerID(cmd))+"/by_client_key", clientKeyQuery(args[0]), "clientAudienceMapping")
	},
}

var xaaClientMappingsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an XAA client audience mapping (pretty JSON)",
	Long: `Map a client to its client id at a cross-app access resource server.
--client-key names the client and is immutable; --audience-client-id is its
client id at the resource server. --disabled creates the mapping with the
client's exchanges rejected.

A client key already mapped under the server exits 2 ("a client mapping for
this client key already exists; edit it instead"), and so does one whose
mapping was deleted ("was deleted and can't be recreated").

The resource server is looked up first with a tenant-wide resource-server
search, because the server stores a mapping under an unknown resource server
and the mapping routes carry no app to look it up under. A server the search
does not return exits 4, nothing sent, --dry-run included. Search does not
return a deleted server, and a server created a moment earlier was missing
from it for about a second in the run checked; retry in that case.

Prints the created mapping, unwrapped like "xaa client-audience-mappings get".
Honors --dry-run.

Example:
  c1i xaa client-audience-mappings create --resource-server-id <resource-server-id> \
    --client-key https://client.example.com/metadata.json --audience-client-id orders-agent`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		rsID := xaaResourceServerID(cmd)
		body := map[string]any{"clientKey": mustGetString(cmd, "client-key")}
		if _, err := xaaClientMappingUpdateSpec.apply(cmd, body); err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: xaaClientMappingsPath(rsID), Body: body, Unwrap: named("clientAudienceMapping")},
			withPreRead(xaaResourceServerSearchCheck(rsID)))
	},
}

// xaaResourceServerSearchCheck refuses a --resource-server-id the tenant-wide
// search does not return (exit 4). Search omits deleted servers, so a deleted
// one exits 4 too, and lags a just-created one.
func xaaResourceServerSearchCheck(rsID string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		items, err := collectPages(c, pageSpec[map[string]any]{
			Route:    "POST " + xaaResourceServersSearchPath,
			PageSize: maxPageSize,
			Fetch:    postPage(ctx, xaaResourceServersSearchPath, map[string]any{}),
			Parse:    parseList[map[string]any],
			ID:       func(item map[string]any) string { return optStr(item, "id") },
		})
		if err != nil {
			return nil, fmt.Errorf("checking --resource-server-id %q: %w", rsID, err)
		}
		for _, item := range items {
			if optStr(item, "id") == rsID {
				return nil, nil
			}
		}
		return nil, &refNotFoundError{msg: fmt.Sprintf("--resource-server-id %q: resource server not found (or deleted); one created in the last few seconds may not be searchable yet", rsID)}
	}}
}

var xaaClientMappingUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("audience-client-id", "audienceClientId"),
		boolUpdate("disabled", "disabled"),
	},
	scope: []string{"resource-server-id"},
}

var xaaClientMappingsUpdateCmd = &cobra.Command{
	Use:   "update <client-key>",
	Short: "Update an XAA client audience mapping (pretty JSON)",
	Long: `Update a client audience mapping's audience client id or disabled flag.
Only the flags you pass change; the client key and resource server are
immutable.

The server validates audienceClientId on every update, whatever the mask
names, so without --audience-client-id c1i reads the mapping first and fills
it in; the mask still decides what changes. That read makes an unknown key
exit 4 before the update is sent, --dry-run included. An update of a deleted
mapping exits 0 and applies; deletedAt stays set.

Prints the updated mapping, unwrapped like "xaa client-audience-mappings get".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := xaaClientMappingsPath(xaaResourceServerID(cmd))
		obj := map[string]any{}
		mask, err := xaaClientMappingUpdateSpec.buildUpdateMask(cmd, obj)
		if err != nil {
			return err
		}
		obj["clientKey"] = args[0]
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path + "/update", Body: wrapUpdate("clientAudienceMapping", obj, mask), Unwrap: named("clientAudienceMapping")},
			withPreRead(requiredFieldsFill(path+"/by_client_key", clientKeyQuery(args[0]), "clientAudienceMapping", "client audience mapping", []string{"audienceClientId"}, obj)))
	},
}

var xaaClientMappingsDeleteCmd = &cobra.Command{
	Use:   "delete <client-key>",
	Short: "Delete an XAA client audience mapping",
	Long: `Delete a client audience mapping. Honors --dry-run.

The delete is soft: "xaa client-audience-mappings get" still returns the
mapping with deletedAt set, and "list" and "search" stop listing it. The key
cannot be mapped again under the same server ("was deleted and can't be
recreated"); "update" still changes the deleted mapping. Repeating the delete
exits 0; an unknown key exits 4.

Prints the API's response, which is {}; exit 0 is the success signal.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: xaaClientMappingsPath(xaaResourceServerID(cmd)) + "/delete", Body: map[string]any{"clientKey": args[0]}})
	},
}

var xaaClientMappingsHistoryCmd = &cobra.Command{
	Use:   "history <client-key>",
	Short: "List an XAA client audience mapping's change history (NDJSON output)",
	Long:  xaaHistoryLong("client audience mapping", "xaa client-audience-mappings get"),
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		path := xaaClientMappingsPath(xaaResourceServerID(cmd)) + "/history"
		// Rows carry no id of their own, so only the token and idle-page checks apply.
		return listPages(cmd, c, pageSpec[json.RawMessage]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, clientKeyQuery(args[0])),
			Parse: parseList[json.RawMessage],
		})
	},
}

// addXAAResourceServerFlag registers the required parent --resource-server-id.
func addXAAResourceServerFlag(c *cobra.Command) {
	c.Flags().String("resource-server-id", "", "ID of the resource server the mapping belongs to")
	markRequired(c, "resource-server-id")
}

func init() {
	for _, c := range []*cobra.Command{xaaClientMappingsListCmd, xaaClientMappingsGetCmd, xaaClientMappingsCreateCmd,
		xaaClientMappingsUpdateCmd, xaaClientMappingsDeleteCmd, xaaClientMappingsHistoryCmd} {
		addXAAResourceServerFlag(c)
	}
	addPaginationFlags(xaaClientMappingsListCmd)
	addPaginationFlags(xaaClientMappingsHistoryCmd)

	s := xaaClientMappingsSearchCmd
	addRepeatableStringFlag(s, "resource-server-id", "Only mappings of this resource server (repeatable)")
	s.Flags().String("query", "", "Text to match (client key or audience client id)")
	s.Flags().Bool("disabled", false, "true: only disabled mappings; false: only enabled ones (omit for both)")
	addPaginationFlags(s)

	cr := xaaClientMappingsCreateCmd
	cr.Flags().String("client-key", "", "Client key: a CIMD client id URL, dcr://<client_id> or c1://<service_principal_id>; immutable")
	cr.Flags().String("audience-client-id", "", "The client's client id at the resource server")
	cr.Flags().Bool("disabled", false, "Create the mapping with the client's exchanges rejected")
	markRequired(cr, "client-key", "audience-client-id")
	markMutating(cr)

	up := xaaClientMappingsUpdateCmd
	up.Flags().String("audience-client-id", "", "New client id at the resource server")
	up.Flags().Bool("disabled", false, "Reject the client's exchanges for this server without removing the mapping")
	registerUpdateSpec(up, xaaClientMappingUpdateSpec)
	markMutating(up)

	markMutating(xaaClientMappingsDeleteCmd)

	xaaClientMappingsCmd.AddCommand(xaaClientMappingsListCmd, xaaClientMappingsSearchCmd, xaaClientMappingsGetCmd,
		xaaClientMappingsCreateCmd, xaaClientMappingsUpdateCmd, xaaClientMappingsDeleteCmd, xaaClientMappingsHistoryCmd)
	xaaCmd.AddCommand(xaaClientMappingsCmd)
}
