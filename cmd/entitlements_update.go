package cmd

import (
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// emergencyPolicyRequiredMsg is the server's own refusal, quoted verbatim so
// the client-side check greps back to it.
const emergencyPolicyRequiredMsg = "emergency grant policy id must be set if emergency grant is enabled"

var entitlementsUpdateCmd = &cobra.Command{
	Use:   "update <entitlement-id>",
	Short: "Update an app entitlement's name, description, policies, or grant duration (pretty JSON)",
	Long: `Update writable fields on an app entitlement.

The API takes {"entitlement": {...}, "updateMask": "..."}; this command builds
it, with the mask derived from exactly the flags you pass. An omitted flag is
left alone.

Policies: --grant-policy-id, --revoke-policy-id and --certify-policy-id must
name a policy of that type, or the server refuses with e.g.:
  grant policy id must reference a grant policy
An id that names no policy fails with a 404 (exit 4):
  Access request policy not found. Is it deleted?
Pass an empty value (--grant-policy-id "") to clear one. The server does not
check the type of --emergency-grant-policy-id.

--emergency-grant-enabled=true needs --emergency-grant-policy-id in the same
call, even when the entitlement already has one stored; the server refuses it
alone with:
  ` + emergencyPolicyRequiredMsg + `
This command refuses that combination at exit 2 before sending. The server does
NOT refuse clearing the policy while emergency grants stay enabled, so pass
--emergency-grant-enabled=false alongside --emergency-grant-policy-id "".

--duration-grant sets the maximum grant duration. It takes a Go duration (8h,
90m, 3600s) and is sent in seconds; 8h reads back as "28800s". It must be
positive: the server stores "0s" and "-60s" without complaint. --duration-unset
removes the limit (standing access). The server holds the two as one choice and
refuses both together, so passing both is a usage error.

--display-name cannot be empty: the server accepts "" but restores the
entitlement's original name instead of failing. --description "" clears it.

Prints the server's response as pretty JSON (--fields is not applied to
mutation output).

Example:
  c1i entitlements update <entitlement-id> --app-id <app-id> \
    --grant-policy-id <policy-id> --duration-grant 8h`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		id := args[0]

		body, err := buildEntitlementUpdateBody(cmd, appID, id)
		if err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/apps/%s/entitlements/%s", appID, id)
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

// entitlementUpdateStringFields maps flag -> wire key, in mask order.
var entitlementUpdateStringFields = []struct{ flag, key string }{
	{"display-name", "displayName"},
	{"description", "description"},
	{"grant-policy-id", "grantPolicyId"},
	{"revoke-policy-id", "revokePolicyId"},
	{"certify-policy-id", "certifyPolicyId"},
	{"emergency-grant-policy-id", "emergencyGrantPolicyId"},
}

// buildEntitlementUpdateBody builds the wrapped request from changed flags.
// Every error is a *usageError.
func buildEntitlementUpdateBody(cmd *cobra.Command, appID, id string) (map[string]any, error) {
	f := cmd.Flags()
	if _, err := requireNonEmptyIfSet(cmd, "display-name"); err != nil {
		return nil, err
	}

	ent := map[string]any{"id": id, "appId": appID}
	var paths []string
	for _, s := range entitlementUpdateStringFields {
		if f.Changed(s.flag) {
			v, _ := f.GetString(s.flag)
			ent[s.key] = v
			paths = append(paths, s.key)
		}
	}

	if f.Changed("emergency-grant-enabled") {
		enabled, _ := f.GetBool("emergency-grant-enabled")
		policy, _ := f.GetString("emergency-grant-policy-id")
		if enabled && policy == "" {
			return nil, &usageError{fmt.Errorf("--emergency-grant-enabled needs a non-empty --emergency-grant-policy-id in the same call; the server refuses it alone with %q", emergencyPolicyRequiredMsg)}
		}
		ent["emergencyGrantEnabled"] = enabled
		paths = append(paths, "emergencyGrantEnabled")
	}

	if f.Changed("duration-grant") && f.Changed("duration-unset") {
		return nil, &usageError{fmt.Errorf("--duration-grant and --duration-unset are mutually exclusive: both set the entitlement's max_grant_duration")}
	}
	if v, ok, err := grantDurationFlag(cmd, "duration-grant"); err != nil {
		return nil, err
	} else if ok {
		ent["durationGrant"] = v
		paths = append(paths, "durationGrant")
	}
	if f.Changed("duration-unset") {
		if v, _ := f.GetBool("duration-unset"); !v {
			return nil, &usageError{fmt.Errorf("--duration-unset=false is not a change; pass --duration-grant to set a limit")}
		}
		ent["durationUnset"] = map[string]any{}
		paths = append(paths, "durationUnset")
	}

	if len(paths) == 0 {
		return nil, &usageError{fmt.Errorf("nothing to update: pass at least one field flag (see --help)")}
	}
	return map[string]any{
		"entitlement": ent,
		"updateMask":  strings.Join(paths, ","),
	}, nil
}

func init() {
	f := entitlementsUpdateCmd.Flags()
	f.String("app-id", "", "Application ID the entitlement belongs to")
	f.String("display-name", "", "New display name (omit to leave unchanged; cannot be empty)")
	f.String("description", "", "New description; an empty value clears it (omit to leave unchanged)")
	f.String("grant-policy-id", "", "Grant policy ID; an empty value clears it (omit to leave unchanged)")
	f.String("revoke-policy-id", "", "Revoke policy ID; an empty value clears it (omit to leave unchanged)")
	f.String("certify-policy-id", "", "Certify policy ID; an empty value clears it (omit to leave unchanged)")
	f.String("emergency-grant-policy-id", "", "Emergency grant policy ID; an empty value clears it (omit to leave unchanged)")
	f.Bool("emergency-grant-enabled", false, "Enable or disable emergency grants; enabling requires --emergency-grant-policy-id in the same call")
	f.String("duration-grant", "", "Maximum grant duration as a protobuf duration, e.g. 3600s; a Go duration such as 8h is converted to seconds")
	f.Bool("duration-unset", false, "Remove the maximum grant duration (standing access) instead of setting a protobuf duration like 3600s; excludes --duration-grant")
	markRequired(entitlementsUpdateCmd, "app-id")
	entitlementsCmd.AddCommand(entitlementsUpdateCmd)
}
