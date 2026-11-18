package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// Server refusals quoted in help, so each greps back to its source.
const (
	automationC1OnlyMsg   = "only the C1 app can have entitlement automation"
	automationConflictMsg = "conflicting concurrent write, retry the request"
)

// automationResponseKey is the envelope key of GetAutomation and
// UpdateAutomation (CreateAutomation uses "automation").
const automationResponseKey = "AppEntitlementAutomation"

func entitlementAutomationPath(appID, entitlementID string) string {
	return client.Path("/api/v1/apps/%s/entitlements/%s/automation", appID, entitlementID)
}

var entitlementsAutomationCmd = &cobra.Command{
	Use:   "automation",
	Short: "Manage an entitlement's membership automation",
	Long: `Manage the automation rule of an app entitlement: per the API, it
"automatically provision[s] or revoke[s] access based on defined conditions".
An entitlement has at most one.

Only entitlements of the C1 app take one (access profiles, groups and
similar); any other app's entitlement is refused with a 400 (exit 2):
  ` + automationC1OnlyMsg + `

The condition is one of:
  --cel EXPR       a CEL expression over the user, e.g.
                   'subject.department == "Engineering"'
  --refs-file F    a JSON array of {"appId","id"} entitlement references
                   (holders of any of them); the server takes 1 to 32
  --none           no condition
Each --refs-file entitlement is looked up first: the server stores an unknown
one without complaint, so c1i exits 4 instead, --dry-run included. Test a CEL
expression with "c1i policies validate-cel <condition>" first; the server
refuses a malformed one with a 400.

An automation acts on real users as soon as it exists; start with a
condition that matches the users you mean.`,
}

var entitlementsAutomationGetCmd = &cobra.Command{
	Use:   "get <entitlement-id>",
	Short: "Get an entitlement's automation (pretty JSON)",
	Long: `Get the automation of an app entitlement.

An entitlement without one exits 4 (the server answers 404), as does an
unknown entitlement. A deleted automation is still returned, at exit 0, with
deletedAt set. The condition is under exactly one of .cel, .entitlements,
.none or .basic.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), entitlementAutomationPath(appID, args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeNamedResource(cmd, data, automationResponseKey)
	},
}

var entitlementsAutomationCreateCmd = &cobra.Command{
	Use:   "create <entitlement-id>",
	Short: "Create an entitlement's automation (pretty JSON)",
	Long: `Create the automation of an app entitlement from --display-name (the
server requires 3 to 512 bytes), an optional --description and at most one
condition flag (with none, the server stores an automation without a
condition).

--body-file sends an AppEntitlementAutomation object instead, as
{"automation": <object>}; it is the only way to set the "basic" condition,
and excludes the flags above.

The server refuses a second automation on the same entitlement with a 409
(exit 2), "` + automationConflictMsg + `"; in testing it refused the same
way after the first was deleted, and retrying did not help.

Prints the created automation as pretty JSON, unwrapped like "get".

Example:
  c1i entitlements automation create <entitlement-id> --app-id <app-id> \
    --display-name "Engineering" --cel 'subject.department == "Engineering"'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		automation, refs, err := buildAutomationCreate(cmd)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: entitlementAutomationPath(appID, args[0]) + "/create",
			Body: map[string]any{"automation": automation}, Unwrap: named("automation")},
			automationPreReads(refs, "")...)
	},
}

var automationFieldFlags = []string{"display-name", "description", "cel", "refs-file", "none"}

func buildAutomationCreate(cmd *cobra.Command) (map[string]any, []appRef, error) {
	f := cmd.Flags()
	if flagGiven(cmd, "body-file") {
		for _, n := range automationFieldFlags {
			if flagGiven(cmd, n) {
				return nil, nil, &usageError{fmt.Errorf("--body-file is mutually exclusive with --%s", n)}
			}
		}
		path, _ := f.GetString("body-file")
		obj, err := readJSONFile[map[string]any](cmd, "body-file", path)
		if err != nil {
			return nil, nil, err
		}
		refs, err := bodyAutomationRefs(obj)
		return obj, refs, err
	}
	if err := requireNonEmpty(cmd, "display-name"); err != nil {
		return nil, nil, err
	}
	obj := map[string]any{"displayName": mustGetString(cmd, "display-name")}
	if flagGiven(cmd, "description") {
		obj["description"] = mustGetString(cmd, "description")
	}
	_, err := applyAutomationCondition(cmd, obj)
	if err != nil {
		return nil, nil, err
	}
	return obj, conditionRefs(obj), nil
}

