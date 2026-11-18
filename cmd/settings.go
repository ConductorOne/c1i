package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Inspect and update tenant settings",
	Long: `Inspect and update tenant-wide settings: AI governance, session security,
SSO provider, cross-app access, security/billing/operations contacts, access
requests, verified domains, notifications, and the AWS external id.

Each area is a singleton: "show" prints it as pretty JSON, unwrapped from its
envelope; "update" prints the updated object the same way. Masked updates take
field flags, or the object "show" prints via --body-file plus an explicit
--update-mask (camelCase paths; only the masked fields change).`,
}

// settingsUpdateSpec completes spec with the --body-file/--update-mask path
// every masked settings update shares.
func settingsUpdateSpec(fields ...updateField) *updateSpec {
	return &updateSpec{fields: fields, alt: "--body-file with --update-mask", scope: []string{"body-file", "update-mask"}}
}

// newSettingsShowCmd reads a singleton at path and prints its key unwrapped.
func newSettingsShowCmd(short, long, path, key string) *cobra.Command {
	return &cobra.Command{Use: "show", Short: short, Long: long, RunE: func(cmd *cobra.Command, _ []string) error {
		return getSingleton(cmd, path, key)
	}}
}

// newSettingsUpdateCmd is a masked singleton update: POST {key: obj,
// updateMask} to path, printed unwrapped like show. The caller registers
// spec's field flags.
func newSettingsUpdateCmd(short, long, path, key string, spec *updateSpec) *cobra.Command {
	c := &cobra.Command{Use: "update", Short: short, Long: long, RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := settingsUpdateBody(cmd, key, spec)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body, Unwrap: named(key)})
	}}
	f := c.Flags()
	f.String("body-file", "", "Settings JSON object as \"show\" prints it, sent verbatim (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the field flags")
	f.String("update-mask", "", bodyFileMaskUsage)
	setBlankHint(c, "update-mask", updateMaskBlankHint)
	registerUpdateSpec(c, spec)
	markMutating(c)
	return c
}

func settingsUpdateBody(cmd *cobra.Command, key string, spec *updateSpec) (map[string]any, error) {
	mask, usingBody, err := bodyFileMask(cmd, spec.flagNames()...)
	if err != nil {
		return nil, err
	}
	var obj map[string]any
	if usingBody {
		bodyFile, _ := cmd.Flags().GetString("body-file")
		if obj, err = readJSONFile[map[string]any](cmd, "body-file", bodyFile); err != nil {
			return nil, err
		}
	} else {
		obj = map[string]any{}
		if mask, err = spec.buildUpdateMask(cmd, obj); err != nil {
			return nil, err
		}
	}
	return wrapUpdate(key, obj, mask), nil
}

// newSettingsBodyUpdateCmd is an update with no mask: --body-file is the whole
// request, sent verbatim once check accepts it. With no mask, a body that
// names nothing has an unmeasured effect (a mask-less ai-governance update
// reset every field), so check refuses it before any request.
func newSettingsBodyUpdateCmd(short, long, path string, unwrap unwrapRule, check func(map[string]any) error) *cobra.Command {
	c := &cobra.Command{Use: "update", Short: short, Long: long, RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		if err := check(body); err != nil {
			return &usageError{err}
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body, Unwrap: unwrap})
	}}
	c.Flags().String("body-file", "", "JSON request object file (or \"-\" for stdin)")
	annotateRequired(c, "body-file")
	markMutating(c)
	return c
}

// newSettingsHistoryCmd streams a settings history as raw NDJSON records
// (metadata + snapshot), following the cursor.
func newSettingsHistoryCmd(short, long, path string) *cobra.Command {
	c := &cobra.Command{Use: "history", Short: short, Long: long, RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		// Rows carry no id of their own, so only the token and idle-page checks apply.
		return listPages(cmd, c, pageSpec[json.RawMessage]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[json.RawMessage],
		})
	}}
	addPaginationFlags(c)
	return c
}

// settingsDurationForm ends every settings duration flag's help.
const settingsDurationForm = "as a protobuf duration, e.g. 3600s, or a Go duration such as 8h (omit to leave unchanged)"

