package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// setupOwner is the object whose scope these commands edit. Campaigns and
// access-review templates expose the same setup routes under different paths.
type setupOwner struct {
	pathFmt string // client.Path format; its one %s is the owner id
	use     string // positional, e.g. "<campaign-id>"
	noun    string // e.g. "campaign"
	// scopeKey is the read's stable top-level key, standing in for an id:
	// a campaign answers scopeV2, a template scope.
	scopeKey string
}

var campaignSetup = setupOwner{pathFmt: "/api/v1/access_review/%s", use: "<campaign-id>", noun: "campaign", scopeKey: "scopeV2"}

func (o setupOwner) path(id, suffix string) string {
	return client.Path(o.pathFmt, id) + suffix
}

var (
	setupEntitlementShape    = appRefShape{name: "AccessReviewSetupEntitlementInput", idKey: "appEntitlementId", article: "an"}
	excludedEntitlementShape = appRefShape{name: "AccessReviewExcludedEntitlementInput", idKey: "appEntitlementId", article: "an"}
	resourceTypeRefShape     = appRefShape{name: "ResourceTypeIdRef", idKey: "resourceTypeId", article: "a"}
)

// refKind is what a referenced id names and how to look it up.
type refKind struct {
	what string
	path func(r appRef) string
}

var (
	entitlementRefKind = refKind{"entitlement", func(r appRef) string {
		return client.Path("/api/v1/apps/%s/entitlements/%s", r.AppID, r.ID)
	}}
	resourceTypeRefKind = refKind{"resource type", func(r appRef) string {
		return client.Path("/api/v1/apps/%s/resource_types/%s", r.AppID, r.ID)
	}}
)

const setupRefHelp = "Looks up each referenced %s first: an unknown one exits 4 and a deleted one 2, before anything is sent, --dry-run included."

// labeledRef is a reference plus how an error names it.
type labeledRef struct {
	ref   appRef
	label string
}

// existingRefsCheck looks up each reference before the send. The server
// answers an unknown one inconsistently (400, 500, or a silent skip), so the
// check gives the not-found exit every caller expects.
func existingRefsCheck(kind refKind, refs []labeledRef) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		return nil, requireExistingRefs(ctx, c, kind, refs)
	}}
}

func requireExistingRefs(ctx context.Context, c *client.Client, kind refKind, refs []labeledRef) error {
	seen := map[[2]string]bool{}
	for _, r := range refs {
		key := [2]string{r.ref.AppID, r.ref.ID}
		if seen[key] {
			continue
		}
		seen[key] = true
		data, err := c.Get(ctx, kind.path(r.ref), nil)
		if err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return &refNotFoundError{fmt.Sprintf("%s: no such %s", r.label, kind.what), apiErr}
			}
			return fmt.Errorf("checking %s: %w", r.label, err)
		}
		if !json.Valid(data) {
			return &nonJSONResponseError{fmt.Errorf("checking %s: the %s lookup did not return JSON", r.label, kind.what)}
		}
		var resp map[string]json.RawMessage
		if json.Unmarshal(data, &resp) != nil {
			return &nonJSONResponseError{fmt.Errorf("checking %s: the %s lookup did not return an object", r.label, kind.what)}
		}
		for _, raw := range resp {
			if hasJSONField(raw, "deletedAt", 3) {
				return &usageError{fmt.Errorf("%s: the %s is deleted", r.label, kind.what)}
			}
		}
	}
	return nil
}

func flagRefs(refs []appRef) []labeledRef {
	out := make([]labeledRef, len(refs))
	for i, r := range refs {
		out[i] = labeledRef{r, fmt.Sprintf("--entitlement-id %q under --app-id %q", r.ID, r.AppID)}
	}
	return out
}

// bodyFileRefs parses body's field, in either spelling, when present and non-null.
func bodyFileRefs(body map[string]any, field string, shape appRefShape) ([]labeledRef, error) {
	v, key, err := bodyField(body, field)
	if err != nil || v == nil {
		return nil, err
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, &usageError{fmt.Errorf("--body-file %s must be a JSON array, not %s", key, jsonKindName(v))}
	}
	refs, err := parseAppRefs(arr, "--body-file "+key, shape)
	if err != nil {
		return nil, err
	}
	out := make([]labeledRef, len(refs))
	for i, r := range refs {
		out[i] = labeledRef{r, fmt.Sprintf("--body-file %s[%d] (appId %q, %s %q)", key, i, r.AppID, shape.idKey, r.ID)}
	}
	return out, nil
}