// applyAutomationCondition sets the condition oneof from --cel, --refs-file
// or --none and returns its update-mask path, "conditions": the server
// ignores a mask naming the case ("cel") and applies only the oneof's name.
func applyAutomationCondition(cmd *cobra.Command, obj map[string]any) ([]string, error) {
	f := cmd.Flags()
	var set []string
	for _, n := range []string{"cel", "refs-file", "none"} {
		if flagGiven(cmd, n) {
			set = append(set, "--"+n)
		}
	}
	switch {
	case len(set) == 0:
		return nil, nil
	case len(set) > 1:
		return nil, &usageError{fmt.Errorf("%s and %s are mutually exclusive: an automation has one condition", set[0], set[1])}
	}
	switch {
	case flagGiven(cmd, "cel"):
		obj["cel"] = map[string]any{"expression": mustGetString(cmd, "cel")}
	case flagGiven(cmd, "refs-file"):
		path, _ := f.GetString("refs-file")
		raw, err := readJSONFile[[]any](cmd, "refs-file", path)
		if err != nil {
			return nil, err
		}
		refs, err := parseAppRefs(raw, "--refs-file", appEntitlementRefShape)
		if err != nil {
			return nil, err
		}
		obj["entitlements"] = map[string]any{"entitlementRefs": refs}
	default:
		if on, _ := f.GetBool("none"); !on {
			return nil, &usageError{fmt.Errorf("--none=false is not a condition; pass --cel or --refs-file instead")}
		}
		obj["none"] = map[string]any{}
	}
	return []string{"conditions"}, nil
}

// conditionRefs returns the references applyAutomationCondition set, if any.
func conditionRefs(obj map[string]any) []appRef {
	ents, _ := obj["entitlements"].(map[string]any)
	refs, _ := ents["entitlementRefs"].([]appRef)
	return refs
}

// bodyAutomationRefs parses a --body-file's entitlements.entitlementRefs, so
// each can be looked up before the send.
func bodyAutomationRefs(obj map[string]any) ([]appRef, error) {
	ents, ok, err := bodyFieldAs[map[string]any](obj, "entitlements")
	if err != nil || !ok {
		return nil, err
	}
	rawV, key, err := bodyField(ents, "entitlementRefs")
	if err != nil {
		return nil, err
	}
	raw, ok := rawV.([]any)
	if !ok {
		return nil, nil
	}
	return parseAppRefs(raw, "--body-file entitlements."+key, appEntitlementRefShape)
}

func automationRefLabels(refs []appRef) []labeledRef {
	out := make([]labeledRef, len(refs))
	for i, r := range refs {
		out[i] = labeledRef{r, fmt.Sprintf("entitlement reference %d (appId %q, id %q)", i, r.AppID, r.ID)}
	}
	return out
}

var entitlementsAutomationUpdateCmd = &cobra.Command{
	Use:   "update <entitlement-id>",
	Short: "Update an entitlement's automation (pretty JSON)",
	Long: `Update an entitlement automation's display name, description or condition.
The mask is derived from the flags you pass; an omitted flag is left alone.
A condition flag replaces the whole condition. --description "" clears it.

The server requires a display name on every update, even one that does not
change it, so without --display-name c1i first reads the automation and sends
its current name (an entitlement without one exits 4 there, --dry-run
included). Setting "basic" is not supported here; use "c1i api".

The server also updates a deleted automation at exit 0: the printed object
keeps deletedAt set, so check it before reading exit 0 as "the rule is live".

Prints the updated automation as pretty JSON, unwrapped like "get".

Example:
  c1i entitlements automation update <entitlement-id> --app-id <app-id> --none`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		path := entitlementAutomationPath(appID, args[0])
		body := map[string]any{}
		mask, err := entitlementAutomationUpdateSpec.buildUpdateMask(cmd, body)
		if err != nil {
			return err
		}
		setUpdateMask(body, mask)
		namePath := ""
		if _, ok := body["displayName"]; !ok {
			namePath = path
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path + "/update", Body: body, Unwrap: named(automationResponseKey)},
			automationPreReads(conditionRefs(body), namePath)...)
	},
}

