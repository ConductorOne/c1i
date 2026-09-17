package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpClassifiersCmd = &cobra.Command{
	Use:   "classifiers",
	Short: "Manage MCP gateway classifiers",
	Long: `Manage the AI-governance classifier surfaces used with the MCP gateway.

A classifier is a reusable ordered rule cascade. Bind it to an enforcement
surface with "bindings create"; rules may reference tool gates. Templates
instantiate curated classifiers and their dependent gates. The singleton
"policy" surface is separate from named classifiers.

Create and update commands take JSON objects through --body-file (or "-" for
standard input), preserving nested CEL rules, hooks, gates, and template
parameters without a lossy flag translation. Updates require --update-mask as
an explicit safeguard: including "rules" replaces the entire ordered cascade.`,
}

var mcpClassifiersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List classifiers (NDJSON output)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return listClassifierPages(cmd, "classifiers", classifierRow, func(c *client.Client, pageSize int, pageToken string) ([]byte, error) {
			params := pageParams(pageSize, pageToken)
			return c.Get(cmd.Context(), "/api/v1/classifiers", params)
		})
	},
}

var mcpClassifiersGetCmd = &cobra.Command{
	Use:   "get <classifier-id>",
	Short: "Get a classifier (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getClassifierResource(cmd, client.Path("/api/v1/classifiers/%s", args[0]), "classifier")
	},
}

var mcpClassifiersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a classifier from a JSON object (pretty JSON)",
	Long: `Create a named, reusable classifier.

Pass the Classifier object (not the CreateClassifierRequest wrapper) with
--body-file. displayName is required by C1. Rules are evaluated in listed order,
first match wins; leave a new rule's id empty for C1 to assign it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		classifier, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postClassifierMutation(cmd, "/api/v1/classifiers", map[string]any{"classifier": classifier})
	},
}

var mcpClassifiersUpdateCmd = &cobra.Command{
	Use:   "update <classifier-id>",
	Short: "Update selected classifier fields from JSON (pretty JSON)",
	Long: `Update a classifier with a complete Classifier JSON object and an explicit
--update-mask. C1 requires displayName even when it is not in the mask. The
positional id overrides any id in the file. Use camelCase proto field paths;
for example, --update-mask description,defaultOutcome.

Including rules in --update-mask replaces every rule in evaluation order. Read
and preserve the existing classifier first when changing rules.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readClassifierUpdateBody(cmd, args[0])
		if err != nil {
			return err
		}
		return postClassifierMutation(cmd, client.Path("/api/v1/classifiers/%s", args[0]), body)
	},
}

var mcpClassifiersDeleteCmd = &cobra.Command{
	Use:   "delete <classifier-id>",
	Short: "Delete a classifier (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return postClassifierMutation(cmd, client.Path("/api/v1/classifiers/%s/delete", args[0]), map[string]any{})
	},
}

var mcpClassifierBindingsCmd = &cobra.Command{
	Use:   "bindings",
	Short: "Manage classifier-to-enforcement-surface bindings",
}

var mcpClassifierBindingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List classifier bindings (NDJSON output)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return listClassifierPages(cmd, "bindings", classifierBindingRow, func(c *client.Client, pageSize int, pageToken string) ([]byte, error) {
			return c.Get(cmd.Context(), "/api/v1/classifier_bindings", pageParams(pageSize, pageToken))
		})
	},
}

var mcpClassifierBindingsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Bind a classifier from a JSON object (pretty JSON)",
	Long: `Attach a classifier to an enforcement surface.

Pass the ClassifierBinding object (not the CreateClassifierBindingRequest
wrapper) through --body-file. surface and classifierId are required. targetId
identifies the applicable agent or gateway target when the selected surface
uses one.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		binding, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postClassifierMutation(cmd, "/api/v1/classifier_bindings", map[string]any{"binding": binding})
	},
}

var mcpClassifierBindingsDeleteCmd = &cobra.Command{
	Use:   "delete <binding-id>",
	Short: "Delete a classifier binding (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return postClassifierMutation(cmd, client.Path("/api/v1/classifier_bindings/%s/delete", args[0]), map[string]any{})
	},
}

var mcpClassifierTemplatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Inspect and instantiate classifier templates",
}

var mcpClassifierTemplatesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List classifier templates (NDJSON output)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		latestOnly, _ := cmd.Flags().GetBool("latest-only")
		return listClassifierPages(cmd, "templates", classifierTemplateRow, func(c *client.Client, pageSize int, pageToken string) ([]byte, error) {
			body := map[string]any{"pageSize": pageSize}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}
			if latestOnly {
				body["latestOnly"] = true
			}
			return c.Post(cmd.Context(), "/api/v1/classifier_templates/list", body)
		})
	},
}

var mcpClassifierTemplatesGetCmd = &cobra.Command{
	Use:   "get <template-id>",
	Short: "Get a classifier template revision (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]string{}
		if version, _ := cmd.Flags().GetString("template-version"); version != "" {
			params["template_version"] = version
		}
		return getClassifierResourceWithParams(cmd, client.Path("/api/v1/classifier_templates/%s", args[0]), params, "template")
	},
}

var mcpClassifierTemplatesInstantiateCmd = &cobra.Command{
	Use:   "instantiate <template-id>",
	Short: "Instantiate a classifier template (pretty JSON)",
	Long: `Instantiate a template into a tenant-owned, editable classifier.

