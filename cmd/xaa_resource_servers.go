package cmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var xaaResourceServers = xaaKind{
	noun:     "resource-server",
	label:    "resource server",
	segment:  "resource_servers",
	envelope: "resourceServer",
	listKey:  "resourceServers",
	row:      xaaResourceServerRow,
	required: []string{"displayName", "normalizedAudience"},
}

const xaaResourceServersSearchPath = "/api/v1/search/xaa/resource_servers"

var xaaResourceServersCmd = &cobra.Command{
	Use:   "resource-servers",
	Short: "Manage XAA resource servers",
	Long: `Manage an app's cross-app access resource servers: third-party
authorization servers registered as audiences C1 mints grants for. Each one
belongs to the app given by --app-id.`,
}

// xaaResourceServerRow is the NDJSON row of "list" and "search".
func xaaResourceServerRow(item map[string]any) map[string]any {
	row := xaaRowBase(item)
	var uris any
	if u := arrayField(item, "resourceUris"); len(u) > 0 {
		uris = u
	}
	row["normalized_audience"] = optString(item, "normalizedAudience")
	row["resource_uris"] = uris
	row["max_grant_lifetime"] = optString(item, "maxGrantLifetime")
	row["signing_algorithm"] = optString(item, "signingAlgorithm")
	row["require_proof_of_possession"] = boolField(item, "requireProofOfPossession")
	row["disabled"] = boolField(item, "disabled")
	row["modify_claims_hook"] = item["modifyClaimsHook"]
	row["sector_id"] = optString(item, "sectorId")
	return row
}

var xaaResourceServersListCmd = xaaResourceServers.listCmd("List an app's XAA resource servers (NDJSON output)",
	`List the cross-app access resource servers of an app (NDJSON output), one
row per server: id, app_id, display_name, description, normalized_audience,
resource_uris, max_grant_lifetime, signing_algorithm,
require_proof_of_possession, disabled, modify_claims_hook, sector_id,
created_at, updated_at. A deleted server is not listed.

A well-formed but unknown app returns zero rows and exit 0.`)

var xaaResourceServersSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search XAA resource servers across the tenant (NDJSON output)",
	Long: `Search cross-app access resource servers across the tenant (NDJSON output;
rows as in "xaa resource-servers list"). With no filter, every server is listed.

--app-id (repeatable) narrows to those apps. --query matched display names in
the run checked, not the audience. --signing-algorithm (repeatable) narrows to
those algorithms; "unspecified" is a server using the tenant default.
--disabled=true lists only disabled servers, --disabled=false only enabled
ones; omit it for both. A server created seconds ago may not be found yet.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body := map[string]any{}
		addStringsToBody(cmd, body, [][2]string{{"query", "query"}})
		if err := addRepeatableToBody(cmd, body, [][2]string{{"app-id", "appIds"}}); err != nil {
			return err
		}
		if flagGiven(cmd, "signing-algorithm") {
			algs, err := parseRepeatableEnum(cmd, xaaSearchSigningAlgFlag)
			if err != nil {
				return err
			}
			body["signingAlgorithmFilter"] = algs
		}
		if flagGiven(cmd, "disabled") {
			body["disabled"], _ = cmd.Flags().GetBool("disabled")
		}
		path := xaaResourceServersSearchPath
		return listMapPages(cmd, "POST "+path, "list", []string{"id"}, xaaResourceServerRow, postPage(cmd.Context(), path, body))
	},
}

// xaaSearchSigningAlgFlag adds "unspecified", which search matches against
// servers that use the tenant default.
var xaaSearchSigningAlgFlag = enumFlag{flag: "signing-algorithm", prefix: "XAA_SIGNING_ALGORITHM_", unknown: enumPassPrefixed,
	values: append([]enumValue{{name: "unspecified", api: "XAA_SIGNING_ALGORITHM_UNSPECIFIED"}}, xaaSigningAlgFlag.values...)}

// The shared tenant flag is named for its settings command; create and update
// expose the same values as --signing-algorithm.
var xaaResourceServerSigningAlgFlag = xaaSigningAlgFlag.as("signing-algorithm")

var xaaResourceServersGetCmd = xaaResourceServers.getCmd("Show one XAA resource server (pretty JSON)",
	`Show one cross-app access resource server, unwrapped: the server's fields at
the top level plus scopeDiscoveryStatus, the outcome of the latest scope
discovery run (state, startedAt, completedAt, lastError, findings,
importedScopeValues). A deleted server is still returned, with deletedAt set.
An unknown id exits 4.`)

var xaaResourceServerCreateFlags = []string{"audience", "display-name", "description", "resource-uri", "max-grant-lifetime", "signing-algorithm", "require-proof-of-possession", "disabled"}

var xaaResourceServersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register an XAA resource server on an app (pretty JSON)",
	Long: `Register a third-party authorization server as a cross-app access audience
