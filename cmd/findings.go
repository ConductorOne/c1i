package cmd

import (
	"encoding/json"
	"fmt"

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
auto-paginate and emit NDJSON; get and mutation commands print one JSON object.`,
}

var findingsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search findings from a JSON query (NDJSON output)",
	Long: `Search findings with an optional FindingSearchRequest JSON object.

The object may contain query, severities, states, findingTypes, appIds, target
IDs, sourceKinds, refs, assigneeIdentityUserIds, and the other public search
filters. A missing --body-file searches all accessible findings.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readOptionalJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postFindingsList(cmd, "/api/v1/findings/search", body, findingRow)
	},
}

var findingsGetCmd = &cobra.Command{
	Use: "get <finding-id>", Short: "Get a finding (pretty JSON)", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getFindingResource(cmd, client.Path("/api/v1/findings/%s", args[0]), "id")
	},
}

var findingsCreateCmd = &cobra.Command{
	Use: "create", Short: "Create a custom finding from JSON (pretty JSON)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postFindingsMutation(cmd, "/api/v1/findings", body)
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
		return postFindingsMutation(cmd, client.Path("/api/v1/findings/%s/state", args[0]), body)
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
		return postFindingsMutation(cmd, client.Path("/api/v1/findings/%s/assignee", args[0]), body)
	},
}

var findingsCreateTaskCmd = &cobra.Command{
	Use: "create-task <finding-id>", Short: "Create a remediation task for a finding (pretty JSON)", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{"findingId": args[0]}
		policyID, err := requireNonEmptyIfSet(cmd, "policy-id")
		if err != nil {
			return err
		}
		if policyID != "" {
			body["policyId"] = policyID
		}
		return postFindingsMutation(cmd, client.Path("/api/v1/findings/%s/task", args[0]), body)
	},
}

var findingsBulkStateCmd = &cobra.Command{
	Use: "bulk-state", Short: "Run an asynchronous bulk state operation from JSON (pretty JSON)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postFindingsMutation(cmd, "/api/v1/findings/bulk/state", body)
	},
}

var findingsBulkCreateTasksCmd = &cobra.Command{
	Use: "bulk-create-tasks", Short: "Create remediation tasks in bulk from JSON (pretty JSON)", Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postFindingsMutation(cmd, "/api/v1/findings/bulk/tasks", body)
	},
}

func newFindingRulesCmd(name, short, path, key string, row listRowFn) *cobra.Command {
	group := &cobra.Command{Use: name, Short: short}
	list := &cobra.Command{Use: "list", Short: "List rules (NDJSON output)", RunE: func(cmd *cobra.Command, _ []string) error {
		return getFixedList(cmd, path, row)
	}}
	addLimitFlag(list)
	get := &cobra.Command{Use: "get <rule-id>", Short: "Get a rule (pretty JSON)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return getFindingResource(cmd, client.Path(path+"/%s", args[0]), "id")
	}}
	create := &cobra.Command{Use: "create", Short: "Create a rule from JSON (pretty JSON)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		object, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postFindingsMutation(cmd, path, map[string]any{key: object})
	}}
	update := &cobra.Command{Use: "update <rule-id>", Short: "Update a rule from JSON (pretty JSON)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		object, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		object["id"] = args[0]
		return postFindingsMutation(cmd, client.Path(path+"/%s/update", args[0]), map[string]any{key: object})
	}}
	deleteCmd := &cobra.Command{Use: "delete <rule-id>", Short: "Delete a rule (pretty JSON)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return deleteFindingsMutation(cmd, client.Path(path+"/%s", args[0]))
	}}
	for _, command := range []*cobra.Command{create, update} {
		command.Flags().String("body-file", "", "JSON resource object file (or \"-\" for stdin)")
	}
	group.AddCommand(list, get, create, update, deleteCmd)
	return group
}

var findingsRoutingRulesCmd = newFindingRulesCmd("routing-rules", "Manage finding routing rules", "/api/v1/findings/routing-rules", "routingRule", findingRuleRow)
var findingsTransformationRulesCmd = newFindingRulesCmd("transformation-rules", "Manage finding transformation rules", "/api/v1/findings/transformation-rules", "transformationRule", findingTransformationRuleRow)

