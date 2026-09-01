package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// resourceTypeKinds is the AppResourceType enum, and the single source for
// every place this repo names it. TestResourceTypeKindsAreDocumented holds the
// help text, README.md and the configure-new-app guide to this list. Not used
// to validate --resource-type: the server stays authoritative if the enum
// grows, and normalizeResourceType passes an unknown name through.
var resourceTypeKinds = []string{"ROLE", "GROUP", "LICENSE", "PROJECT", "CATALOG", "CUSTOM", "VAULT", "PROFILE_TYPE"}

// Stand-ins printed by --dry-run for ids only a preceding response can supply.
// Alphanumeric + "_" so client.Path leaves them unescaped and the previewed
// path stays readable.
const (
	newResourceTypeIDPlaceholder = "NEW_APP_RESOURCE_TYPE_ID"
	newResourceIDPlaceholder     = "NEW_APP_RESOURCE_ID"
)

var entitlementsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an app entitlement, with its resource type and resource (pretty JSON)",
	Long: `Create an app entitlement, plus the app resource type and resource it
points at. The app itself need not be manually managed -- the entitlement this
creates is (isManuallyManaged is true on it, false on the app it was created
on).

An entitlement points at an app resource, which lives under an app resource
type, so this is up to three POSTs in order:

  1. POST /api/v1/apps/{app-id}/resource_types
  2. POST /api/v1/apps/{app-id}/resource_types/{app-resource-type-id}/resources
  3. POST /api/v1/apps/{app-id}/entitlements

Steps 1 and 2 are skipped for whichever of --resource-type-id/--resource-id
you already have, so one resource type can carry many resources and one
resource many entitlements:

  --app-id --display-name                  all three (a new type and resource)
  ... --resource-type-id                   steps 2 and 3 (a new resource
                                           under an existing type)
  ... --resource-type-id --resource-id     step 3 only

Both ids are required by the server even though the OpenAPI schema marks only
displayName required — omitting them fails with:
  invalid CreateAppEntitlementRequest.AppResourceTypeId: value does not match regex pattern "^[a-zA-Z0-9]{27}$"
so --resource-id without --resource-type-id is rejected here, at exit 2,
before anything is sent.

--resource-type describes the resource type this command creates, so passing
it together with --resource-type-id is a usage error rather than a silently
ignored flag. Case-insensitive here; the API itself takes only the uppercase
form. One of:
  ` + strings.Join(resourceTypeKinds, ", ") + `
Only CUSTOM can repeat on one app: a second resource type of any other kind
fails with a 500 (exit 6, though retrying never helps):
  app resource type already exists
Reuse the one that exists with --resource-type-id instead.

Omitting --duration-grant leaves the entitlement at standing access
(durationUnset in the response). It takes a protobuf duration, not a Go one --
seconds with an "s" suffix, e.g. 3600s. "1h" is refused by the server with:
  invalid google.protobuf.Duration value "1h"

Assign owners inline with --owner-id: the create request carries
appEntitlementOwnerIds, so no follow-up call is needed. An empty --owner-id is
a usage error, not an owner quietly dropped. Owner provisioning is
asynchronous — one measured create took 116s to read back on
"GET /api/v1/apps/{app-id}/entitlements/{id}/ownerids".

--dry-run previews every request in the sequence. The ids that only exist
after a real step 1 or 2 print as ` + newResourceTypeIDPlaceholder + `/` +
		newResourceIDPlaceholder + `.

There is no rollback. The three creates are independent, so if step 2 or 3
fails the objects earlier steps created still exist; the error names them, the
flags that reuse them, and the create-only flags a retry has to drop (a reused
id makes those a usage error), verbatim:
  (already created: re-run with --resource-type-id <id>, dropping --resource-type-display-name, to reuse instead of duplicating)

Prints the created entitlement as pretty JSON under "appEntitlementView"
(--fields is not applied to mutation output). The response echoes
appResourceTypeId/appResourceId and expands both objects, so every id this
command touched is in that one payload.

Example:
  c1i entitlements create --app-id "$APP_ID" \
    --display-name "Payroll admin" --resource-type-display-name "Payroll role" \
    --slug member --alias payroll_admin`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "display-name"); err != nil {
			return err
		}

		plan, err := buildEntitlementCreatePlan(cmd)
		if err != nil {
			// Already a *usageError from the flag helpers; flags.go documents
			// that callers must not re-wrap.
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		if dryRunActive() {
			return plan.previewRequests(cmd)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		return plan.run(cmd, c)
	},
}

