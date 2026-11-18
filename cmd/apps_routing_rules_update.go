package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsRoutingRulesUpdateCmd = &cobra.Command{
	Use:   "update <rule-id>",
	Short: "Change one of an app's entitlement routing rules (pretty JSON)",
	Long: `Change an entitlement routing rule. Only the flags given change.

The API has no update mask: every update replaces the whole rule, and a
writable field the request leaves out is cleared. So c1i reads the rule first
and sends every writable field (display name, description, enabled, condition
and the whole settings object) with your flags merged in. Priority is
server-assigned on update and is preserved; change order with "reorder". A change someone else made between
that read and the write is overwritten.

--enabled=false disables the rule; --enabled makes it route real requests.
--description "" and --condition "" clear those fields (an empty condition
matches every target no earlier rule matched). A policy or schema id, and
--max-grant-duration, can be changed but not unset here.
The order is not changed here: use "apps routing-rules reorder".

c1i reads the app and the rule before sending, --dry-run included: an unknown
app or rule exits 4, and a deleted app or rule exits 2. The server checks
referenced policy and schema ids as on create.

Prints the updated rule, unwrapped like "apps routing-rules get": the id is
.id. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate the flags and refuse "nothing to update" before any request;
		// the body itself is built on the rule read below.
		if _, err := routingRuleUpdateSpec.buildUpdateMask(cmd, map[string]any{}); err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		path := routingRulePath(appID, args[0])
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Unwrap: named("routingRule")},
			liveAppPreRead(appID, "--app-id"),
			withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
				body, err := mergedRoutingRule(ctx, c, path, args[0])
				if err != nil {
					return nil, err
				}
				if _, err := routingRuleUpdateSpec.apply(cmd, body); err != nil {
					return nil, err
				}
				m.Body = body
				return nil, nil
			}}))
	},
}

// mergedRoutingRule reads the rule and returns its writable fields, the base
// a full-replace update overlays the flags on.
func mergedRoutingRule(ctx context.Context, c *client.Client, path, id string) (map[string]any, error) {
	data, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("API error reading rule %s before the update: %w", id, err)
	}
	var resp struct {
		Rule *struct {
			DisplayName string         `json:"displayName"`
			Description string         `json:"description"`
			Enabled     bool           `json:"enabled"`
			Condition   string         `json:"condition"`
			Settings    map[string]any `json:"settings"`
			DeletedAt   *string        `json:"deletedAt"`
		} `json:"routingRule"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || resp.Rule == nil {
		return nil, &nonJSONResponseError{fmt.Errorf("reading rule %s before the update: the response held no routingRule", id)}
	}
	r := resp.Rule
	if r.DeletedAt != nil {
		return nil, &usageError{fmt.Errorf("rule %q is deleted", id)}
	}
	settings := r.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	return map[string]any{
		"displayName": r.DisplayName,
		"description": r.Description,
		"enabled":     r.Enabled,
		"condition":   r.Condition,
		"settings":    settings,
	}, nil
}

var routingRuleUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		strUpdate("condition", "condition"),
		boolUpdate("enabled", "enabled"),
		hookUpdate(routingRuleSettingsFlags, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
			given, err := routingRuleSettings(cmd)
			if err != nil || len(given) == 0 {
				return nil, err
			}
			settings, _ := obj["settings"].(map[string]any)
			if settings == nil {
				settings = map[string]any{}
				obj["settings"] = settings
			}
			for k, v := range given {
				settings[k] = v
			}
			return []string{"settings"}, nil
		}),
	},
	scope: []string{"app-id"},
}

func init() {
	addRoutingRuleAppFlag(appsRoutingRulesUpdateCmd)
	f := appsRoutingRulesUpdateCmd.Flags()
	f.String("display-name", "", "New display name")
	f.String("description", "", `New description; "" clears it`)
	f.String("condition", "", `New CEL condition; "" clears it (matches every target no earlier rule matched)`)
	f.Bool("enabled", false, "Enable (routes real requests) or, with =false, disable the rule")
	addRoutingRuleSettingsFlags(appsRoutingRulesUpdateCmd)
	allowEmpty(appsRoutingRulesUpdateCmd, emptyIsSent, "description", "condition")
	registerUpdateSpec(appsRoutingRulesUpdateCmd, routingRuleUpdateSpec)
	markMutating(appsRoutingRulesUpdateCmd)
	appsRoutingRulesCmd.AddCommand(appsRoutingRulesUpdateCmd)
}
