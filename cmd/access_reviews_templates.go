package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var templateSetup = setupOwner{pathFmt: "/api/v1/access_review_template/%s", use: "<template-id>", noun: "template", scopeKey: "scope"}

func templatePath(id string) string { return client.Path("/api/v1/access_review_template/%s", id) }

// templatePolicyKind is the template's one policy slot. The server stores a
// policy of any type here, so the type is checked client-side.
var templatePolicyKind = policyKind{kind: "review", flag: "policy-id", field: "policyId", wantType: "POLICY_TYPE_CERTIFY"}

var templateScopeTypeFlag = enumFlag{flag: "scope-type", prefix: "ACCESS_REVIEW_SCOPE_TYPE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "by-entitlements", api: "ACCESS_REVIEW_SCOPE_TYPE_BY_ENTITLEMENTS"},
	{name: "by-access-conflicts", api: "ACCESS_REVIEW_SCOPE_TYPE_BY_ACCESS_CONFLICTS"},
	{name: "by-resource", api: "ACCESS_REVIEW_SCOPE_TYPE_BY_RESOURCE"},
	{name: "by-inheritance", api: "ACCESS_REVIEW_SCOPE_TYPE_BY_INHERITANCE"},
	{name: "by-users", api: "ACCESS_REVIEW_SCOPE_TYPE_BY_USERS"},
	{name: "by-access-profiles", api: "ACCESS_REVIEW_SCOPE_TYPE_BY_ACCESS_PROFILES"},
}}

const templatePolicyHelp = "Looks up the referenced policy first: an unknown id exits 4, and a deleted or non-certify policy exits 2, before anything is sent, --dry-run included."

var accessReviewTemplatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage access-review templates, reusable campaign configurations",
	Long: `Manage access-review templates: reusable campaign configurations (owners,
review policy, scope, schedule).

The API has no route that lists or searches templates. A template id comes from
"templates create" (.id), or from a campaign made from a template:
"access-reviews list" rows carry it as accessReviewTemplateId.`,
}

var accessReviewTemplatesGetCmd = &cobra.Command{
	Use:   "get <template-id>",
	Short: "Get an access-review template (pretty JSON)",
	Long: `Get an access-review template, unwrapped: the id is .id.

No route lists templates; see "access-reviews templates --help" for where an id
comes from. In the run checked, a deleted template was still returned, with
deletedAt null.`,
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
		data, err := c.Get(cmd.Context(), templatePath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "id")
	},
}

var accessReviewTemplatesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an access-review template (pretty JSON)",
	Long: `Create an access-review template.

The server requires a display name, a review policy and 1 to 10 owners
("value must contain between 1 and 10 items, inclusive"). Omitting --scope-type
left the server default, ACCESS_REVIEW_SCOPE_TYPE_BY_ENTITLEMENTS, in the run
checked. The flags create a template with no scope, no schedule and auto-start
off; set the scope with "access-reviews templates scope|entitlements".

--body-file is the full Create request, sent verbatim, for the fields the flags
don't cover (recurrenceRule, isCampaignScheduleEnabled, autoStartCampaign,
notification and column config, ...). It is mutually exclusive with every other
flag here; its ownerIds and policyId are looked up like the flags'.

Prints the template unwrapped like "access-reviews templates get": the id is
.id. Honors --dry-run.

` + templatePolicyHelp,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, fromFile, err := buildTemplateCreateBody(cmd)
		if err != nil {
			return err
		}
		owners, ownerKey, err := campaignOwnerIDs(body)
		if err != nil {
			return err
		}
		policyV, policyKey, _ := bodyField(body, "policyId")
		policyID, _ := policyV.(string)
		ownerLabel, policyLabel := "owner-id", "policy-id"
		if fromFile {
			ownerLabel, policyLabel = "body-file "+ownerKey, "body-file "+policyKey
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/access_review_template", Body: body},
			withPreRead(templatePolicyCheck(policyLabel, policyID)),
			withPreRead(existingUsersCheck(ownerLabel, owners)))
	},
}

