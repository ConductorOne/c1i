package cmd

import (
	"github.com/spf13/cobra"
)

const settingsAIGovernancePath = "/api/v1/settings/ai-governance"

var settingsAIGovernanceCmd = &cobra.Command{
	Use:   "ai-governance",
	Short: "AI governance settings (MCP client access, tool classification, discovery)",
	Long: `The tenant's AI governance settings: allowed MCP client types, default client
lifecycle, default tool classification, audit verbosity, tool auto-discovery,
and related switches. The agent classifier policy is a separate singleton:
"c1i mcp classifiers policy".`,
}

var settingsAIGovernanceShowCmd = newSettingsShowCmd("Show the tenant's AI governance settings (pretty JSON)",
	`Show the tenant's AI governance settings, unwrapped from the
aiGovernanceSettings envelope.`,
	settingsAIGovernancePath, "aiGovernanceSettings")

var auditVerbosityFlag = enumFlag{flag: "audit-verbosity", prefix: "AUDIT_VERBOSITY_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "minimal", api: "AUDIT_VERBOSITY_MINIMAL"},
	{name: "standard", api: "AUDIT_VERBOSITY_STANDARD"},
	{name: "full", api: "AUDIT_VERBOSITY_FULL"},
}}

var mcpClientTypeFlag = enumFlag{flag: "allowed-client-type", prefix: "MCP_CLIENT_TYPE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "personal", api: "MCP_CLIENT_TYPE_PERSONAL"},
	{name: "shared", api: "MCP_CLIENT_TYPE_SHARED"},
	{name: "service", api: "MCP_CLIENT_TYPE_SERVICE"},
	{name: "ephemeral", api: "MCP_CLIENT_TYPE_EPHEMERAL"},
}}

var defaultToolClassificationFlag = toolClassificationFlag.as("default-tool-classification")

// lifecycleUpdate sets one defaultClientLifecycle duration by its nested
// mask path, so the other two durations stay as they are.
func lifecycleUpdate(flag, key string) updateField {
	return hookUpdate([]string{flag}, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		v, ok, err := protoDurationFlag(cmd, flag)
		if err != nil || !ok {
			return nil, err
		}
		lc, _ := obj["defaultClientLifecycle"].(map[string]any)
		if lc == nil {
			lc = map[string]any{}
			obj["defaultClientLifecycle"] = lc
		}
		lc[key] = v
		return []string{"defaultClientLifecycle." + key}, nil
	})
}

var settingsAIGovernanceUpdateSpec = settingsUpdateSpec(
	enumUpdate("default-tool-classification", "defaultToolClassification", &defaultToolClassificationFlag),
	enumUpdate("audit-verbosity", "auditVerbosity", &auditVerbosityFlag),
	boolUpdate("auto-discovery", "autoDiscoveryEnabled"),
	durationUpdate("discovery-interval", "discoveryInterval"),
	boolUpdate("prefer-code-mode", "preferCodeModeOverDirectTools"),
	boolUpdate("surface-requestable-tools", "surfaceRequestableTools"),
	boolUpdate("untrusted-judge-disable", "untrustedJudgeDisable"),
	listUpdate("allowed-client-type", "allowedClientTypes", &mcpClientTypeFlag),
	lifecycleUpdate("inactivity-hide-after", "inactivityHideAfter"),
	lifecycleUpdate("inactivity-close-after", "inactivityCloseAfter"),
	lifecycleUpdate("retention-delete-after", "retentionDeleteAfter"),
)

var settingsAIGovernanceUpdateCmd = newSettingsUpdateCmd("Update AI governance settings (pretty JSON)",
	`Update the tenant's AI governance settings. Only the flags you pass change;
the mask is derived from them. The lifecycle flags set one
defaultClientLifecycle duration each and leave the other two as they are.

--allowed-client-type replaces the whole list (repeat it for several types).
Per the API schema, an empty list allows all types; a flag cannot send one, so
use --body-file with --update-mask allowedClientTypes.

Any other field (codeModeConcurrency, usageAttribution*, ...) goes through
--body-file with --update-mask. An unknown camelCase path is a 400, "invalid
update_mask"; a snake_case path is a 400, "FieldMask.paths contains invalid
path" (both exit 2).

Prints the updated settings as pretty JSON, unwrapped like "settings
ai-governance show".`,
	settingsAIGovernancePath, "aiGovernanceSettings", settingsAIGovernanceUpdateSpec)

var settingsAIGovernanceHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "List the AI governance settings change history (NDJSON output)",
	Long: `One NDJSON record per change to the AI governance settings, newest first as
the server returns them. Each record carries the change metadata (id, actor,
changeKind, createdAt, traceId, ...) and the full settings snapshot.

The endpoint takes no page_size or page_token and returns the whole history.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return getFixedList(cmd, settingsAIGovernancePath+"/history", func(item map[string]any) map[string]any { return item })
	},
}

var settingsAIGovernanceTenantDefaultsCmd = &cobra.Command{
	Use:   "tenant-defaults",
	Short: "Show the legacy tenant-default subset of AI governance settings (pretty JSON)",
	Long: `Show the legacy tenant-default subset of the AI governance settings
(requireToolApproval), as returned. The raw object is not projected, so an
explicit --fields is a usage error.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return getFindingsRaw(cmd, settingsAIGovernancePath+"/tenant-defaults")
	},
}

func init() {
	c := settingsAIGovernanceUpdateCmd
	f := c.Flags()
	f.String("default-tool-classification", "", "Classification for newly discovered tools: "+defaultToolClassificationFlag.usage())
	f.String("audit-verbosity", "", "Audit detail for MCP tool calls: "+auditVerbosityFlag.usage())
	f.Bool("auto-discovery", false, "Periodically re-discover tools from registered MCP servers (--auto-discovery=false to stop; omit to leave unchanged)")
	addProtoDurationFlag(c, "discovery-interval", "Auto-discovery interval "+settingsDurationForm)
	f.Bool("prefer-code-mode", false, "Set preferCodeModeOverDirectTools (omit to leave unchanged)")
	f.Bool("surface-requestable-tools", false, "Set surfaceRequestableTools (omit to leave unchanged)")
	f.Bool("untrusted-judge-disable", false, "Set untrustedJudgeDisable (omit to leave unchanged)")
	addRepeatableStringFlag(c, "allowed-client-type", "MCP client type allowed to connect (repeatable; replaces the list): "+mcpClientTypeFlag.usage())
	addProtoDurationFlag(c, "inactivity-hide-after", "Hide an MCP client after this long inactive, "+settingsDurationForm)
	addProtoDurationFlag(c, "inactivity-close-after", "Close an MCP client's access after this long inactive, "+settingsDurationForm)
	addProtoDurationFlag(c, "retention-delete-after", "Remove an MCP client's record after this long inactive, "+settingsDurationForm)

	addLimitFlag(settingsAIGovernanceHistoryCmd)
	settingsAIGovernanceCmd.AddCommand(settingsAIGovernanceShowCmd, settingsAIGovernanceUpdateCmd, settingsAIGovernanceHistoryCmd, settingsAIGovernanceTenantDefaultsCmd)
	settingsCmd.AddCommand(settingsAIGovernanceCmd)
}
