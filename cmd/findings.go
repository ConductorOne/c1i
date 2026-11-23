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

var findingsCmd = &cobra.Command{
	Use:   "findings",
	Short: "Manage security findings and their governance rules",
	Long: `Manage security findings and the public API surfaces that govern them.

Finding create, state, bulk, routing-rule, transformation-rule, and settings
updates accept JSON files so oneof actions and nested rule documents reach C1
unchanged. Pass --body-file - to read JSON from standard input. Search commands
auto-paginate and emit NDJSON; get and mutation commands print one JSON object.
Mutations print the finding or rule unwrapped like get (id is .id; use jq -er).
bulk-state and bulk-create-tasks print {"bulkActionId"}, settings update prints
{"list":[...]}, and create-task prints {"finding":{...},"taskId":...}: its ids
are .taskId (the created task) and .finding.id. Deletes print {}.`,
}

var findingsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search findings from a JSON query (NDJSON output)",
	Long: `Search findings with an optional FindingSearchRequest JSON object.

The object may contain query, severities, states, findingTypes, appIds, target
IDs, sourceKinds, refs, assigneeIdentityUserIds, and the other public search
filters. A missing --body-file searches all accessible findings. Paging belongs
to --page-size and --page-token; a pageToken or pageSize in the file is refused.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readOptionalJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postBodyList(cmd, "/api/v1/findings/search", body, findingRow, "id")
	},
}

var findingsGetCmd = &cobra.Command{
	Use: "get <finding-id>", Short: "Get a finding (pretty JSON)", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getResource(cmd, client.Path("/api/v1/findings/%s", args[0]), nil, "id", "finding")
	},
}

var findingsCreateCmd = &cobra.Command{
	Use: "create", Short: "Create a custom finding from JSON (pretty JSON)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/findings", Body: body})
	},
}

var findingsStateCmd = &cobra.Command{
	Use: "state <finding-id>", Short: "Change a finding state from JSON (pretty JSON)", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		body["findingId"] = args[0]
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path("/api/v1/findings/%s/state", args[0]), Body: body})
	},
}

var findingsAssigneeCmd = &cobra.Command{
	Use: "assignee <finding-id>", Short: "Assign or clear a finding assignee from JSON (pretty JSON)", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		body["findingId"] = args[0]
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path("/api/v1/findings/%s/assignee", args[0]), Body: body})
	},
}

var findingsCreateTaskCmd = &cobra.Command{
	Use: "create-task <finding-id>", Short: "Create a remediation task for a finding (pretty JSON)", Args: cobra.ExactArgs(1),
	Long: "Create a remediation task for a finding (pretty JSON). --policy-id is optional; C1.ai otherwise selects the app policy or the built-in Finding Review policy.\n\n" + policyRefHelp,
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{"findingId": args[0]}
		var opts []mutationOpt
		if policyID, _ := cmd.Flags().GetString("policy-id"); policyID != "" {
			policyID = strings.TrimSpace(policyID)
			body["policyId"] = policyID
			// The policy type a remediation task needs is unverified: existence only.
			opts = append(opts, withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
				_, err := fetchBoundPolicyType(ctx, c, "policy-id", policyID)
				return nil, err
			}}))
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path("/api/v1/findings/%s/task", args[0]), Body: body, Unwrap: raw()}, opts...)
	},
}

var findingsBulkStateCmd = &cobra.Command{
	Use: "bulk-state", Short: "Run an asynchronous bulk state operation from JSON (pretty JSON)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/findings/bulk/state", Body: body})
	},
}

var findingsBulkCreateTasksCmd = &cobra.Command{
	Use: "bulk-create-tasks", Short: "Create remediation tasks in bulk from JSON (pretty JSON)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/findings/bulk/tasks", Body: body})
	},
}

func newFindingRulesCmd(name, short, path, key, orderField, orderLabel string, row listRowFn) *cobra.Command {
	group := &cobra.Command{Use: name, Short: short}
	list := &cobra.Command{Use: "list", Short: "List rules (NDJSON output)", RunE: func(cmd *cobra.Command, _ []string) error {
		return getFixedList(cmd, path, row)
	}}
	addLimitFlag(list)
	get := &cobra.Command{Use: "get <rule-id>", Short: "Get a rule (pretty JSON)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return getResource(cmd, client.Path(path+"/%s", args[0]), nil, "id", key)
	}}
	create := &cobra.Command{Use: "create", Short: "Create a rule from JSON (pretty JSON)", Args: cobra.NoArgs,
		Long: fmt.Sprintf(`Create a rule from a JSON object (pretty JSON). %[1]s is an exact, unique