var findingsSettingsCmd = &cobra.Command{Use: "settings", Short: "Inspect and update finding detector settings"}
var findingsSettingsShowCmd = &cobra.Command{Use: "show", Short: "Show finding detector settings (pretty JSON)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
	return getFindingsRaw(cmd, "/api/v1/findings/settings")
}}
var findingsSettingsUpdateCmd = &cobra.Command{Use: "update", Short: "Atomically update detector settings from JSON (pretty JSON)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
	body, err := readRequiredJSONObject(cmd, "body-file")
	if err != nil {
		return err
	}
	return postFindingsMutation(cmd, "/api/v1/findings/settings/update", body)
}}

var findingsAuditsCmd = &cobra.Command{Use: "audits", Short: "Search finding audit events"}
var findingsAuditsSearchCmd = &cobra.Command{Use: "search", Short: "Search finding audit events from JSON (NDJSON output)", RunE: func(cmd *cobra.Command, _ []string) error {
	body, err := readOptionalJSONObject(cmd, "body-file")
	if err != nil {
		return err
	}
	return postFindingsList(cmd, "/api/v1/search/finding_audits", body, findingAuditRow)
}}

var findingsShadowMCPOccurrencesCmd = &cobra.Command{Use: "shadow-mcp-occurrences", Short: "Search occurrences behind shadow MCP findings"}
var findingsShadowMCPOccurrencesSearchCmd = &cobra.Command{Use: "search", Short: "Search shadow MCP occurrences (NDJSON output)", RunE: func(cmd *cobra.Command, _ []string) error {
	body, err := readRequiredJSONObject(cmd, "body-file")
	if err != nil {
		return err
	}
	return postFindingsList(cmd, "/api/v1/search/shadow_mcp_occurrences", body, shadowMCPOccurrenceRow)
}}

type listRowFn func(map[string]any) map[string]any

func postFindingsList(cmd *cobra.Command, path string, request map[string]any, row listRowFn) error {
	return listJSONPages(cmd, row, func(c *client.Client, size int, token string) ([]byte, error) {
		body := cloneJSONObject(request)
		body["pageSize"] = size
		if token != "" {
			body["pageToken"] = token
		}
		return c.Post(cmd.Context(), path, body)
	})
}

func getFixedList(cmd *cobra.Command, path string, row listRowFn) error {
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
	var response struct {
		List []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	enc := newEmitter(cmd)
	limit := getIntFlag(cmd, "limit")
	for _, raw := range response.List {
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return fmt.Errorf("failed to parse response item: %w", err)
		}
		if err := enc.Encode(row(item)); err != nil {
			return err
		}
		if limitReached(enc.Written(), limit) {
			break
		}
	}
	return nil
}

func listJSONPages(cmd *cobra.Command, row listRowFn, fetch func(*client.Client, int, string) ([]byte, error)) error {
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newListClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	token, _ := cmd.Flags().GetString("page-token")
	manual := cmd.Flags().Changed("page-token")
	requested := pageSizeFlag(cmd)
	limit := getIntFlag(cmd, "limit")
	enc := newEmitter(cmd)
	for !limitReached(enc.Written(), limit) {
		size := requested
		if !enc.Filtered() {
			size = effectivePageSize(requested, limit, enc.Written())
		}
		data, err := fetch(c, size, token)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		items, next, err := extractListAndToken(data, "list")
		if err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}
		for _, raw := range items {
			var item map[string]any
			if err := json.Unmarshal(raw, &item); err != nil {
				return fmt.Errorf("failed to parse response item: %w", err)
			}
			if err := enc.Encode(row(item)); err != nil {
				return err
			}
			if limitReached(enc.Written(), limit) {
				return nil
			}
		}
		if next == "" || manual {
			return nil
		}
		token = next
	}
	return nil
}

func getFindingResource(cmd *cobra.Command, path, idKey string) error {
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
	return writeResource(cmd, data, idKey)
}