func newSetupScopeGetCmd(o setupOwner) *cobra.Command {
	return &cobra.Command{
		Use:   "get " + o.use,
		Short: "Get a " + o.noun + "'s scope, entitlement selection and exclusions (pretty JSON)",
		Long: `Get a ` + o.noun + `'s scope and entitlement setup as one JSON object: the selected
entitlements (list), the scope, excludedEntitlements and excludedResourceTypes.
The response is not paginated.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL, err := GetBaseURL()
			if err != nil {
				return err
			}
			c, err := newClient(cmd, baseURL)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
			data, err := c.Get(cmd.Context(), o.path(args[0], "/scope_and_entitlements"), nil)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}
			return writeResource(cmd, data, o.scopeKey)
		},
	}
}

// newSetupBodyCmd is a scope replacement: the body file goes out as is, after
// the references in refKey are looked up.
func newSetupBodyCmd(o setupOwner, use, short, long, suffix, refKey string, shape appRefShape, kind refKind) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use + " " + o.use,
		Short: short,
		Long:  long + "\n\n" + fmt.Sprintf(setupRefHelp, kind.what),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readRequiredJSONObject(cmd, "body-file")
			if err != nil {
				return err
			}
			refs, err := bodyFileRefs(body, refKey, shape)
			if err != nil {
				return err
			}
			return sendMutation(cmd, mutation{Method: http.MethodPost, Path: o.path(args[0], suffix), Body: body, Unwrap: byID(o.scopeKey)},
				withPreRead(existingRefsCheck(kind, refs)))
		},
	}
	cmd.Flags().String("body-file", "", "Full request JSON object (file or \"-\" for stdin)")
	ownBodyKeys(cmd, "body-file", argBodyKeys(0, o.use, "accessReviewId", "access_review_id"))
	annotateRequired(cmd, "body-file")
	markMutating(cmd)
	return cmd
}

// newSetupRefsCmd sends --app-id with each --entitlement-id as body[key].
func newSetupRefsCmd(o setupOwner, use, short, long, suffix, key string, shape appRefShape) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use + " " + o.use,
		Short: short,
		Long:  long + "\n\n" + fmt.Sprintf(setupRefHelp, "entitlement"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := appRefsFromFlags(cmd, true, shape)
			if err != nil {
				return err
			}
			return sendMutation(cmd, mutation{Method: http.MethodPost, Path: o.path(args[0], suffix), Body: map[string]any{key: refs}, Unwrap: byID(o.scopeKey)},
				withPreRead(existingRefsCheck(entitlementRefKind, flagRefs(refs))))
		},
	}
	cmd.Flags().String("app-id", "", "Application ID that owns the entitlements")
	addRepeatableStringFlag(cmd, "entitlement-id", "Application entitlement ID (repeatable)")
	markRequired(cmd, "app-id")
	annotateRequired(cmd, "entitlement-id")
	markMutating(cmd)
	return cmd
}

var accessReviewScopeCmd = &cobra.Command{
	Use:   "scope",
	Short: "Read or replace an access-review campaign's scope and entitlement selection",
}

var accessReviewEntitlementsCmd = &cobra.Command{
	Use:   "entitlements",
	Short: "Add or remove entitlements in an access-review campaign's selection",
}

var accessReviewExcludedEntitlementsCmd = &cobra.Command{
	Use:   "excluded-entitlements",
	Short: "Add or remove entitlements in an access-review campaign's excluded set",
}

var accessReviewScopeGetCmd = newSetupScopeGetCmd(campaignSetup)

var accessReviewScopeSetCmd = newSetupBodyCmd(campaignSetup, "set",
	"Replace a campaign's scope and its whole entitlement selection (pretty JSON)",
	`Replace a campaign's scopeV2 and its whole entitlement selection from a JSON
request object:

  {"scopeV2": {...}, "entitlements": [{"appId": "…", "appEntitlementId": "…"}]}

The API documents: "Each call replaces the whole selection." To change only some
entitlements, use "access-reviews entitlements add|remove". Prints the API's
response: its "list" holds only the entitlements this call ADDED (the server
returns the delta), not the resulting selection -- follow with "access-reviews
scope get" to read that.`,
	"/scope_and_entitlements", "entitlements", setupEntitlementShape, entitlementRefKind)

var accessReviewScopeSetExcludedCmd = newSetupBodyCmd(campaignSetup, "set-excluded",
	"Replace a campaign's scope and its whole excluded-entitlement set (pretty JSON)",
	`Replace a campaign's scopeV2 and its whole excluded-entitlement set from a JSON
request object:

  {"scopeV2": {..., "excludedSpecificResources": {}},
   "excludedEntitlements": [{"appId": "…", "appEntitlementId": "…"}]}

Exclusions apply only while scopeV2 sets excludedSpecificResources. The API
documents support for BY_USERS, selector/resource-type BY_ENTITLEMENTS,
BY_INHERITANCE and BY_ACCESS_CONFLICTS campaigns; another scope is refused with
"exclusions are not supported for this access review scope" (exit 2). To change
only some exclusions, use "access-reviews excluded-entitlements add|remove".
Prints the API's response, {"excludedEntitlements": [...], "scopeV2": {...}}.

To exclude by resource type, set scopeV2.excludedAppSelectionCriteria here. The
API's separate scope_by_excluded_resource_type route is "superseded and
scheduled for removal", so c1i has no command for it.`,
	"/scope_and_excluded_entitlements", "excludedEntitlements", excludedEntitlementShape, entitlementRefKind)

var accessReviewScopeSetByResourceTypeCmd = newSetupBodyCmd(campaignSetup, "set-by-resource-type",
	"Replace a campaign's scope and its resource-type selection (prints {})",
	`Replace a campaign's scopeV2 and its resource-type selection from a JSON request
object:

  {"scopeV2": {"resourceTypeSelections": {}, ...},
   "resourceTypeSelections": [{"appId": "…", "resourceTypeId": "…"}]}

The API documents that the list "Replaces all previously selected resource
types." Prints the API's response, which is {}. In the run checked,
"access-reviews scope get" did not list the selected resource types back.`,
	"/scope_by_resource_type", "resourceTypeSelections", resourceTypeRefShape, resourceTypeRefKind)

var accessReviewEntitlementsAddCmd = newSetupRefsCmd(campaignSetup, "add",
	"Add entitlements to a campaign's selection (pretty JSON)",
	`Add one application's entitlements to a campaign's selection without replacing
the rest. An entitlement that is already selected is skipped. The server takes 1
to 200 per call ("value must contain between 1 and 200 items, inclusive"). A
campaign with no scope yet gets scopeV2.specificResources; one with another
scope is refused ("campaign entitlements can only be added or removed when the
campaign's apps and resources scope is set to specific entitlements", exit 2):
change it with "access-reviews scope set".
Prints the API's response, {"entitlementCount": N}: the selection's size after
the call.`,
	"/entitlements/add", "entitlements", setupEntitlementShape)

var accessReviewEntitlementsRemoveCmd = newSetupRefsCmd(campaignSetup, "remove",
	"Remove entitlements from a campaign's selection (pretty JSON)",
	`Remove one application's entitlements from a campaign's selection. An
entitlement that is not selected is ignored. Like "add", it needs a campaign
whose scopeV2 sets specificResources. Prints the API's response,
{"entitlementCount": N, "removedCount": M}: the selection's size after the call
and how many of the given entitlements were removed.`,
	"/entitlements/remove", "entitlements", setupEntitlementShape)

var accessReviewExcludedEntitlementsAddCmd = newSetupRefsCmd(campaignSetup, "add",
	"Add entitlements to a campaign's excluded set (prints {})",
	`Add one application's entitlements to a campaign's excluded set without
replacing the rest. An entitlement that is already excluded is skipped. The
campaign's scopeV2 must set excludedSpecificResources first, or the server
answers "excluded_specific_resources must be active before adding excluded
entitlements" (exit 2); set it with "access-reviews scope set-excluded". Prints
the API's response, which is {}.`,
	"/excluded_entitlements/add_delete", "toAdd", excludedEntitlementShape)

var accessReviewExcludedEntitlementsRemoveCmd = newSetupRefsCmd(campaignSetup, "remove",
	"Remove entitlements from a campaign's excluded set (prints {})",
	`Remove one application's entitlements from a campaign's excluded set. An
entitlement that is not excluded is ignored. In the run checked, removing the
last excluded entitlement also cleared scopeV2.excludedSpecificResources, so a
later "excluded-entitlements add" needed "access-reviews scope set-excluded"
first. Prints the API's response, which is {}.`,
	"/excluded_entitlements/add_delete", "toDelete", excludedEntitlementShape)

func init() {
	accessReviewScopeCmd.AddCommand(accessReviewScopeGetCmd, accessReviewScopeSetCmd, accessReviewScopeSetExcludedCmd, accessReviewScopeSetByResourceTypeCmd)
	accessReviewEntitlementsCmd.AddCommand(accessReviewEntitlementsAddCmd, accessReviewEntitlementsRemoveCmd)
	accessReviewExcludedEntitlementsCmd.AddCommand(accessReviewExcludedEntitlementsAddCmd, accessReviewExcludedEntitlementsRemoveCmd)
	accessReviewsCmd.AddCommand(accessReviewScopeCmd, accessReviewEntitlementsCmd, accessReviewExcludedEntitlementsCmd)
}

// hasJSONField reports whether raw (an object, possibly nested under the
// response's view envelope) carries the named field with a non-null value.
func hasJSONField(raw json.RawMessage, field string, depth int) bool {
	if depth < 0 {
		return false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return false
	}
	if v, ok := obj[field]; ok && string(v) != "null" && string(v) != `""` {
		return true
	}
	for _, v := range obj {
		if len(v) > 0 && v[0] == '{' && hasJSONField(v, field, depth-1) {
			return true
		}
	}
	return false
}
