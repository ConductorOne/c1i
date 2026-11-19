package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsAccountProvisioningCmd = &cobra.Command{
	Use:   "account-provisioning",
	Short: "Manage an entitlement's account-provisioning policy",
	Long: `Manage provisionerPolicy on an app entitlement: the account-provisioning
configuration shown in the Console under App > Accounts management >
Provisioning. This family manages exactly the "connector.account" arm
(provisioning through this app's own connector, with CEL config fields) --
"entitlements proxy-bindings" plus "c1i docs guide
delegate-entitlement-provisioning" cover the separate "delegated" arm, which
has no first-class command.

The entitlement update endpoint takes the whole entitlement plus an
updateMask; a request that sends provisionerPolicy with that mask but omits
the account-provisioning object still answers 200 and WIPES the stored
policy. "set" always builds and sends the complete policy with
updateMask=provisionerPolicy, so it cannot send that empty body. Run "show"
first to see what is stored before changing it.`,
}

var entitlementsAccountProvisioningShowCmd = &cobra.Command{
	Use:   "show <entitlement-id>",
	Short: "Show an entitlement's account-provisioning policy (pretty JSON)",
	Long: `Show the provisionerPolicy of an app entitlement.

Prints .provisionerPolicy alone, not the whole entitlement ("entitlements
get" prints that); named "show", not "get", because the policy has no id of
its own to guarantee at the top level. An entitlement with no provisioning
configured prints "{}"; a 200 whose response does not hold the entitlement
exits 6, so a broken response cannot pass for "nothing configured".
--fields projects within this object, the same rule as every other read.`,
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
		data, err := c.Get(cmd.Context(), client.Path("/api/v1/apps/%s/entitlements/%s", appID, args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		policy, err := entitlementProvisionerPolicy(data)
		if err != nil {
			return err
		}
		return writeObject(cmd, policy)
	},
}

// entitlementProvisionerPolicy extracts .provisionerPolicy from a GET
// entitlement response. The read declares where the entitlement sits, so a
// response without it exits 6 like "entitlements get" (a broken envelope must
// not print the "{}" that means "nothing configured"). The field absent
// entirely (its proto3 zero value) prints as "{}" here; this command has not
// observed the server send an explicit {"unconfigured":{}} in that case, so
// the two are not distinguished.
func entitlementProvisionerPolicy(data []byte) ([]byte, error) {
	ent, err := resourceAt(data, "id", []string{"appEntitlementView", "appEntitlement"})
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(ent, &obj); err != nil || obj == nil {
		return nil, &nonJSONResponseError{fmt.Errorf("reading entitlement: the response held no entitlement")}
	}
	if p, ok := obj["provisionerPolicy"]; ok {
		if t := bytes.TrimSpace(p); len(t) > 0 && string(t) != "null" {
			return p, nil
		}
	}
	return []byte("{}"), nil
}

var entitlementsAccountProvisioningSetCmd = &cobra.Command{
	Use:   "set <entitlement-id>",
	Short: "Set an entitlement's connector account-provisioning policy",
	Long: `Set provisionerPolicy.connector.account on an app entitlement: provision
accounts through --connector-id, with --field key=CEL (repeatable, at least
one) as the connector's account-schema config, and a fixed schemaId "" sent
every time. Exactly one of --save-to-vault-id (repeatable) or --do-not-save
chooses whether the starter password the connector returns for a new account
is saved to a credential store (vault).

--connector-id is read first, --dry-run included: an unknown one exits 4, a
deleted one 2 (see "Soft delete" in README).

--test evaluates each --field expression with "c1i policies test-provision"
(POST .../test-account-provision-policy), in the order given, before
anything is written -- --dry-run included -- and stops at the first one the
server rejects, naming its key. The server answers a CEL expression that does
not compile with a 500 (exit 6), not a 4xx.

The request always carries the complete policy with
updateMask=provisionerPolicy (see "account-provisioning" above), so no flag
combination here can send the empty body that wipes it. Prints the updated
entitlement, unwrapped like "entitlements get" (jq -er .id); --fields is a
usage error on a mutation.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")

		config, order, err := accountProvisionFields(cmd)
		if err != nil {
			return err
		}
		vaultIDs, doNotSave, err := accountProvisionStorage(cmd)
		if err != nil {
			return err
		}
		test, _ := cmd.Flags().GetBool("test")

		account := map[string]any{
			"connectorId": connectorID,
			"schemaId":    "",
			"config":      config,
		}
		if doNotSave {
			account["doNotSave"] = map[string]any{}
		} else {
			account["saveToVault"] = map[string]any{"vaultIds": vaultIDs}
		}
		ent := map[string]any{
			"id":    id,
			"appId": appID,
			"provisionerPolicy": map[string]any{
				"connector": map[string]any{"account": account},
			},
		}
		body := wrapUpdate("entitlement", ent, "provisionerPolicy")

		opts := []mutationOpt{withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
			return nil, requireLiveConnector(ctx, c, appID, connectorID)
		}})}
		if test {
			opts = append(opts, withPreRead(accountProvisionTestPreRead(config, order)))
		}

		path := client.Path("/api/v1/apps/%s/entitlements/%s", appID, id)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body}, opts...)
	},
}

// accountProvisionFields parses --field, a repeatable key=CEL config entry,
// into config (by key) and order (insertion order, for --test's "first
// failure" report). A blank occurrence or one with no "=" is cutKeyValue's
// usage error; a repeated key is refused here, before anything is sent.
func accountProvisionFields(cmd *cobra.Command) (config map[string]string, order []string, err error) {
	pairs, err := repeatableStringFlag(cmd, "field")
	if err != nil {
		return nil, nil, err
	}
	if len(pairs) == 0 {
		return nil, nil, &usageError{fmt.Errorf("flag --field requires at least one value")}
	}
	config = make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, err := cutKeyValue("field", p)
		if err != nil {
			return nil, nil, err
		}
		if err := checkNoControl("--field", "", "key", k, false); err != nil {
			return nil, nil, err
		}
		if _, dup := config[k]; dup {
			return nil, nil, &usageError{fmt.Errorf("--field %q given more than once", k)}
		}
		config[k] = v
		order = append(order, k)
	}
	return config, order, nil
}

// accountProvisionStorage reads the exactly-one storage mode: vault ids to
// save a new account's starter password to, or --do-not-save. Neither or both given
// is a usage error.
func accountProvisionStorage(cmd *cobra.Command) (vaultIDs []string, doNotSave bool, err error) {
	vaultIDs, err = repeatableStringFlag(cmd, "save-to-vault-id")
	if err != nil {
		return nil, false, err
	}
	vaultGiven := flagGiven(cmd, "save-to-vault-id")
	doNotSaveGiven := flagGiven(cmd, "do-not-save")
	if doNotSaveGiven {
		if v, _ := cmd.Flags().GetBool("do-not-save"); !v {
			return nil, false, &usageError{fmt.Errorf("--do-not-save=false is not a change; pass --save-to-vault-id to save instead, or drop --do-not-save")}
		}
	}
	switch {
	case vaultGiven && doNotSaveGiven:
		return nil, false, &usageError{fmt.Errorf("--save-to-vault-id and --do-not-save are mutually exclusive")}
	case !vaultGiven && !doNotSaveGiven:
		return nil, false, &usageError{fmt.Errorf("exactly one of --save-to-vault-id or --do-not-save is required")}
	}
	return vaultIDs, doNotSaveGiven, nil
}

// requireLiveConnector reads a connector before storing it as an
// entitlement's provisioner: unknown exits 4, deleted exits 2 (the one
// deleted rule, cmd/deleted.go).
func requireLiveConnector(ctx context.Context, c *client.Client, appID, connectorID string) error {
	data, err := c.Get(ctx, connectorPath(appID, connectorID), nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return refNotFound("connector-id", connectorID, "connector", apiErr)
		}
		return fmt.Errorf("checking --connector-id %q: %w", connectorID, err)
	}
	deleted, _, ok := deletedAtPath(data, "connectorView", "connector")
	if !ok {
		return &nonJSONResponseError{fmt.Errorf("checking --connector-id %q: the response has no connectorView.connector", connectorID)}
	}
	if deleted {
		return &usageError{fmt.Errorf("--connector-id %q: the connector is deleted", connectorID)}
	}
	return nil
}

// accountProvisionTestPreRead evaluates every field's CEL through "policies
// test-provision"'s own route, in flag order, before anything is written
// (--dry-run too); it stops at the first expression the server rejects,
// naming the field.
func accountProvisionTestPreRead(config map[string]string, order []string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		for _, k := range order {
			if _, err := c.Post(ctx, policiesTestProvisionPath, map[string]any{"cel": config[k]}); err != nil {
				return nil, fmt.Errorf("--field %s: testing CEL: %w", k, err)
			}
		}
		return nil, nil
	}}
}

func init() {
	entitlementsAccountProvisioningShowCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	markRequired(entitlementsAccountProvisioningShowCmd, "app-id")

	f := entitlementsAccountProvisioningSetCmd.Flags()
	f.String("app-id", "", "Application ID the entitlement belongs to")
	f.String("connector-id", "", "Connector that provisions the account; must be a live connector of --app-id")
	addRepeatableStringFlag(entitlementsAccountProvisioningSetCmd, "field", "Account-schema config field as key=CEL, repeatable")
	addRepeatableStringFlag(entitlementsAccountProvisioningSetCmd, "save-to-vault-id", "Vault (credential store) ID to save a new account's starter password to, repeatable (required unless --do-not-save)")
	f.Bool("do-not-save", false, "Don't save a new account's starter password to a vault (required unless --save-to-vault-id)")
	f.Bool("test", false, "Evaluate every --field expression with the account-provision policy tester before writing; abort on the first one the server rejects")
	markRequired(entitlementsAccountProvisioningSetCmd, "app-id", "connector-id", "field")
	markMutating(entitlementsAccountProvisioningSetCmd)

	entitlementsAccountProvisioningCmd.AddCommand(entitlementsAccountProvisioningShowCmd, entitlementsAccountProvisioningSetCmd)
	entitlementsCmd.AddCommand(entitlementsAccountProvisioningCmd)
}