// requireNonEmptyBodyKey refuses a body whose keys are all missing, null, or
// an empty object or array, and one that sets any of them in both spellings.
func requireNonEmptyBodyKey(body map[string]any, keys ...string) error {
	set := false
	for _, k := range keys {
		raw, _, err := bodyField(body, k)
		if err != nil {
			return err
		}
		switch v := raw.(type) {
		case map[string]any:
			set = set || len(v) > 0
		case []any:
			set = set || len(v) > 0
		}
	}
	if set {
		return nil
	}
	return fmt.Errorf("--body-file must set %s to a non-empty value; the request has no update mask, so nothing is sent without one", strings.Join(keys, " or "))
}

const settingsContactsPath = "/api/v1/settings/contacts"

var settingsContactsCmd = &cobra.Command{Use: "contacts", Short: "Security, billing and operations contact emails"}

var settingsContactsShowCmd = newSettingsShowCmd("Show the tenant's contact email lists (pretty JSON)",
	`Show the organization's security, billing, and operations contact email
addresses (securityEmails, billingEmails, operationsEmails).`,
	settingsContactsPath, "contacts")

var settingsContactsUpdateSpec = settingsUpdateSpec(
	listUpdate("security-email", "securityEmails", nil),
	listUpdate("billing-email", "billingEmails", nil),
	listUpdate("operations-email", "operationsEmails", nil),
)

var settingsContactsUpdateCmd = newSettingsUpdateCmd("Replace contact email lists (pretty JSON)",
	`Replace one or more contact email lists. Each list flag given replaces that
whole list (repeat the flag for several addresses); lists you don't name are
left as they are. The server checks each address ("value must be a valid
email address", exit 2).

To empty a list, use --body-file with an object that omits it, e.g. {} with
--update-mask securityEmails.

Prints the updated contacts as pretty JSON, unwrapped like "settings contacts
show".`,
	settingsContactsPath, "contacts", settingsContactsUpdateSpec)

const settingsRequestPath = "/api/v1/settings/request"

var settingsRequestCmd = &cobra.Command{Use: "request", Short: "Access-request settings"}

var settingsRequestShowCmd = newSettingsShowCmd("Show the tenant's access-request settings (pretty JSON)",
	`Show the tenant's access-request settings: skipJustification and
maxBulkEntitlementSelection.`,
	settingsRequestPath, "requestSettings")

var settingsRequestUpdateSpec = settingsUpdateSpec(
	boolUpdate("skip-justification", "skipJustification"),
	intUpdate("max-bulk-entitlement-selection", "maxBulkEntitlementSelection"),
)

var settingsRequestUpdateCmd = newSettingsUpdateCmd("Update access-request settings (pretty JSON)",
	`Update the tenant's access-request settings. Only the flags you pass change.
The server bounds --max-bulk-entitlement-selection ("value must be inside
range [0, 50]", exit 2); 0 reads back as the server default 10.

Prints the updated settings as pretty JSON, unwrapped like "settings request
show".`,
	settingsRequestPath, "requestSettings", settingsRequestUpdateSpec)

var settingsAWSExternalIDCmd = &cobra.Command{Use: "aws-external-id", Short: "The tenant's AWS external id"}

var settingsAWSExternalIDShowCmd = newSettingsShowCmd("Show the tenant's AWS external id (pretty JSON)",
	`Show the AWS external ID for the tenant, used in IAM role trust policies for
AWS connectors. Prints {"externalId": "..."}.`,
	"/api/v1/settings/aws-external-id", "awsExternalId")

func init() {
	addRepeatableStringFlag(settingsContactsUpdateCmd, "security-email", "Security contact email (repeatable; replaces the list)")
	addRepeatableStringFlag(settingsContactsUpdateCmd, "billing-email", "Billing contact email (repeatable; replaces the list)")
	addRepeatableStringFlag(settingsContactsUpdateCmd, "operations-email", "Operations contact email (repeatable; replaces the list)")
	settingsContactsCmd.AddCommand(settingsContactsShowCmd, settingsContactsUpdateCmd)

	rf := settingsRequestUpdateCmd.Flags()
	rf.Bool("skip-justification", false, "Let requesters skip the justification (--skip-justification=false to require it; omit to leave unchanged)")
	rf.Int("max-bulk-entitlement-selection", 0, "Most entitlements one bulk request may select (omit to leave unchanged)")
	settingsRequestCmd.AddCommand(settingsRequestShowCmd, settingsRequestUpdateCmd)

	settingsAWSExternalIDCmd.AddCommand(settingsAWSExternalIDShowCmd)
	settingsCmd.AddCommand(settingsContactsCmd, settingsRequestCmd, settingsAWSExternalIDCmd)
	rootCmd.AddCommand(settingsCmd)
}