of an app.

--audience is the server's issuer identifier. It must use https, and the
server stores it normalized (lowercase host, no trailing slash). It is
immutable, unique across the tenant (409, exit 2, "is already registered by
another resource server in this tenant"), and must not be the tenant's own
issuer (400: "audience must not equal the tenant's own issuer").

Registering starts a scope discovery run against the audience; its outcome is
scopeDiscoveryStatus in "xaa resource-servers get".

For modifyClaimsHook (a tenant Function the API spec says runs before every
grant is signed and can deny it), pass the whole request with --body-file
instead of the field flags; it is sent verbatim. Its functionId, and commitId
if given, are looked up first: an unknown one exits 4 (the server would store
it), --dry-run included.

The app is looked up first: an unknown --app-id exits 4 and a deleted one 2
(the server would store the server under either). --dry-run does the lookup
too.

Prints the created server, unwrapped like "xaa resource-servers get": the id
is .id. Honors --dry-run.

Example:
  c1i xaa resource-servers create --app-id <app-id> \
    --audience https://auth.example.com --display-name "Example API"`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := buildXAAResourceServerCreate(cmd)
		if err != nil {
			return err
		}
		hookCheck, err := claimsHookCheck(body)
		if err != nil {
			return err
		}
		appID := xaaAppID(cmd)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: xaaResourceServers.collectionPath(appID), Body: body, Unwrap: named("resourceServer")},
			liveAppPreRead(appID, "--app-id"), withPreRead(hookCheck))
	},
}

func buildXAAResourceServerCreate(cmd *cobra.Command) (map[string]any, error) {
	if flagGiven(cmd, "body-file") {
		for _, n := range xaaResourceServerCreateFlags {
			if flagGiven(cmd, n) {
				return nil, &usageError{fmt.Errorf("--body-file is mutually exclusive with --%s", n)}
			}
		}
		return readJSONFile[map[string]any](cmd, "body-file", mustGetString(cmd, "body-file"))
	}
	if err := requireNonEmpty(cmd, "audience", "display-name"); err != nil {
		return nil, fmt.Errorf("%w (or pass --body-file)", err)
	}
	body := map[string]any{
		"normalizedAudience": mustGetString(cmd, "audience"),
		"displayName":        mustGetString(cmd, "display-name"),
	}
	// The create fields are the update table's, so one set of builders serves both.
	if _, err := xaaResourceServerUpdateSpec.apply(cmd, body); err != nil {
		return nil, err
	}
	return body, nil
}

var xaaResourceServerUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		listUpdate("resource-uri", "resourceUris", nil),
		durationUpdate("max-grant-lifetime", "maxGrantLifetime"),
		enumUpdate("signing-algorithm", "signingAlgorithm", &xaaResourceServerSigningAlgFlag),
		boolUpdate("require-proof-of-possession", "requireProofOfPossession"),
		boolUpdate("disabled", "disabled"),
	},
	alt:   "--body-file with --update-mask",
	scope: []string{"app-id", "body-file", "update-mask"},
}