var templateCreateFieldFlags = []string{"display-name", "description", "policy-id", "owner-id", "scope-type", "review-instructions"}

// buildTemplateCreateBody returns the body and whether it came from
// --body-file, so an error names the flag or body key the caller wrote.
func buildTemplateCreateBody(cmd *cobra.Command) (map[string]any, bool, error) {
	if flagGiven(cmd, "body-file") {
		for _, f := range templateCreateFieldFlags {
			if flagGiven(cmd, f) {
				return nil, true, &usageError{fmt.Errorf("--body-file is mutually exclusive with --%s", f)}
			}
		}
		body, err := readJSONFile[map[string]any](cmd, "body-file", mustGetString(cmd, "body-file"))
		if err != nil {
			return nil, true, err
		}
		v, key, err := bodyField(body, "policyId")
		if err != nil {
			return nil, true, err
		}
		if key != "" {
			if s, ok := v.(string); !ok || !hasVisibleRune(s) {
				return nil, true, &usageError{fmt.Errorf("--body-file %s must be a non-blank policy id string", key)}
			}
		}
		return body, true, nil
	}
	for _, f := range []string{"display-name", "policy-id"} {
		if !flagGiven(cmd, f) {
			return nil, false, &usageError{fmt.Errorf("--%s is required (or pass --body-file)", f)}
		}
	}
	owners, err := repeatableStringFlag(cmd, "owner-id")
	if err != nil {
		return nil, false, err
	}
	if len(owners) == 0 {
		return nil, false, &usageError{errors.New("--owner-id is required (or pass --body-file)")}
	}
	body := map[string]any{"displayName": mustGetString(cmd, "display-name"), "ownerIds": owners}
	if err := applyPolicyIDs(cmd, []policyKind{templatePolicyKind}, body); err != nil {
		return nil, false, err
	}
	for flag, key := range map[string]string{"description": "description", "review-instructions": "reviewInstructions"} {
		if v := mustGetString(cmd, flag); v != "" {
			body[key] = v
		}
	}
	if flagGiven(cmd, "scope-type") {
		v, err := templateScopeTypeFlag.parse(mustGetString(cmd, "scope-type"))
		if err != nil {
			return nil, false, err
		}
		body["scopeType"] = v
	}
	return body, false, nil
}

// templatePolicyCheck looks up a policy id from either channel; "" checks nothing.
func templatePolicyCheck(label, id string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		if id == "" {
			return nil, nil
		}
		got, err := fetchBoundPolicyType(ctx, c, label, id)
		if err != nil {
			return nil, err
		}
		if got != templatePolicyKind.wantType {
			return nil, &usageError{fmt.Errorf("--%s %q is a %s policy; it must be %s", label, id, got, templatePolicyKind.wantType)}
		}
		return nil, nil
	}}
}

var accessReviewTemplatesUpdateCmd = &cobra.Command{
	Use:   "update <template-id>",
	Short: "Update an access-review template (pretty JSON)",
	Long: `Update an access-review template's display name, description, review
instructions, review policy or scope type.

The mask is derived from the flags you pass; an omitted flag is left alone. For
any other field (schedule, recurrence, notification config, ...), pass an
AccessReviewTemplate object via --body-file plus an explicit --update-mask
(both required together, mutually exclusive with the flags above). The object
is sent with "id" set to <template-id>.

The server validates displayName and policyId on every update, whatever the
mask names, so when the request leaves either unset c1i reads the template
first and fills it in; the mask still decides what changes. A mask that names
either one needs its value in --body-file (else exit 2, nothing sent). That read makes an
unknown <template-id> exit 4 before the update is sent, --dry-run included. A
policyId is looked up only when the mask names it.

Prints the template unwrapped like "access-reviews templates get".

` + templatePolicyHelp,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, tpl, policyLabel, policyID, err := buildTemplateUpdate(cmd, args[0])
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: templatePath(args[0]), Body: body},
			withPreRead(templatePolicyCheck(policyLabel, policyID)),
			withPreRead(requiredFieldsFill(templatePath(args[0]), "accessReviewTemplate", "template", templateRequiredFields, tpl)))
	},
}

var templateUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		strUpdate("review-instructions", "reviewInstructions"),
		strUpdate("policy-id", "policyId"),
		enumUpdate("scope-type", "scopeType", &templateScopeTypeFlag),
	},
	alt:   "--body-file with --update-mask",
	scope: []string{"body-file", "update-mask"},
}

// buildTemplateUpdate returns the wrapped request, the template object inside
// it (for the fill to complete), and the policy id to look up with the channel
// it came from: only one the mask names, since the mask decides what is stored.
func buildTemplateUpdate(cmd *cobra.Command, id string) (body, tpl map[string]any, policyLabel, policyID string, err error) {
	mask, usingBody, err := bodyFileMask(cmd, templateUpdateSpec.flagNames()...)
	if err != nil {
		return nil, nil, "", "", err
	}
	tpl = map[string]any{}
	policyLabel = "policy-id"
	if usingBody {
		if tpl, err = readJSONFile[map[string]any](cmd, "body-file", mustGetString(cmd, "body-file")); err != nil {
			return nil, nil, "", "", err
		}
		for _, key := range templateRequiredFields {
			v, got, err := bodyField(tpl, key)
			if err != nil {
				return nil, nil, "", "", err
			}
			if got != "" {
				if s, ok := v.(string); !ok || !hasVisibleRune(s) {
					return nil, nil, "", "", &usageError{fmt.Errorf("--body-file %s must be a non-blank string; omit it to keep the current value", got)}
				}
			}
		}
		policyLabel = "body-file policyId"
	} else if mask, err = templateUpdateSpec.buildUpdateMask(cmd, tpl); err != nil {
		return nil, nil, "", "", err
	}
	if err := refuseMaskedFieldWithoutValue(mask, tpl, templateRequiredFields); err != nil {
		return nil, nil, "", "", err
	}
	for _, p := range strings.Split(mask, ",") {
		if foldMaskName(p) == foldMaskName("policyId") {
			policyID, _, _ = bodyFieldAs[string](tpl, "policyId")
		}
	}
	tpl["id"] = id
	return wrapUpdate("accessReviewTemplate", tpl, mask), tpl, policyLabel, policyID, nil
}

// templateRequiredFields are what the server validates on every update,
// whatever the mask names.
var templateRequiredFields = []string{"displayName", "policyId"}

var accessReviewTemplatesDeleteCmd = &cobra.Command{
	Use:   "delete <template-id>",
	Short: "Delete an access-review template",
	Long: `Delete an access-review template by ID. The API documents: "The template can
no longer be used to create campaigns." In the run checked, "templates get"
still returned the deleted template with deletedAt null, and deleting it again
exited 0. An unknown id exits 4. Prints the API's response, which is {}. Exit 0
only means the API accepted the request; in the run checked, the template
stayed readable with deletedAt null. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: templatePath(args[0])})
	},
}

var accessReviewTemplateScopeCmd = &cobra.Command{
	Use:   "scope",
	Short: "Read or replace an access-review template's scope and entitlement selection",
}

var accessReviewTemplateEntitlementsCmd = &cobra.Command{
	Use:   "entitlements",
	Short: "Add or remove entitlements in an access-review template's selection",
}

var accessReviewTemplateExcludedEntitlementsCmd = &cobra.Command{
	Use:   "excluded-entitlements",
	Short: "Add or remove entitlements in an access-review template's excluded set",
}

var accessReviewTemplateScopeGetCmd = newSetupScopeGetCmd(templateSetup)

var accessReviewTemplateScopeSetCmd = newSetupBodyCmd(templateSetup, "set",
	"Replace a template's scope and its whole entitlement selection (pretty JSON)",
	`Replace a template's scope and its whole entitlement selection from a JSON
request object:

  {"scope": {...}, "entitlements": [{"appId": "…", "appEntitlementId": "…"}]}

The API documents: "Each call replaces the whole selection." To change only some
entitlements, use "access-reviews templates entitlements add|remove". Prints
the API's response, in the shape "access-reviews templates scope get" prints.`,
	"/scope_and_entitlements", "entitlements", setupEntitlementShape, entitlementRefKind)