func getFindingsRaw(cmd *cobra.Command, path string) error {
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

func postFindingsMutation(cmd *cobra.Command, path string, body map[string]any) error {
	if dryRunActive() {
		return printDryRun(cmd, "POST", path, body)
	}
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
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
}

func deleteFindingsMutation(cmd *cobra.Command, path string) error {
	if dryRunActive() {
		return printDryRun(cmd, "DELETE", path, nil)
	}
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Delete(cmd.Context(), path)
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
		"id": jsonString(item, "id"), "app_id": jsonString(item, "appId"), "severity": jsonString(item, "severity"), "state": jsonString(item, "state"), "task_id": jsonString(item, "taskId"), "assignee_identity_user_id": jsonString(item, "assigneeIdentityUserId"), "finding_type": findingType(item), "updated_at": item["updatedAt"],
	}
}
func findingRuleRow(item map[string]any) map[string]any {
	return map[string]any{
		"id": jsonString(item, "id"), "app_id": jsonString(item, "appId"), "display_name": jsonString(item, "displayName"), "description": jsonString(item, "description"), "priority": item["priority"], "enabled": findingBool(item, "enabled"), "finding_type": jsonString(item, "findingType"),
	}
}
func findingTransformationRuleRow(item map[string]any) map[string]any {
	row := findingRuleRow(item)
	row["evaluation_order"] = item["evaluationOrder"]
	row["transform_count"] = len(findingArray(item, "transforms"))
	return row
}
func findingAuditRow(item map[string]any) map[string]any {
	return map[string]any{"event_id": jsonString(item, "eventId"), "finding_id": jsonString(item, "findingId"), "event_type": jsonString(item, "eventType"), "created_at": item["createdAt"], "actor_principal_id": jsonString(item, "actorPrincipalId"), "app_id": jsonString(item, "appId"), "ticket_id": jsonString(item, "ticketId")}
}
func shadowMCPOccurrenceRow(item map[string]any) map[string]any {
	return map[string]any{"harness_kind": jsonString(item, "harnessKind"), "device_id": jsonString(item, "deviceId"), "user_id": jsonString(item, "userId"), "first_seen_at": item["firstSeenAt"], "last_seen_at": item["lastSeenAt"]}
}
func jsonString(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}
func findingBool(item map[string]any, key string) bool   { value, _ := item[key].(bool); return value }
func findingArray(item map[string]any, key string) []any { value, _ := item[key].([]any); return value }
func findingType(item map[string]any) string {
	for _, key := range []string{"similarUsernameMatch", "serviceAccountMisclassification", "nhiUnowned", "serviceAccountUnowned", "decoyCredentialUsed", "custom", "connectorAnomalyDetectionDisabled", "deactivatedOwner", "unusedSecret", "credentialPubliclyExposed", "decoyPubliclyExposed", "credentialExpiring", "connectorSyncFailing", "shadowMcp", "shadowApp"} {
		if _, ok := item[key]; ok {
			return key
		}
	}
	return ""
}

func init() {
	for _, command := range []*cobra.Command{findingsSearchCmd, findingsCreateCmd, findingsStateCmd, findingsAssigneeCmd, findingsBulkStateCmd, findingsBulkCreateTasksCmd, findingsAuditsSearchCmd, findingsShadowMCPOccurrencesSearchCmd} {
		command.Flags().String("body-file", "", "JSON request object file (or \"-\" for stdin)")
	}
	addPaginationFlags(findingsSearchCmd)
	addPaginationFlags(findingsAuditsSearchCmd)
	addPaginationFlags(findingsShadowMCPOccurrencesSearchCmd)
	findingsCreateTaskCmd.Flags().String("policy-id", "", "Optional remediation policy ID")
	findingsSettingsUpdateCmd.Flags().String("body-file", "", "JSON request object file (or \"-\" for stdin)")
	findingsSettingsCmd.AddCommand(findingsSettingsShowCmd, findingsSettingsUpdateCmd)
	findingsAuditsCmd.AddCommand(findingsAuditsSearchCmd)
	findingsShadowMCPOccurrencesCmd.AddCommand(findingsShadowMCPOccurrencesSearchCmd)
	findingsCmd.AddCommand(findingsSearchCmd, findingsGetCmd, findingsCreateCmd, findingsStateCmd, findingsAssigneeCmd, findingsCreateTaskCmd, findingsBulkStateCmd, findingsBulkCreateTasksCmd, findingsRoutingRulesCmd, findingsTransformationRulesCmd, findingsSettingsCmd, findingsAuditsCmd, findingsShadowMCPOccurrencesCmd)
	rootCmd.AddCommand(findingsCmd)
}