order: creating a rule whose %[1]s collides with an existing rule's own 400s
("order N is already used by another rule") instead of making room for it.

Pass --insert-at N to put the new rule at %[1]s N instead: every existing
rule at or after N shifts one %[1]s down first, highest first so no two
rules ever collide mid-move, then the new rule is created at N.
--insert-at overrides any %[2]s already in the JSON body. A failure
part-way through names exactly which rules already shifted (nothing is
rolled back); retry with --insert-at past them, or finish the reorder by
hand with "update".`, orderLabel, orderField),
		RunE: func(cmd *cobra.Command, _ []string) error {
			object, err := readRequiredJSONObject(cmd, "body-file")
			if err != nil {
				return err
			}
			if flagGiven(cmd, "insert-at") {
				insertAt, _ := cmd.Flags().GetInt("insert-at")
				if insertAt < 1 {
					return &usageError{fmt.Errorf("--insert-at must be at least 1 (got %d): it is the new rule's 1-indexed %s, not an offset", insertAt, orderLabel)}
				}
				return sendRuleInsertSequence(cmd, path, key, orderField, insertAt, object)
			}
			return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: map[string]any{key: object}}, withErrorMap(orderCollisionHint(orderLabel)))
		}}
	create.Flags().Int("insert-at", 0, fmt.Sprintf("1-indexed %s for the new rule; shifts any colliding rule down instead of 400ing (overrides %s in the JSON body)", orderLabel, orderField))
	update := &cobra.Command{Use: "update <rule-id>", Short: "Update a rule from JSON (pretty JSON)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		object, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		object["id"] = args[0]
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path(path+"/%s/update", args[0]), Body: map[string]any{key: object}})
	}}
	deleteCmd := &cobra.Command{Use: "delete <rule-id>", Short: "Delete a rule (pretty JSON)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: client.Path(path+"/%s", args[0])})
	}}
	for _, command := range []*cobra.Command{create, update} {
		command.Flags().String("body-file", "", "JSON resource object file (or \"-\" for stdin)")
	}
	ownBodyKeys(update, "body-file", argBodyKeys(0, "<rule-id>", "id"))
	for _, command := range []*cobra.Command{create, update} {
		annotateRequired(command, "body-file")
	}
	for _, command := range []*cobra.Command{create, update, deleteCmd} {
		markMutating(command)
	}
	group.AddCommand(list, get, create, update, deleteCmd)
	return group
}

// orderCollisionHint turns the server's order-collision 400 into a pointer
// at --insert-at, which does by hand what the message otherwise demands.
func orderCollisionHint(orderLabel string) func(error) error {
	return func(err error) error {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && strings.Contains(apiErr.Body, "is already used by another rule") {
			return fmt.Errorf("API error: %w (pass --insert-at to shift the colliding rule's %s down automatically instead of freeing it by hand)", err, orderLabel)
		}
		return nil
	}
}

var findingsRoutingRulesCmd = newFindingRulesCmd("routing-rules", "Manage finding routing rules", "/api/v1/findings/routing-rules", "routingRule", "priority", "priority", findingRuleRow)
var findingsTransformationRulesCmd = newFindingRulesCmd("transformation-rules", "Manage finding transformation rules", "/api/v1/findings/transformation-rules", "transformationRule", "evaluationOrder", "evaluation order", findingTransformationRuleRow)

var findingsSettingsCmd = &cobra.Command{Use: "settings", Short: "Inspect and update finding detector settings"}
var findingsSettingsShowCmd = &cobra.Command{Use: "show", Short: "Show finding detector settings (pretty JSON)",
	Long: `Show the tenant's finding detector settings, as returned. The raw object is
not projected, so an explicit --fields is a usage error.`, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return getFindingsRaw(cmd, "/api/v1/findings/settings")
	}}
var findingsSettingsUpdateCmd = &cobra.Command{Use: "update", Short: "Atomically update detector settings from JSON (pretty JSON)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
	body, err := readRequiredJSONObject(cmd, "body-file")
	if err != nil {
		return err
	}
	return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/findings/settings/update", Body: body})
}}

var findingsSettingsEdgeCmd = &cobra.Command{Use: "edge", Short: "Inspect and update Edge finding category settings"}
var findingsSettingsEdgeShowCmd = &cobra.Command{Use: "show", Short: "Show which Edge finding categories are detected (pretty JSON)",
	Long: `Show whether each Edge finding category (list) and subcategory
