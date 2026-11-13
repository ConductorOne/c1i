package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// resourceTypeKinds is the AppResourceType enum, and the single source for
// every place this repo names it. TestResourceTypeKindsAreDocumented holds the
// help text, README.md and the configure-new-app guide to this list.
var resourceTypeKinds = []string{"ROLE", "GROUP", "LICENSE", "PROJECT", "CATALOG", "CUSTOM", "VAULT", "PROFILE_TYPE"}

// resourceTypeFlag passes unknowns: the enum has no prefix, so the server
// stays authoritative if it grows.
var resourceTypeFlag = func() enumFlag {
	e := enumFlag{flag: "resource-type", unknown: enumPassAny}
	for _, k := range resourceTypeKinds {
		e.values = append(e.values, enumValue{name: strings.ToLower(strings.ReplaceAll(k, "_", "-")), api: k})
	}
	return e
}()

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
Reuse the one that exists with --resource-type-id and drop both
--resource-type and --resource-type-display-name; either alongside the id is a
usage error. Likewise, pass --resource-id to reuse a resource and drop
--resource-display-name.

Omitting --duration-grant leaves the entitlement at standing access
(durationUnset in the response). It takes a whole-number protobuf (3600s) or
Go (8h, 1h30m) duration, sent in seconds since the API parses only that form.
No sign, no decimals: -1h or 1.5h exits 2 before sending. Zero is sent as
"0s".

Each --<kind>-policy-id is checked before the first create: it must name an
existing, undeleted policy of the slot's type (a grant policy for
--emergency-grant-policy-id), or the command exits 2 and creates nothing. An
empty one is a usage error. Omitting one leaves that policy unset on the
entitlement. --dry-run skips the check.

Assign owners inline with --owner-id: the create request carries
appEntitlementOwnerIds, so no follow-up call is needed. An empty --owner-id is
a usage error, not an owner quietly dropped. Each id is checked before the
first create and must name an existing user, or the command exits 2 and
creates nothing; the check is a user read, so a credential without read access
exits 3, and --dry-run skips it. Owner provisioning is
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

Prints the created entitlement as pretty JSON, unwrapped like "entitlements
get": the id is .id. The response echoes appResourceTypeId/appResourceId and
expands both objects, so every id this command touched is in that one payload.
--fields is a usage error on a mutation. A --dry-run preview carries
NEW_APP_RESOURCE_TYPE_ID / NEW_APP_RESOURCE_ID literally in its URLs and
bodies; extract ids with jq -er, which fails on a preview.