// entitlementCreatePlan is the resolved create sequence: a step's body is nil
// when the caller supplied that object's id and the step is skipped.
type entitlementCreatePlan struct {
	appID string

	resourceTypeID   string         // reuse this type when non-empty
	resourceTypeBody map[string]any // otherwise create one from this

	resourceID   string
	resourceBody map[string]any

	// Create-only flags this invocation passed, per object. A retry that
	// reuses the object has to drop them; rejectFlagsForReusedObject refuses
	// them alongside the id createdSoFar hands back.
	typeOnlyFlags     []string
	resourceOnlyFlags []string

	// entitlementBody carries everything except appResourceTypeId and
	// appResourceId, which are only known once the steps above have run.
	entitlementBody map[string]any
}

// buildEntitlementCreatePlan resolves flags into the request sequence. Pure (no
// network / auth) so --dry-run and unit tests exercise the same bodies the live
// requests send. Optional fields are omitted when empty rather than sent as
// empty strings/arrays.
func buildEntitlementCreatePlan(cmd *cobra.Command) (*entitlementCreatePlan, error) {
	appID, _ := cmd.Flags().GetString("app-id")
	displayName, _ := cmd.Flags().GetString("display-name")

	resourceTypeID, err := requireNonEmptyIfSet(cmd, "resource-type-id")
	if err != nil {
		return nil, err
	}
	resourceID, err := requireNonEmptyIfSet(cmd, "resource-id")
	if err != nil {
		return nil, err
	}
	if resourceID != "" && resourceTypeID == "" {
		return nil, &usageError{fmt.Errorf("--resource-id requires --resource-type-id: the entitlement carries both ids, and the server rejects a missing one with " +
			`invalid CreateAppEntitlementRequest.AppResourceTypeId: value does not match regex pattern "^[a-zA-Z0-9]{27}$"`)}
	}

	if err := rejectFlagsForReusedObject(cmd, resourceTypeID, "--resource-type-id", "resource-type", "resource-type-display-name"); err != nil {
		return nil, err
	}
	if err := rejectFlagsForReusedObject(cmd, resourceID, "--resource-id", "resource-display-name"); err != nil {
		return nil, err
	}

	// --resource-type has a default, so an explicit "" is a mistake. Checked
	// after the reuse rejection above, whose message is the more useful one when
	// --resource-type-id is also set.
	resourceType, err := requireNonEmptyIfSet(cmd, "resource-type")
	if err != nil {
		return nil, err
	}

	// Same for the display-name overrides: an explicit "" would fall back to
	// --display-name and mis-name the object rather than fail.
	for _, n := range []string{"resource-type-display-name", "resource-display-name"} {
		if _, err := requireNonEmptyIfSet(cmd, n); err != nil {
			return nil, err
		}
	}

	p := &entitlementCreatePlan{
		appID:           appID,
		resourceTypeID:  resourceTypeID,
		resourceID:      resourceID,
		entitlementBody: map[string]any{"displayName": displayName},
	}

	if resourceTypeID == "" {
		p.resourceTypeBody = map[string]any{
			"displayName":  flagOrDefault(cmd, "resource-type-display-name", displayName),
			"resourceType": normalizeResourceType(resourceType),
		}
		p.typeOnlyFlags = changedFlags(cmd, "resource-type", "resource-type-display-name")
	}
	if resourceID == "" {
		p.resourceBody = map[string]any{
			"displayName": flagOrDefault(cmd, "resource-display-name", displayName),
		}
		p.resourceOnlyFlags = changedFlags(cmd, "resource-display-name")
	}

	// Flag name -> request field, equal except where the wire key is camelCase.
	for flag, key := range map[string]string{
		"description":    "description",
		"slug":           "slug",
		"alias":          "alias",
		"duration-grant": "durationGrant",
	} {
		if v, _ := cmd.Flags().GetString(flag); v != "" {
			p.entitlementBody[key] = v
		}
	}
	// An owner id from an unset shell variable would otherwise create the
	// entitlement with fewer owners than asked for, and owner reads are async,
	// so nothing downstream can tell that apart from "not provisioned yet".
	// pflag's CSV round-trip drops a lone `--owner-id ""` entirely, so an empty
	// owner arrives either as a missing value or an empty one.
	owners, _ := cmd.Flags().GetStringArray("owner-id")
	empty := cmd.Flags().Changed("owner-id") && len(owners) == 0
	for _, id := range owners {
		empty = empty || strings.TrimSpace(id) == ""
	}
	if empty {
		return nil, &usageError{fmt.Errorf("--owner-id values must be non-empty")}
	}
	if len(owners) > 0 {
		p.entitlementBody["appEntitlementOwnerIds"] = owners
	}

	return p, nil
}