var accessReviewTemplateScopeSetExcludedCmd = newSetupBodyCmd(templateSetup, "set-excluded",
	"Replace a template's scope and its whole excluded-entitlement set (pretty JSON)",
	`Replace a template's scope and its whole excluded-entitlement set from a JSON
request object:

  {"scope": {..., "excludedSpecificResources": {}},
   "excludedEntitlements": [{"appId": "…", "appEntitlementId": "…"}]}

Exclusions apply only while scope sets excludedSpecificResources. The API
documents support for BY_USERS, selector/resource-type BY_ENTITLEMENTS,
BY_INHERITANCE and BY_ACCESS_CONFLICTS templates; another scope is refused with
"exclusions are not supported for this access review scope" (exit 2). To change
only some exclusions, use "access-reviews templates excluded-entitlements
add|remove". Prints the API's response, {"excludedEntitlements": [...],
"scope": {...}}.

To exclude by resource type, set scope.excludedAppSelectionCriteria here. The
API's separate scope_by_excluded_resource_type route is "superseded and
scheduled for removal", so c1i has no command for it.`,
	"/scope_and_excluded_entitlements", "excludedEntitlements", excludedEntitlementShape, entitlementRefKind)

var accessReviewTemplateScopeSetByResourceTypeCmd = newSetupBodyCmd(templateSetup, "set-by-resource-type",
	"Replace a template's scope and its resource-type selection (prints {})",
	`Replace a template's scope and its resource-type selection from a JSON request
object:

  {"scope": {"resourceTypeSelections": {}, ...},
   "resourceTypeSelections": [{"appId": "…", "resourceTypeId": "…"}]}

Prints the API's response, which is {}. In the run checked, "access-reviews
templates scope get" did not list the selected resource types back.`,
	"/scope_by_resource_type", "resourceTypeSelections", resourceTypeRefShape, resourceTypeRefKind)

var accessReviewTemplateEntitlementsAddCmd = newSetupRefsCmd(templateSetup, "add",
	"Add entitlements to a template's selection (pretty JSON)",
	`Add one application's entitlements to a template's selection without replacing
the rest. An entitlement that is already selected is skipped. The server takes 1
to 200 per call ("value must contain between 1 and 200 items, inclusive"). A
template with no scope yet gets scope.specificResources; one with another scope
is refused ("template entitlements can only be added or removed when the
template's apps and resources scope is set to specific entitlements", exit 2):
change it with "access-reviews templates scope set".
Prints the API's response, {"entitlementCount": N}: the selection's size after
the call.`,
	"/entitlements/add", "entitlements", setupEntitlementShape)

var accessReviewTemplateEntitlementsRemoveCmd = newSetupRefsCmd(templateSetup, "remove",
	"Remove entitlements from a template's selection (pretty JSON)",
	`Remove one application's entitlements from a template's selection. An
entitlement that is not selected is ignored. Like "add", it needs a template
whose scope sets specificResources. Prints the API's response,
{"entitlementCount": N, "removedCount": M}: the selection's size after the call
and how many of the given entitlements were removed.`,
	"/entitlements/remove", "entitlements", setupEntitlementShape)

var accessReviewTemplateExcludedEntitlementsAddCmd = newSetupRefsCmd(templateSetup, "add",
	"Add entitlements to a template's excluded set (prints {})",
	`Add one application's entitlements to a template's excluded set without
replacing the rest. The template's scope must set excludedSpecificResources
first, or the server answers "excluded_specific_resources must be active before
adding excluded entitlements" (exit 2); set it with "access-reviews templates
scope set-excluded". Prints the API's response, which is {}.`,
	"/excluded_entitlements/add_delete", "toAdd", excludedEntitlementShape)