Example:
  c1i entitlements create --app-id "$APP_ID" \
    --display-name "Payroll admin" --resource-type-display-name "Payroll role" \
    --slug member --alias payroll_admin`,
	RunE: func(cmd *cobra.Command, args []string) error {

		plan, err := buildEntitlementCreatePlan(cmd)
		if err != nil {
			// Already a *usageError from the flag helpers; flags.go documents
			// that callers must not re-wrap.
			return err
		}

		// Both checks run before step 1: an unknown owner (or bad policy)
		// otherwise fails at step 3, after the resource type and resource
		// already exist. --dry-run skips the pre-reads.
		opts := []mutationOpt{withPreRead(policyBindingsCheck(cmd, entitlementPolicyKinds))}
		if len(plan.owners) > 0 {
			opts = append(opts, withPreRead(existingUsersCheck("owner-id", plan.owners)))
		}
		return sendMutationSequence(cmd, plan.steps(), plan.placeholderNote(), opts...)
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

	// owners is the deduped --owner-id list, also in entitlementBody. The
	// create sequence pre-checks each one before creating anything.
	owners []string
}

// typeCreateOnlyFlags describe a resource type this command would create, so
// each is refused alongside --resource-type-id. resourceCreateOnlyFlags is the
// same for a resource and --resource-id. Named once each: the reject call, the
// retry message and the docs that tell a reader what to drop all read these.
var (
	typeCreateOnlyFlags     = []string{"resource-type", "resource-type-display-name"}
	resourceCreateOnlyFlags = []string{"resource-display-name"}
)

// buildEntitlementCreatePlan resolves flags into the request sequence. Pure (no
// network / auth) so --dry-run and unit tests exercise the same bodies the live
// requests send. Optional fields are omitted when empty rather than sent as
// empty strings/arrays.
func buildEntitlementCreatePlan(cmd *cobra.Command) (*entitlementCreatePlan, error) {
	appID, _ := cmd.Flags().GetString("app-id")
	displayName, _ := cmd.Flags().GetString("display-name")

	resourceTypeID, _ := cmd.Flags().GetString("resource-type-id")
	resourceID, _ := cmd.Flags().GetString("resource-id")
	if resourceID != "" && resourceTypeID == "" {
		return nil, &usageError{fmt.Errorf("--resource-id requires --resource-type-id: the entitlement carries both ids, and the server rejects a missing one with " +
			`invalid CreateAppEntitlementRequest.AppResourceTypeId: value does not match regex pattern "^[a-zA-Z0-9]{27}$"`)}
	}

	if err := rejectFlagsForReusedObject(cmd, resourceTypeID, "--resource-type-id", typeCreateOnlyFlags...); err != nil {
		return nil, err
	}
	if err := rejectFlagsForReusedObject(cmd, resourceID, "--resource-id", resourceCreateOnlyFlags...); err != nil {
		return nil, err
	}

	resourceType, err := resourceTypeFlag.parse(mustGetString(cmd, "resource-type"))
	if err != nil {
		return nil, err
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
			"resourceType": resourceType,
		}
		p.typeOnlyFlags = changedFlags(cmd, typeCreateOnlyFlags...)
	}
	if resourceID == "" {
		p.resourceBody = map[string]any{
			"displayName": flagOrDefault(cmd, "resource-display-name", displayName),
		}
		p.resourceOnlyFlags = changedFlags(cmd, resourceCreateOnlyFlags...)
	}

	// Flag name -> request field, equal except where the wire key is camelCase.
	for flag, key := range map[string]string{
		"description": "description",
		"slug":        "slug",
		"alias":       "alias",
	} {
		if v, _ := cmd.Flags().GetString(flag); v != "" {
			p.entitlementBody[key] = v
		}
	}
	if v, ok, err := protoDurationFlag(cmd, "duration-grant"); err != nil {
		return nil, err
	} else if ok {
		p.entitlementBody["durationGrant"] = v
	}
	if err := applyPolicyIDs(cmd, entitlementPolicyKinds, p.entitlementBody); err != nil {
		return nil, err
	}
	// An owner id from an unset shell variable would otherwise create the
	// entitlement with fewer owners than asked for, and owner reads are async,
	// so nothing downstream can tell that apart from "not provisioned yet".
	// A repeated id 500s, as it does on the owners PUT.
	owners, err := repeatableStringFlag(cmd, "owner-id")
	if err != nil {
		return nil, err
	}
	if len(owners) > 0 {
		p.owners = dedupe(owners)
		p.entitlementBody["appEntitlementOwnerIds"] = p.owners
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

// steps is the create sequence; a step is omitted when the caller supplied
// that object's id. The last step's response is the printed document.
func (p *entitlementCreatePlan) steps() []mutationStep {
	var steps []mutationStep
	if p.resourceTypeBody != nil {
		steps = append(steps, mutationStep{
			Build: func([]string) mutation {
				return mutation{Method: http.MethodPost, Path: p.resourceTypesPath(), Body: p.resourceTypeBody}
			},
			Placeholder: newResourceTypeIDPlaceholder,
			ResultID:    func(data []byte) (string, error) { return createdObjectID(data, "appResourceType") },
			WrapErr: func(err error, _ []string) error {
				return stepAPIError(err, "the app resource type", "")
			},
		})
	}
	if p.resourceBody != nil {
		steps = append(steps, mutationStep{
			Build: func(ids []string) mutation {
				typeID, _ := p.knownIDs(ids)
				return mutation{Method: http.MethodPost, Path: p.resourcesPath(typeID), Body: p.resourceBody}
			},
			Placeholder: newResourceIDPlaceholder,
			ResultID:    func(data []byte) (string, error) { return createdObjectID(data, "appResource") },
			WrapErr: func(err error, ids []string) error {
				typeID, _ := p.knownIDs(ids)
				return stepAPIError(err, "the app resource", p.createdSoFar(typeID, ""))
			},
		})
	}
	return append(steps, mutationStep{
		Build: func(ids []string) mutation {
			typeID, resID := p.knownIDs(ids)
			return mutation{Method: http.MethodPost, Path: p.entitlementsPath(), Body: p.fullEntitlementBody(typeID, resID)}
		},
		WrapErr: func(err error, ids []string) error {
			typeID, resID := p.knownIDs(ids)
			return stepAPIError(err, "the entitlement", p.createdSoFar(typeID, resID))
		},
	})
}

// knownIDs returns the resource type and resource ids so far: supplied ones,
// and created ones taken from ids in step order.
func (p *entitlementCreatePlan) knownIDs(ids []string) (typeID, resourceID string) {
	typeID, resourceID = p.resourceTypeID, p.resourceID
	i := 0
	next := func() string {
		if i < len(ids) {
			i++
			return ids[i-1]
		}
		return ""
	}
	if p.resourceTypeBody != nil {
		typeID = next()
	}
	if p.resourceBody != nil {
		resourceID = next()
	}
	return typeID, resourceID
}

// stepAPIError renames the helper's "API error: ..." to name the failed step,
// keeping the cause in the chain; any other error is returned as is.
func stepAPIError(err error, what, createdSoFar string) error {
	inner := errors.Unwrap(err)
	if inner == nil || !strings.HasPrefix(err.Error(), "API error: ") {
		return err
	}
	return fmt.Errorf("API error creating %s%s: %w", what, createdSoFar, inner)
}

// placeholderNote explains the preview's stand-in ids (on stderr); "" when the
// caller supplied both ids.
func (p *entitlementCreatePlan) placeholderNote() string {
	var used []string
	if p.resourceTypeBody != nil {
		used = append(used, newResourceTypeIDPlaceholder)
	}
	if p.resourceBody != nil {
		used = append(used, newResourceIDPlaceholder)
	}
	if len(used) == 0 {
		return ""
	}
	subject := "stands in for an id"
	if len(used) > 1 {
		subject = "stand in for ids"
	}
	return fmt.Sprintf("requests are sent in order; %s %s the preceding response returns", strings.Join(used, "/"), subject)
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
	f.String("display-name", "", "Display name for the new entitlement ("+displayNameRule+"; also names the resource type and resource this command creates)")
	f.String("description", "", "Description for the new entitlement")
	f.String("slug", "", "Slug for the new entitlement (e.g. member)")
	f.String("alias", "", "Alias for the new entitlement; exact-match queryable")
	allowEmpty(entitlementsCreateCmd, emptyIsOmitted, "description", "slug", "alias")
	addProtoDurationFlag(cmd, "duration-grant", "Maximum grant duration as a protobuf duration, e.g. 3600s; a Go duration such as 8h is converted to seconds; a sign or decimal (-1h, 1.5h) exits 2; omit for standing access")
	registerPolicyIDFlags(cmd, entitlementPolicyKinds, " (omit to leave unset)")
	// StringArray, not StringSlice: the slice parser drops an empty value
	// outright, so `--owner-id "" --owner-id U` would lose the empty one
	// before this command could reject it.
	addRepeatableStringFlag(cmd, "owner-id", "C1 user ID to own the new entitlement (repeatable)")
	f.String("resource-type", "custom", "Kind of resource type to create: "+resourceTypeFlag.usage())
	f.String("resource-type-id", "", "Existing app resource type to reuse instead of creating one")
	f.String("resource-type-display-name", "", "Display name for the resource type this command creates (default: --display-name)")
	f.String("resource-id", "", "Existing app resource to reuse instead of creating one; requires --resource-type-id")
	f.String("resource-display-name", "", "Display name for the resource this command creates (default: --display-name)")
	markRequired(cmd, "app-id", "display-name")
}

func init() {
	addEntitlementCreateFlags(entitlementsCreateCmd)
	markMutating(entitlementsCreateCmd)
	entitlementsCmd.AddCommand(entitlementsCreateCmd)
}
