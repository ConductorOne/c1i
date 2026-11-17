package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/redact"
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
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: map[string]any{key: object}})
	}}
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
	return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/findings/settings/update", Body: body})
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
var findingsShadowMCPOccurrencesSearchCmd = &cobra.Command{Use: "search", Short: "Search shadow MCP occurrences (NDJSON output)", RunE: func(cmd *cobra.Command, _ []string) error {
	body, err := readRequiredJSONObject(cmd, "body-file")
	if err != nil {
		return err
	}
	return postBodyList(cmd, "/api/v1/search/shadow_mcp_occurrences", body, shadowMCPOccurrenceRow)
}}

type listRowFn func(map[string]any) map[string]any

// postBodyList pages a POST list/search whose filter comes from --body-file;
// the paging keys are the flags' (see ownBodyKeys).
func postBodyList(cmd *cobra.Command, path string, request map[string]any, row listRowFn, idKeys ...string) error {
	return listJSONPages(cmd, "POST "+path, idKeys, row, func(c *client.Client, size int, token string) ([]byte, error) {
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
	items, next, err := extractListAndToken(data, "list")
	if err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	enc := newEmitter(cmd)
	limit := getIntFlag(cmd, "limit")
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
	// These routes don't paginate today, so a cursor isn't followed; it still
	// must not pass as a complete result.
	if next != "" {
		notice(cmd, noticeInfo, fmt.Sprintf(
			"Warning: GET %s returned a partial result — the server reported more pages, which c1i does not fetch for this command.", redact.URL(path)), nil)
	}
	return nil
}

// listJSONPages auto-paginates fetch. idKeys name the output row fields that
// together identify it for the stuck-cursor guard; none when rows have no id.
func listJSONPages(cmd *cobra.Command, route string, idKeys []string, row listRowFn, fetch func(*client.Client, int, string) ([]byte, error)) error {
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
	guard := newPaginationCursorGuard()
	if len(idKeys) > 1 {
		guard.withCompositeIDs()
	}
	for !limitReached(enc.Written(), limit) {
		if guard.tokenAlreadyUsed(token) {
			return guard.warnStuck(cmd, route)
		}
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
		rows := make([]map[string]any, len(items))
		ids := make([]string, len(items))
		for i, raw := range items {
			var item map[string]any
			if err := json.Unmarshal(raw, &item); err != nil {
				return fmt.Errorf("failed to parse response item: %w", err)
			}
			rows[i] = row(item)
			ids[i] = rowGuardID(rows[i], idKeys)
		}
		if len(idKeys) == 0 {
			if err := guard.checkIDlessPage(cmd, route, len(rows)); err != nil {
				return err
			}
		} else {
			keep, err := guard.checkPage(cmd, route, ids)
			if err != nil {
				return err
			}
			rows = keepRows(rows, keep)
		}
		for _, r := range rows {
			if err := enc.Encode(r); err != nil {
				return err
			}
			if limitReached(enc.Written(), limit) {
				return nil
			}
		}
		if next == "" || manual {
			return nil
		}
		guard.markTokenUsed(token)
		token = next
	}
	return nil
}

// rowGuardID joins a row's idKeys values with scopedRowID; "" if any is
// missing or there are none.
func rowGuardID(row map[string]any, idKeys []string) string {
	if len(idKeys) == 0 {
		return ""
	}
	parts := make([]string, len(idKeys))
	for i, k := range idKeys {
		parts[i], _ = row[k].(string)
	}
	return scopedRowID(parts...)
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
func findingRuleRow(item map[string]any) map[string]any {
	return map[string]any{
		"id": optString(item, "id"), "app_id": optString(item, "appId"), "display_name": optString(item, "displayName"), "description": optString(item, "description"), "priority": item["priority"], "enabled": boolField(item, "enabled"), "finding_type": optString(item, "findingType"),
	}
}
func findingTransformationRuleRow(item map[string]any) map[string]any {
	row := findingRuleRow(item)
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
	findingsSettingsCmd.AddCommand(findingsSettingsShowCmd, findingsSettingsUpdateCmd)
	findingsAuditsCmd.AddCommand(findingsAuditsSearchCmd)
	findingsShadowMCPOccurrencesCmd.AddCommand(findingsShadowMCPOccurrencesSearchCmd)
	findingsCmd.AddCommand(findingsSearchCmd, findingsGetCmd, findingsCreateCmd, findingsStateCmd, findingsAssigneeCmd, findingsCreateTaskCmd, findingsBulkStateCmd, findingsBulkCreateTasksCmd, findingsRoutingRulesCmd, findingsTransformationRulesCmd, findingsSettingsCmd, findingsAuditsCmd, findingsShadowMCPOccurrencesCmd)
	rootCmd.AddCommand(findingsCmd)
}