var xaaResourceServersUpdateCmd = &cobra.Command{
	Use:   "update <resource-server-id>",
	Short: "Update an XAA resource server (pretty JSON)",
	Long: `Update a cross-app access resource server. Only the flags you pass change;
--resource-uri replaces the whole list (repeat it). --disabled rejects token
exchanges for the server without removing it.

The audience is immutable: a mask naming it is refused by the server
("normalized_audience is immutable"). For modifyClaimsHook, or to clear a
field (resourceUris, maxGrantLifetime, signingAlgorithm back to the tenant
default), pass a resourceServer object via --body-file plus an explicit
--update-mask; a masked field the object leaves out is cleared. Name mask
paths in camelCase: the server refused a wildcard or snake_case path with 400
(exit 2), "FieldMask.paths contains invalid path". A modifyClaimsHook
functionId or commitId is looked up first; an unknown one exits 4.

The server validates displayName and normalizedAudience on every update,
whatever the mask names, so when the request leaves either unset c1i reads
the server first and fills it in; the mask still decides what changes. That
read makes an unknown id exit 4 before the update is sent, --dry-run included.
An update of a deleted server exits 0 and applies; deletedAt stays set.

Prints the updated server, unwrapped like "xaa resource-servers get".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID := xaaAppID(cmd)
		body, obj, err := buildXAAUpdate(cmd, xaaResourceServers, xaaResourceServerUpdateSpec)
		if err != nil {
			return err
		}
		hookCheck, err := claimsHookCheck(obj)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: xaaResourceServers.itemPath(appID, args[0]), Body: body, Unwrap: named("resourceServer")},
			withPreRead(xaaResourceServers.requiredFill(appID, args[0], obj)), withPreRead(hookCheck))
	},
}

var xaaResourceServersDeleteCmd = xaaResourceServers.deleteCmd("Delete an XAA resource server",
	`Delete a cross-app access resource server. Honors --dry-run.

The delete is soft: "xaa resource-servers get" still returns the server with
deletedAt set, and "list" and "search" stop listing it. Repeating the delete
exits 0; an unknown id exits 4. Its scopes and access profiles, and their
backing entitlements, were deleted within seconds in the run checked. The API
spec says it also cascades to client audience mappings.

Prints the API's response, which is {}; exit 0 is the success signal.`)

var xaaResourceServersHistoryCmd = xaaResourceServers.historyCmd("List an XAA resource server's change history (NDJSON output)",
	xaaHistoryLong("resource server", "xaa resource-servers get")+
		"\n\nThe snapshot omits scopeDiscoveryStatus, which is get-only.")

var xaaResourceServersResyncScopesCmd = &cobra.Command{
	Use:   "resync-scopes <resource-server-id>",
	Short: "Start a scope discovery run for an XAA resource server",
	Long: `Start another scope discovery run for a cross-app access resource server.
The API spec says the run imports newly advertised scopes in state
pending-review, and that a request while a run is in progress queues one more
run. Its outcome is scopeDiscoveryStatus in "xaa resource-servers get".

An unknown id exits 4. Prints the API's response, which is {}. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: xaaResourceServers.itemPath(xaaAppID(cmd), args[0]) + "/resync_scopes", Body: map[string]any{}})
	},
}

func init() {
	cr := xaaResourceServersCreateCmd
	addXAAAppFlag(cr)
	cr.Flags().String("audience", "", "Issuer identifier of the resource server, https (required unless --body-file)")
	cr.Flags().String("display-name", "", "Display name (required unless --body-file)")
	addXAAResourceServerFieldFlags(cr, "Description")
	cr.Flags().String("body-file", "", "Full create request JSON object, sent verbatim (file, or \"-\" for stdin); mutually exclusive with the field flags")
	ownBodyKeys(cr, "body-file", []ownedKey{{key: "appId", owner: "--app-id"}, {key: "app_id", owner: "--app-id"}})
	annotateRequired(cr, "audience", "display-name")
	markMutating(cr)

	up := xaaResourceServersUpdateCmd
	addXAAAppFlag(up)
	up.Flags().String("display-name", "", "New display name")
	addXAAResourceServerFieldFlags(up, xaaDescriptionUpdateUsage)
	up.Flags().String("body-file", "", "resourceServer JSON object (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the field flags")
	up.Flags().String("update-mask", "", bodyFileMaskUsage)
	setBlankHint(up, "update-mask", updateMaskBlankHint)
	ownBodyKeys(up, "body-file", []ownedKey{{key: "appId", owner: "--app-id"}, {key: "app_id", owner: "--app-id"}}, argBodyKeys(0, "<resource-server-id>", "id"))
	registerUpdateSpec(up, xaaResourceServerUpdateSpec)
	allowEmpty(up, emptyIsSent, "description")
	markMutating(up)

	s := xaaResourceServersSearchCmd
	addRepeatableStringFlag(s, "app-id", "Only servers of this app (repeatable)")
	s.Flags().String("query", "", "Text to match (display names, in the run checked)")
	addRepeatableStringFlag(s, "signing-algorithm", "Only servers with this signing algorithm (repeatable): "+xaaSearchSigningAlgFlag.usage())
	s.Flags().Bool("disabled", false, "true: only disabled servers; false: only enabled ones (omit for both)")
	addPaginationFlags(s)

	rs := xaaResourceServersResyncScopesCmd
	addXAAAppFlag(rs)
	markMutating(rs)

	xaaResourceServersCmd.AddCommand(xaaResourceServersListCmd, xaaResourceServersSearchCmd, xaaResourceServersGetCmd,
		cr, up, xaaResourceServersDeleteCmd, xaaResourceServersHistoryCmd, rs)
	xaaCmd.AddCommand(xaaResourceServersCmd)
}

// addXAAResourceServerFieldFlags registers the optional fields create and
// update share.
func addXAAResourceServerFieldFlags(c *cobra.Command, descUsage string) {
	f := c.Flags()
	f.String("description", "", descUsage)
	addRepeatableStringFlag(c, "resource-uri", "Resource identifier (RFC 8707) the server governs (repeatable; replaces the list)")
	addProtoDurationFlag(c, "max-grant-lifetime", "Maximum grant lifetime, as a protobuf duration, e.g. 3600s, or a Go duration such as 1h")
	f.String("signing-algorithm", "", "JWS algorithm for grants: "+xaaSigningAlgFlag.usage()+" (omit for the tenant default)")
	f.Bool("require-proof-of-possession", false, "Mint proof-of-possession-bound grants for clients presenting a DPoP proof")
	f.Bool("disabled", false, "Reject token exchanges for this server without removing it")
}