// rejectFlagsForReusedObject fails when flags that only describe an object this
// command would create are passed alongside the id of an existing one, rather
// than ignoring them and creating something the caller didn't ask for.
func rejectFlagsForReusedObject(cmd *cobra.Command, reusedID, reusedFlag string, names ...string) error {
	if reusedID == "" {
		return nil
	}
	if passed := changedFlags(cmd, names...); len(passed) > 0 {
		return &usageError{fmt.Errorf("%s only applies when this command creates that object; drop it or drop %s", passed[0], reusedFlag)}
	}
	return nil
}

// changedFlags returns the named flags the caller actually passed, "--"-prefixed.
func changedFlags(cmd *cobra.Command, names ...string) []string {
	var passed []string
	for _, n := range names {
		if cmd.Flags().Changed(n) {
			passed = append(passed, "--"+n)
		}
	}
	return passed
}

// normalizeResourceType upper-cases the enum name so "custom" and
// "profile-type" work; anything unrecognized is passed through for the server
// to reject, keeping it authoritative if the enum grows.
func normalizeResourceType(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
}

func (p *entitlementCreatePlan) resourceTypesPath() string {
	return client.Path("/api/v1/apps/%s/resource_types", p.appID)
}

func (p *entitlementCreatePlan) resourcesPath(resourceTypeID string) string {
	return client.Path("/api/v1/apps/%s/resource_types/%s/resources", p.appID, resourceTypeID)
}

func (p *entitlementCreatePlan) entitlementsPath() string {
	return client.Path("/api/v1/apps/%s/entitlements", p.appID)
}

// fullEntitlementBody returns the step-3 body with the resource ids filled in,
// leaving p.entitlementBody untouched so a caller can build it more than once.
func (p *entitlementCreatePlan) fullEntitlementBody(resourceTypeID, resourceID string) map[string]any {
	body := make(map[string]any, len(p.entitlementBody)+2)
	for k, v := range p.entitlementBody {
		body[k] = v
	}
	body["appResourceTypeId"] = resourceTypeID
	body["appResourceId"] = resourceID
	return body
}

// run issues the planned requests in order and prints the entitlement the last
// one returns.
func (p *entitlementCreatePlan) run(cmd *cobra.Command, c *client.Client) error {
	ctx := cmd.Context()

	resourceTypeID := p.resourceTypeID
	if p.resourceTypeBody != nil {
		data, err := c.Post(ctx, p.resourceTypesPath(), p.resourceTypeBody)
		if err != nil {
			return fmt.Errorf("API error creating the app resource type: %w", err)
		}
		if resourceTypeID, err = createdObjectID(data, "appResourceType"); err != nil {
			return err
		}
	}

	resourceID := p.resourceID
	if p.resourceBody != nil {
		data, err := c.Post(ctx, p.resourcesPath(resourceTypeID), p.resourceBody)
		if err != nil {
			return fmt.Errorf("API error creating the app resource%s: %w", p.createdSoFar(resourceTypeID, ""), err)
		}
		if resourceID, err = createdObjectID(data, "appResource"); err != nil {
			return err
		}
	}

	data, err := c.Post(ctx, p.entitlementsPath(), p.fullEntitlementBody(resourceTypeID, resourceID))
	if err != nil {
		return fmt.Errorf("API error creating the entitlement%s: %w", p.createdSoFar(resourceTypeID, resourceID), err)
	}

	return writeRawObject(cmd, data)
}

