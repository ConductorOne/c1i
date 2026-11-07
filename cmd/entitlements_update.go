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

Policies: each --<kind>-policy-id is checked before anything is sent. It must
name an existing, undeleted policy of the slot's type, or the command exits 2.
--emergency-grant-policy-id takes a grant policy. The server enforces that only
when the same call enables emergency grants, and otherwise stores any id, so
this command checks it on every bind. Unbind a slot with the repeatable
--unbind-policy grant|revoke|certify|emergency-grant; an empty policy id is a
usage error, so an unset shell variable cannot unbind one. --dry-run skips the
check.

--emergency-grant-enabled=true needs --emergency-grant-policy-id in the same
call, even when the entitlement already has one stored; the server refuses it
alone with:
  ` + emergencyPolicyRequiredMsg + `
This command refuses that at exit 2 before sending, including alongside
--unbind-policy emergency-grant. The server does NOT refuse unbinding the
policy while emergency grants stay enabled, so pass
--emergency-grant-enabled=false alongside --unbind-policy emergency-grant.

--duration-grant sets the maximum grant duration. It takes a Go duration (8h,
90m, 3600s) and is sent in seconds; 8h reads back as "28800s". It must be
positive: the server stores "0s" and "-60s" without complaint. --duration-unset
removes the limit (standing access). The server holds the two as one choice and
refuses both together, so passing both is a usage error.

A blank --display-name (empty, whitespace or invisible-only) exits 2,
nothing sent. Sent as is, the server accepts "" but restores the
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
		if err := validatePolicyBindings(cmd.Context(), cmd, c, entitlementPolicyKinds); err != nil {
			return err
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
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
	for _, s := range []struct{ flag, key string }{{"display-name", "displayName"}, {"description", "description"}} {
		if f.Changed(s.flag) {
			v, _ := f.GetString(s.flag)
			ent[s.key] = v
			paths = append(paths, s.key)
		}
	}
	policyPaths, err := applyPolicyUpdates(cmd, entitlementPolicyKinds, ent)
	if err != nil {
		return nil, err
	}
	paths = append(paths, policyPaths...)

	if f.Changed("emergency-grant-enabled") {
		enabled, _ := f.GetBool("emergency-grant-enabled")
		// Unset or unbound in this call; a stored policy does not satisfy the server.
		if v, _ := ent["emergencyGrantPolicyId"].(string); enabled && v == "" {
			return nil, &usageError{fmt.Errorf("--emergency-grant-enabled needs --emergency-grant-policy-id in the same call; the server refuses it alone with %q", emergencyPolicyRequiredMsg)}
		}
		ent["emergencyGrantEnabled"] = enabled
		paths = append(paths, "emergencyGrantEnabled")
	}

	if f.Changed("duration-grant") && f.Changed("duration-unset") {
		return nil, &usageError{fmt.Errorf("--duration-grant and --duration-unset are mutually exclusive: both set the entitlement's max_grant_duration")}
	}
	if v, ok, err := protoDurationFlag(cmd, "duration-grant", false); err != nil {
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
	f.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	f.String("description", "", "New description; an empty value clears it (omit to leave unchanged)")
	registerPolicyIDFlags(entitlementsUpdateCmd, entitlementPolicyKinds, " (omit to leave unchanged)")
	registerUnbindPolicyFlag(entitlementsUpdateCmd, entitlementPolicyKinds)
	f.Bool("emergency-grant-enabled", false, "Enable or disable emergency grants; enabling requires --emergency-grant-policy-id in the same call")
	f.String("duration-grant", "", "Maximum grant duration as a protobuf duration, e.g. 3600s; a Go duration such as 8h is converted to seconds")
	f.Bool("duration-unset", false, "Remove the maximum grant duration (standing access) instead of setting one (a protobuf duration like 3600s or a Go duration like 8h); excludes --duration-grant")
	markRequired(entitlementsUpdateCmd, "app-id")
	entitlementsCmd.AddCommand(entitlementsUpdateCmd)
}