var accessReviewTemplateExcludedEntitlementsRemoveCmd = newSetupRefsCmd(templateSetup, "remove",
	"Remove entitlements from a template's excluded set (prints {})",
	`Remove one application's entitlements from a template's excluded set. In the
run checked, removing the last excluded entitlement also cleared
scope.excludedSpecificResources, so a later "excluded-entitlements add" needed
"access-reviews templates scope set-excluded" first. Prints the API's
response, which is {}.`,
	"/excluded_entitlements/add_delete", "toDelete", excludedEntitlementShape)

func init() {
	c := accessReviewTemplatesCreateCmd
	c.Flags().String("display-name", "", "Display name ("+displayNameRule+") (required unless --body-file)")
	c.Flags().String("description", "", "Description")
	c.Flags().String("review-instructions", "", "Instructions shown to reviewers")
	allowEmpty(c, emptyIsOmitted, "description", "review-instructions")
	c.Flags().String("policy-id", "", "Review policy ID; must exist and be a certify policy (required unless --body-file)")
	addRepeatableStringFlag(c, "owner-id", "Owner user ID, repeatable (required unless --body-file)")
	c.Flags().String("scope-type", "", "Scope type: "+templateScopeTypeFlag.usage())
	c.Flags().String("body-file", "", "Full Create request JSON body, verbatim (file, or \"-\" for stdin); mutually exclusive with the flags above")
	markUserRefChecked(c, "owner-id")

	u := accessReviewTemplatesUpdateCmd
	u.Flags().String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	u.Flags().String("description", "", `New description; "" is sent as an empty value (omit to leave unchanged)`)
	u.Flags().String("review-instructions", "", `New reviewer instructions; "" is sent as an empty value (omit to leave unchanged)`)
	allowEmpty(u, emptyIsSent, "description", "review-instructions")
	u.Flags().String("policy-id", "", "New review policy ID; must exist and be a certify policy (omit to leave unchanged)")
	u.Flags().String("scope-type", "", "New scope type: "+templateScopeTypeFlag.usage())
	u.Flags().String("body-file", "", "AccessReviewTemplate JSON object (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the flags above")
	u.Flags().String("update-mask", "", bodyFileMaskUsage)
	setBlankHint(u, "update-mask", updateMaskBlankHint)
	ownBodyKeys(u, "body-file", argBodyKeys(0, "<template-id>", "id"))
	registerUpdateSpec(u, templateUpdateSpec)

	for _, command := range []*cobra.Command{c, u, accessReviewTemplatesDeleteCmd} {
		markMutating(command)
	}
	accessReviewTemplateScopeCmd.AddCommand(accessReviewTemplateScopeGetCmd, accessReviewTemplateScopeSetCmd, accessReviewTemplateScopeSetExcludedCmd, accessReviewTemplateScopeSetByResourceTypeCmd)
	accessReviewTemplateEntitlementsCmd.AddCommand(accessReviewTemplateEntitlementsAddCmd, accessReviewTemplateEntitlementsRemoveCmd)
	accessReviewTemplateExcludedEntitlementsCmd.AddCommand(accessReviewTemplateExcludedEntitlementsAddCmd, accessReviewTemplateExcludedEntitlementsRemoveCmd)
	accessReviewTemplatesCmd.AddCommand(accessReviewTemplatesGetCmd, accessReviewTemplatesCreateCmd, accessReviewTemplatesUpdateCmd, accessReviewTemplatesDeleteCmd,
		accessReviewTemplateScopeCmd, accessReviewTemplateEntitlementsCmd, accessReviewTemplateExcludedEntitlementsCmd)
	accessReviewsCmd.AddCommand(accessReviewTemplatesCmd)
}