// createdSoFar names the objects THIS invocation created before failing, the
// flags that reuse them, and the create-only flags the retry must drop.
// Nothing is rolled back: a compensating delete can fail too, which would
// leave a worse state described by a less honest message.
func (p *entitlementCreatePlan) createdSoFar(resourceTypeID, resourceID string) string {
	var parts, drop []string
	if p.resourceTypeBody != nil && resourceTypeID != "" {
		parts = append(parts, "--resource-type-id "+resourceTypeID)
		drop = append(drop, p.typeOnlyFlags...)
	}
	if p.resourceBody != nil && resourceID != "" {
		parts = append(parts, "--resource-id "+resourceID)
		drop = append(drop, p.resourceOnlyFlags...)
	}
	if len(parts) == 0 {
		return ""
	}
	msg := " (already created: re-run with " + strings.Join(parts, " ")
	if len(drop) > 0 {
		// Named, not merely implied: the reused id makes these a usage error,
		// so a retry that kept them would exit 2.
		msg += ", dropping " + strings.Join(drop, " ") + ","
	}
	return msg + " to reuse instead of duplicating)"
}

// previewRequests previews every request in the sequence, not just the first —
// two of the three are writes the caller would otherwise not see coming.
func (p *entitlementCreatePlan) previewRequests(cmd *cobra.Command) error {
	resourceTypeID, resourceID := p.resourceTypeID, p.resourceID

	var used []string
	if p.resourceTypeBody != nil {
		resourceTypeID = newResourceTypeIDPlaceholder
		used = append(used, newResourceTypeIDPlaceholder)
	}
	if p.resourceBody != nil {
		resourceID = newResourceIDPlaceholder
		used = append(used, newResourceIDPlaceholder)
	}
	if len(used) > 0 {
		subject := "stands in for an id"
		if len(used) > 1 {
			subject = "stand in for ids"
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"[dry-run] requests are sent in order; %s %s the preceding response returns\n",
			strings.Join(used, "/"), subject)
	}

	if p.resourceTypeBody != nil {
		if err := printDryRun(cmd, "POST", p.resourceTypesPath(), p.resourceTypeBody); err != nil {
			return err
		}
	}
	if p.resourceBody != nil {
		if err := printDryRun(cmd, "POST", p.resourcesPath(resourceTypeID), p.resourceBody); err != nil {
			return err
		}
	}
	return printDryRun(cmd, "POST", p.entitlementsPath(), p.fullEntitlementBody(resourceTypeID, resourceID))
}

// createdObjectID pulls <key>.id out of a create response. A 200 that carries
// no id would otherwise read as success while the next request in the chain
// addresses a path with an empty segment.
func createdObjectID(data []byte, key string) (string, error) {
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", &nonJSONResponseError{fmt.Errorf("failed to parse %s response: %w", key, err)}
	}
	raw, ok := resp[key]
	if !ok {
		return "", &nonJSONResponseError{fmt.Errorf("response carried no %q object", key)}
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", &nonJSONResponseError{fmt.Errorf("failed to parse %s response: %w", key, err)}
	}
	if obj.ID == "" {
		return "", &nonJSONResponseError{fmt.Errorf("response carried no %s.id", key)}
	}
	return obj.ID, nil
}

// addEntitlementCreateFlags registers the flag set. Shared with the tests so a
// flag can't be added to the command yet missed by what exercises it.
func addEntitlementCreateFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("app-id", "", "Application ID to create the entitlement on")
	f.String("display-name", "", "Display name for the new entitlement (also names the resource type and resource this command creates)")
	f.String("description", "", "Description for the new entitlement")
	f.String("slug", "", "Slug for the new entitlement (e.g. member)")
	f.String("alias", "", "Alias for the new entitlement; exact-match queryable")
	f.String("duration-grant", "", "Maximum grant duration as a protobuf duration, e.g. 3600s; omit for standing access")
	// StringArray, not StringSlice: the slice parser drops an empty value
	// outright, so `--owner-id "" --owner-id U` would lose the empty one
	// before this command could reject it.
	f.StringArray("owner-id", nil, "C1 user ID to own the new entitlement (repeatable)")
	f.String("resource-type", "CUSTOM", "Kind of resource type to create: "+strings.Join(resourceTypeKinds, ", "))
	f.String("resource-type-id", "", "Existing app resource type to reuse instead of creating one")
	f.String("resource-type-display-name", "", "Display name for the resource type this command creates (default: --display-name)")
	f.String("resource-id", "", "Existing app resource to reuse instead of creating one; requires --resource-type-id")
	f.String("resource-display-name", "", "Display name for the resource this command creates (default: --display-name)")
	markRequired(cmd, "app-id", "display-name")
}

func init() {
	addEntitlementCreateFlags(entitlementsCreateCmd)
	entitlementsCmd.AddCommand(entitlementsCreateCmd)
}