--body-file is optional. When supplied, pass optional request fields:
templateVersion, displayName, and params. Required params are declared by
"templates get" and validated by C1.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readOptionalClassifierJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postClassifierMutation(cmd, client.Path("/api/v1/classifier_templates/%s/instantiate", args[0]), body)
	},
}

var mcpClassifierTemplatesAddRuleCmd = &cobra.Command{
	Use:   "add-rule <classifier-id> <template-id>",
	Short: "Add one template rule to a classifier (pretty JSON)",
	Long: `Insert one rule from a template into a classifier.

--body-file is optional. It may contain templateVersion, ruleIndex,
insertIndex, and params. The positional classifier and template IDs are
canonical. ruleIndex defaults to 0; insertIndex at or past the end appends.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readOptionalClassifierJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		body["templateId"] = args[1]
		return postClassifierMutation(cmd, client.Path("/api/v1/classifiers/%s/rules/from_template", args[0]), body)
	},
}

var mcpClassifierPolicyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Inspect and update the singleton agent classifier policy",
}

var mcpClassifierPolicyShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the singleton agent classifier policy (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return getClassifierResource(cmd, "/api/v1/settings/ai-governance/agent-classifier-policy", "policy")
	},
}

var mcpClassifierPolicyUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update selected singleton policy fields from JSON (pretty JSON)",
	Long: `Update the tenant's singleton agent classifier policy using a partial
AgentClassifierPolicy JSON object and --update-mask. The policy is implicitly
created by its first update. Including rules in the mask replaces the entire
first-match-wins cascade in the supplied order.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readMaskedClassifierObject(cmd, "policy", "")
		if err != nil {
			return err
		}
		return postClassifierMutation(cmd, "/api/v1/settings/ai-governance/agent-classifier-policy", body)
	},
}

var mcpClassifierToolGatesCmd = &cobra.Command{
	Use:   "tool-gates",
	Short: "Manage classifier tool gates",
}

var mcpClassifierToolGatesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tool gates (NDJSON output)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return listClassifierPages(cmd, "list", toolGateRow, func(c *client.Client, pageSize int, pageToken string) ([]byte, error) {
			return c.Get(cmd.Context(), "/api/v1/tool_gates", pageParams(pageSize, pageToken))
		})
	},
}

var mcpClassifierToolGatesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search tool gates (NDJSON output)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		query, _ := cmd.Flags().GetString("query")
		return listClassifierPages(cmd, "list", toolGateRow, func(c *client.Client, pageSize int, pageToken string) ([]byte, error) {
			body := map[string]any{"query": query, "pageSize": pageSize}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}
			return c.Post(cmd.Context(), "/api/v1/search/tool_gates", body)
		})
	},
}

var mcpClassifierToolGatesGetCmd = &cobra.Command{
	Use:   "get <tool-gate-id>",
	Short: "Get a tool gate (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getClassifierResource(cmd, client.Path("/api/v1/tool_gates/%s", args[0]), "toolGate")
	},
}

var mcpClassifierToolGatesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a tool gate from JSON (pretty JSON)",
	Long: `Create a tool gate from the full ToolGatesServiceCreateRequest JSON object.

displayName and grantPolicyId are required. filter may set a CEL expression or
a built-in pattern, but never both; C1 resolves a built-in pattern to its CEL
expression.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postClassifierMutation(cmd, "/api/v1/tool_gates", body)
	},
}

var mcpClassifierToolGatesUpdateCmd = &cobra.Command{
	Use:   "update <tool-gate-id>",
	Short: "Update selected tool-gate fields from JSON (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readMaskedClassifierObject(cmd, "toolGate", args[0])
		if err != nil {
			return err
		}
		return postClassifierMutation(cmd, client.Path("/api/v1/tool_gates/%s", args[0]), body)
	},
}

var mcpClassifierToolGatesDeleteCmd = &cobra.Command{
	Use:   "delete <tool-gate-id>",
	Short: "Delete a tool gate (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return postClassifierMutation(cmd, client.Path("/api/v1/tool_gates/%s/delete", args[0]), map[string]any{})
	},
}

type classifierPageFetcher func(c *client.Client, pageSize int, pageToken string) ([]byte, error)
type classifierRowFn func(item map[string]any) map[string]any

func listClassifierPages(cmd *cobra.Command, listKey string, row classifierRowFn, fetch classifierPageFetcher) error {
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newListClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	pageToken, _ := cmd.Flags().GetString("page-token")
	manualPaging := cmd.Flags().Changed("page-token")
	requestedPageSize := pageSizeFlag(cmd)
	limit := getIntFlag(cmd, "limit")
	enc := newEmitter(cmd)
	for !limitReached(enc.Written(), limit) {
		pageSize := requestedPageSize
		if !enc.Filtered() {
			pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
		}
		data, err := fetch(c, pageSize, pageToken)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		items, nextPageToken, err := extractListAndToken(data, listKey)
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
		if nextPageToken == "" || manualPaging {
			return nil
		}
		pageToken = nextPageToken
	}
	return nil
}

func pageParams(pageSize int, pageToken string) map[string]string {
	params := map[string]string{"page_size": strconv.Itoa(pageSize)}
	if pageToken != "" {
		params["page_token"] = pageToken
	}
	return params
}

func getClassifierResource(cmd *cobra.Command, path, key string) error {
	return getClassifierResourceWithParams(cmd, path, nil, key)
}

func getClassifierResourceWithParams(cmd *cobra.Command, path string, params map[string]string, key string) error {
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Get(cmd.Context(), path, params)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	return writeNamedResource(cmd, data, key)
}

func postClassifierMutation(cmd *cobra.Command, path string, body map[string]any) error {
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

func readOptionalClassifierJSONObject(cmd *cobra.Command, flag string) (map[string]any, error) {
	file, _ := cmd.Flags().GetString(flag)
	if file == "" {
		return map[string]any{}, nil
	}
	return readRequiredJSONObject(cmd, flag)
}

func readClassifierUpdateBody(cmd *cobra.Command, id string) (map[string]any, error) {
	body, err := readMaskedClassifierObject(cmd, "classifier", id)
	if err != nil {
		return nil, err
	}
	classifier := body["classifier"].(map[string]any)
	if _, ok := classifier["displayName"].(string); !ok {
		return nil, &usageError{fmt.Errorf("classifier update body must include displayName")}
	}
	return body, nil
}

func readMaskedClassifierObject(cmd *cobra.Command, field, id string) (map[string]any, error) {
	object, err := readRequiredJSONObject(cmd, "body-file")
	if err != nil {
		return nil, err
	}
	mask, _ := cmd.Flags().GetString("update-mask")
	if strings.TrimSpace(mask) == "" {
		return nil, &usageError{fmt.Errorf("--update-mask is required")}
	}
	if id != "" {
		object["id"] = id
	}
	return map[string]any{field: object, "updateMask": mask}, nil
}

func classifierRow(item map[string]any) map[string]any {
	return map[string]any{
		"id":               stringField(item, "id"),
		"display_name":     stringField(item, "displayName"),
		"description":      stringField(item, "description"),
		"applicable_type":  stringField(item, "applicableType"),
		"managed_by":       stringField(item, "managedBy"),
		"default_outcome":  stringField(item, "defaultOutcome"),
		"rule_count":       len(arrayField(item, "rules")),
		"template_id":      nestedStringField(item, "templateRef", "templateId"),
		"template_version": nestedStringField(item, "templateRef", "version"),
		"deleted_at":       nilIfBlank(item["deletedAt"]),
	}
}

func classifierBindingRow(item map[string]any) map[string]any {
	return map[string]any{
		"id":            stringField(item, "id"),
		"classifier_id": stringField(item, "classifierId"),
		"surface":       stringField(item, "surface"),
		"target_id":     stringField(item, "targetId"),
		"deleted_at":    nilIfBlank(item["deletedAt"]),
	}
}

func classifierTemplateRow(item map[string]any) map[string]any {
	return map[string]any{
		"id":                   stringField(item, "id"),
		"template_version":     stringField(item, "templateVersion"),
		"display_name":         stringField(item, "displayName"),
		"applicable_type":      stringField(item, "applicableType"),
		"distribution":         stringField(item, "distribution"),
		"managed_by":           stringField(item, "managedBy"),
		"preset_tier":          stringField(item, "presetTier"),
		"is_latest":            boolField(item, "isLatest"),
		"rule_count":           len(arrayField(item, "rules")),
		"required_param_count": len(arrayField(item, "requiredParams")),
		"deleted_at":           nilIfBlank(item["deletedAt"]),
	}
}

func toolGateRow(item map[string]any) map[string]any {
	return map[string]any{
		"id":               stringField(item, "id"),
		"display_name":     stringField(item, "displayName"),
		"description":      stringField(item, "description"),
		"priority":         item["priority"],
		"enabled":          boolField(item, "enabled"),
		"grant_policy_id":  stringField(item, "grantPolicyId"),
		"built_in_pattern": nestedStringField(item, "filter", "builtInPattern"),
		"deleted_at":       nilIfBlank(item["deletedAt"]),
	}
}

func stringField(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}

func nestedStringField(item map[string]any, parent, key string) string {
	child, _ := item[parent].(map[string]any)
	return stringField(child, key)
}

func arrayField(item map[string]any, key string) []any {
	value, _ := item[key].([]any)
	return value
}

func boolField(item map[string]any, key string) bool {
	value, _ := item[key].(bool)
	return value
}

func nilIfBlank(value any) any {
	if value == nil || value == "" {
		return nil
	}
	return value
}

func init() {
	addPaginationFlags(mcpClassifiersListCmd)
	addPaginationFlags(mcpClassifierBindingsListCmd)
	mcpClassifierTemplatesListCmd.Flags().Bool("latest-only", false, "Return only the latest revision of each template")
	addPaginationFlags(mcpClassifierTemplatesListCmd)
	mcpClassifierTemplatesGetCmd.Flags().String("template-version", "", "Template version (default: latest)")
	addPaginationFlags(mcpClassifierToolGatesListCmd)
	mcpClassifierToolGatesSearchCmd.Flags().String("query", "", "Text to search for (default: all tool gates)")
	addPaginationFlags(mcpClassifierToolGatesSearchCmd)

	for _, command := range []*cobra.Command{
		mcpClassifiersCreateCmd,
		mcpClassifiersUpdateCmd,
		mcpClassifierBindingsCreateCmd,
		mcpClassifierTemplatesInstantiateCmd,
		mcpClassifierTemplatesAddRuleCmd,
		mcpClassifierPolicyUpdateCmd,
		mcpClassifierToolGatesCreateCmd,
		mcpClassifierToolGatesUpdateCmd,
	} {
		command.Flags().String("body-file", "", "JSON object file (or \"-\" for stdin)")
	}
	for _, command := range []*cobra.Command{mcpClassifiersUpdateCmd, mcpClassifierPolicyUpdateCmd, mcpClassifierToolGatesUpdateCmd} {
		command.Flags().String("update-mask", "", "Comma-separated camelCase fields to update (required)")
	}

	mcpClassifiersCmd.AddCommand(mcpClassifiersListCmd, mcpClassifiersGetCmd, mcpClassifiersCreateCmd, mcpClassifiersUpdateCmd, mcpClassifiersDeleteCmd)
	mcpClassifierBindingsCmd.AddCommand(mcpClassifierBindingsListCmd, mcpClassifierBindingsCreateCmd, mcpClassifierBindingsDeleteCmd)
	mcpClassifierTemplatesCmd.AddCommand(mcpClassifierTemplatesListCmd, mcpClassifierTemplatesGetCmd, mcpClassifierTemplatesInstantiateCmd, mcpClassifierTemplatesAddRuleCmd)
	mcpClassifierPolicyCmd.AddCommand(mcpClassifierPolicyShowCmd, mcpClassifierPolicyUpdateCmd)
	mcpClassifierToolGatesCmd.AddCommand(mcpClassifierToolGatesListCmd, mcpClassifierToolGatesSearchCmd, mcpClassifierToolGatesGetCmd, mcpClassifierToolGatesCreateCmd, mcpClassifierToolGatesUpdateCmd, mcpClassifierToolGatesDeleteCmd)
	mcpClassifiersCmd.AddCommand(mcpClassifierBindingsCmd, mcpClassifierTemplatesCmd, mcpClassifierPolicyCmd, mcpClassifierToolGatesCmd)
	mcpCmd.AddCommand(mcpClassifiersCmd)
}