// automationPreReads looks up each condition reference and, when namePath is
// set, reads the automation there for its current display name. With neither
// there is no read, so no client is built for a --dry-run.
func automationPreReads(refs []appRef, namePath string) []mutationOpt {
	var opts []mutationOpt
	if len(refs) > 0 {
		opts = append(opts, withPreRead(existingRefsCheck(entitlementRefKind, automationRefLabels(refs))))
	}
	if namePath != "" {
		opts = append(opts, withPreRead(currentAutomationNameRead(namePath)))
	}
	return opts
}

// currentAutomationNameRead fills the request's displayName from the stored
// automation: the server requires one on every update.
func currentAutomationNameRead(path string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
		body := m.Body.(map[string]any)
		data, err := c.Get(ctx, path, nil)
		if err != nil {
			return nil, fmt.Errorf("reading the current automation: %w", err)
		}
		var resp struct {
			Automation struct {
				DisplayName *string `json:"displayName"`
			} `json:"AppEntitlementAutomation"`
		}
		if err := json.Unmarshal(data, &resp); err != nil || resp.Automation.DisplayName == nil {
			return nil, &nonJSONResponseError{fmt.Errorf("reading the current automation: the response has no %s.displayName", automationResponseKey)}
		}
		body["displayName"] = *resp.Automation.DisplayName
		return nil, nil
	}}
}

var entitlementAutomationUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		hookUpdate([]string{"cel", "refs-file", "none"}, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
			return applyAutomationCondition(cmd, obj)
		}),
	},
	scope: []string{"app-id"},
}

var entitlementsAutomationDeleteCmd = &cobra.Command{
	Use:   "delete <entitlement-id>",
	Short: "Delete an entitlement's automation",
	Long: `Delete the automation of an app entitlement.

The delete is soft: "get" still returns the automation at exit 0 with
deletedAt set, and a repeated delete exits 0 too. The server prints {}.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: entitlementAutomationPath(appID, args[0])})
	},
}

func init() {
	for _, c := range []*cobra.Command{
		entitlementsAutomationGetCmd,
		entitlementsAutomationCreateCmd,
		entitlementsAutomationUpdateCmd,
		entitlementsAutomationDeleteCmd,
	} {
		c.Flags().String("app-id", "", "Application ID the entitlement belongs to (the C1 app)")
		markRequired(c, "app-id")
		entitlementsAutomationCmd.AddCommand(c)
	}
	for _, c := range []*cobra.Command{entitlementsAutomationCreateCmd, entitlementsAutomationUpdateCmd} {
		f := c.Flags()
		f.String("cel", "", "Condition: a CEL expression over the user (excludes the other condition flags)")
		f.String("refs-file", "", `Condition: JSON array of {"appId","id"} entitlement references (file, or "-" for stdin; excludes the other condition flags)`)
		f.Bool("none", false, "Condition: none (excludes the other condition flags)")
		markMutating(c)
	}
	entitlementsAutomationCreateCmd.Flags().String("description", "", "Description")
	entitlementsAutomationCreateCmd.Flags().String("display-name", "", "Display name ("+displayNameRule+")")
	annotateRequired(entitlementsAutomationCreateCmd, "display-name")
	entitlementsAutomationUpdateCmd.Flags().String("description", "", `New description; "" clears it (omit to leave unchanged)`)
	allowEmpty(entitlementsAutomationUpdateCmd, emptyIsSent, "description")
	entitlementsAutomationCreateCmd.Flags().String("body-file", "", `AppEntitlementAutomation JSON object, sent as {"automation": ...} (file, or "-" for stdin); excludes the other field flags`)
	entitlementsAutomationUpdateCmd.Flags().String("display-name", "", "New display name (omit to keep the current one; "+displayNameRule+")")
	registerUpdateSpec(entitlementsAutomationUpdateCmd, entitlementAutomationUpdateSpec)
	markMutating(entitlementsAutomationDeleteCmd)
	entitlementsCmd.AddCommand(entitlementsAutomationCmd)
}