(subcategories) is detected, as returned. Categories never configured read
back their shipped default. The Edge finding type's own switch ("findings
settings show") still gates every category. The raw object is not projected,
so an explicit --fields is a usage error.`, RunE: func(cmd *cobra.Command, _ []string) error {
		return getFindingsRaw(cmd, "/api/v1/findings/settings/edge")
	}}
var findingsSettingsEdgeUpdateCmd = &cobra.Command{Use: "update", Short: "Enable or disable Edge finding categories from JSON (pretty JSON)",
	Long: `Enable or disable detection for one or more Edge finding categories in a
single write. The body names only what changes, e.g.
{"settings": [{"category": "EDGE_FINDING_CATEGORY_DATA_VOLUME", "enabled": true}],
 "subcategorySettings": [{"subcategory": "...", "enabled": false}]}.
A body without a non-empty "settings" or "subcategorySettings" exits 2 and
nothing is sent. An unknown category is a 400 (exit 2).

Prints the resulting settings as pretty JSON, like "findings settings edge show".`, RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		// No mask: a body naming no category has an unmeasured effect.
		if err := requireNonEmptyBodyKey(body, "settings", "subcategorySettings"); err != nil {
			return &usageError{err}
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/findings/settings/edge/update", Body: body})
	}}

var findingsAuditsCmd = &cobra.Command{Use: "audits", Short: "Search finding audit events"}
var findingsAuditsSearchCmd = &cobra.Command{Use: "search", Short: "Search finding audit events from JSON (NDJSON output)", RunE: func(cmd *cobra.Command, _ []string) error {
	body, err := readOptionalJSONObject(cmd, "body-file")
	if err != nil {
		return err
	}
	return postBodyList(cmd, "/api/v1/search/finding_audits", body, findingAuditRow, "finding_id", "event_id")
}}

var findingsShadowMCPOccurrencesCmd = &cobra.Command{Use: "shadow-mcp-occurrences", Short: "Search occurrences behind shadow MCP findings"}
var findingsShadowMCPOccurrencesSearchCmd = &cobra.Command{Use: "search", Short: "Search shadow MCP occurrences (NDJSON output)",
	Long: `Search the occurrences behind a shadow MCP finding from a required JSON body.

The body must carry the finding's 27-character findingId (the server rejects a
body without it with a 400, "value does not match regex pattern
^[a-zA-Z0-9]{27}$"). Paging belongs to --page-size and --page-token; a pageToken
or pageSize in the body is refused (exit 2).`, RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postBodyList(cmd, "/api/v1/search/shadow_mcp_occurrences", body, shadowMCPOccurrenceRow)
	}}

func getFindingsRaw(cmd *cobra.Command, path string) error {
	if err := refuseMutationFields(cmd); err != nil {
		return err
	}
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Get(cmd.Context(), path, nil)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	return writeRawObject(cmd, data)
}

func readOptionalJSONObject(cmd *cobra.Command, flag string) (map[string]any, error) {
	file, _ := cmd.Flags().GetString(flag)
	if file == "" {
		return map[string]any{}, nil
	}
	return readRequiredJSONObject(cmd, flag)
}

func cloneJSONObject(input map[string]any) map[string]any {
	copy := make(map[string]any, len(input)+2)
	for key, value := range input {
		copy[key] = value
	}
	return copy
}

func findingRow(item map[string]any) map[string]any {
	return map[string]any{
		"id": optString(item, "id"), "app_id": optString(item, "appId"), "severity": optString(item, "severity"), "state": optString(item, "state"), "task_id": optString(item, "taskId"), "assignee_identity_user_id": optString(item, "assigneeIdentityUserId"), "finding_type": findingType(item), "updated_at": optString(item, "updatedAt"),
	}
}

// findingRuleRow is a routing-rule row. Routing rules carry a real integer
// priority; transformation rules do not (they order themselves by
// evaluationOrder), so the two rows are built from separate bases.
func findingRuleRow(item map[string]any) map[string]any {
	row := findingRuleCommonRow(item)
	row["priority"] = item["priority"]
	return row
}

// findingRuleCommonRow is the column set both rule kinds share.
func findingRuleCommonRow(item map[string]any) map[string]any {
	return map[string]any{
		"id": optString(item, "id"), "app_id": optString(item, "appId"), "display_name": optString(item, "displayName"), "description": optString(item, "description"), "enabled": boolField(item, "enabled"), "finding_type": optString(item, "findingType"),
	}
}
func findingTransformationRuleRow(item map[string]any) map[string]any {
	row := findingRuleCommonRow(item)
	row["evaluation_order"] = item["evaluationOrder"]
	row["transform_count"] = len(arrayField(item, "transforms"))
	return row
}
func findingAuditRow(item map[string]any) map[string]any {
	return map[string]any{"event_id": optString(item, "eventId"), "finding_id": optString(item, "findingId"), "event_type": optString(item, "eventType"), "created_at": optString(item, "createdAt"), "actor_principal_id": optString(item, "actorPrincipalId"), "app_id": optString(item, "appId"), "ticket_id": optString(item, "ticketId")}
}
func shadowMCPOccurrenceRow(item map[string]any) map[string]any {
	return map[string]any{"harness_kind": optString(item, "harnessKind"), "device_id": optString(item, "deviceId"), "user_id": optString(item, "userId"), "first_seen_at": optString(item, "firstSeenAt"), "last_seen_at": optString(item, "lastSeenAt")}
}
func findingType(item map[string]any) any {
	for _, key := range []string{"similarUsernameMatch", "serviceAccountMisclassification", "nhiUnowned", "serviceAccountUnowned", "decoyCredentialUsed", "custom", "connectorAnomalyDetectionDisabled", "deactivatedOwner", "unusedSecret", "credentialPubliclyExposed", "decoyPubliclyExposed", "credentialExpiring", "connectorSyncFailing", "shadowMcp", "shadowApp"} {
		if _, ok := item[key]; ok {
			return key
		}
	}
	return nil
}

func init() {
	for _, command := range []*cobra.Command{findingsSearchCmd, findingsCreateCmd, findingsStateCmd, findingsAssigneeCmd, findingsBulkStateCmd, findingsBulkCreateTasksCmd, findingsAuditsSearchCmd, findingsShadowMCPOccurrencesSearchCmd} {
		command.Flags().String("body-file", "", "JSON request object file (or \"-\" for stdin)")
	}
	for _, command := range []*cobra.Command{findingsCreateCmd, findingsStateCmd, findingsAssigneeCmd, findingsBulkStateCmd, findingsBulkCreateTasksCmd, findingsShadowMCPOccurrencesSearchCmd} {
		annotateRequired(command, "body-file")
	}
	for _, command := range []*cobra.Command{findingsSearchCmd, findingsAuditsSearchCmd, findingsShadowMCPOccurrencesSearchCmd} {
		ownBodyKeys(command, "body-file", pagingBodyKeys)
	}
	for _, command := range []*cobra.Command{findingsStateCmd, findingsAssigneeCmd} {
		ownBodyKeys(command, "body-file", argBodyKeys(0, "<finding-id>", "findingId", "finding_id"))
	}
	addPaginationFlags(findingsSearchCmd)
	addPaginationFlags(findingsAuditsSearchCmd)
	addPaginationFlags(findingsShadowMCPOccurrencesSearchCmd)
	for _, command := range []*cobra.Command{findingsCreateCmd, findingsStateCmd, findingsAssigneeCmd, findingsCreateTaskCmd, findingsBulkStateCmd, findingsBulkCreateTasksCmd, findingsSettingsUpdateCmd} {
		markMutating(command)
	}
	findingsCreateTaskCmd.Flags().String("policy-id", "", "Optional remediation policy ID")
	findingsSettingsUpdateCmd.Flags().String("body-file", "", "JSON request object file (or \"-\" for stdin)")
	annotateRequired(findingsSettingsUpdateCmd, "body-file")
	findingsSettingsEdgeUpdateCmd.Flags().String("body-file", "", "JSON request object file (or \"-\" for stdin)")
	annotateRequired(findingsSettingsEdgeUpdateCmd, "body-file")
	markMutating(findingsSettingsEdgeUpdateCmd)
	findingsSettingsEdgeCmd.AddCommand(findingsSettingsEdgeShowCmd, findingsSettingsEdgeUpdateCmd)
	findingsSettingsCmd.AddCommand(findingsSettingsShowCmd, findingsSettingsUpdateCmd, findingsSettingsEdgeCmd)
	findingsAuditsCmd.AddCommand(findingsAuditsSearchCmd)
	findingsShadowMCPOccurrencesCmd.AddCommand(findingsShadowMCPOccurrencesSearchCmd)
	findingsCmd.AddCommand(findingsSearchCmd, findingsGetCmd, findingsCreateCmd, findingsStateCmd, findingsAssigneeCmd, findingsCreateTaskCmd, findingsBulkStateCmd, findingsBulkCreateTasksCmd, findingsRoutingRulesCmd, findingsTransformationRulesCmd, findingsSettingsCmd, findingsAuditsCmd, findingsShadowMCPOccurrencesCmd)
	rootCmd.AddCommand(findingsCmd)
}
