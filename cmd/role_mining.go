package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var roleMiningCmd = &cobra.Command{
	Use:   "role-mining",
	Short: "Manage role mining analyses, suggestions, and configuration",
	Long: `Manage public role mining analyses, suggestions, configuration, and generated access profiles.

Mutations and searches accept JSON request objects through --body-file so profile
filters, entitlement selections, and full configuration documents reach C1
unchanged. Searches that support public REST pagination emit NDJSON.`,
}

var roleMiningTriggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Queue an organization role mining analysis (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return postRoleMiningMutation(cmd, "/api/v1/role-mining/trigger", map[string]any{})
	},
}

var roleMiningSuggestionsCmd = &cobra.Command{Use: "suggestions", Short: "Manage role mining suggestions"}
var roleMiningSuggestionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List role mining suggestions (NDJSON output)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return getFixedList(cmd, "/api/v1/role-mining/suggestions", roleMiningSuggestionRow)
	},
}
var roleMiningSuggestionsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search role mining suggestions from JSON (NDJSON output)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postRoleMiningList(cmd, "/api/v1/search/role-mining/suggestions", body, roleMiningSuggestionRow)
	},
}
var roleMiningSuggestionsGetCmd = &cobra.Command{
	Use:   "get <suggestion-id>",
	Short: "Get a role mining suggestion (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getRoleMiningResource(cmd, client.Path("/api/v1/role-mining/suggestions/%s", args[0]), "id")
	},
}
var roleMiningSuggestionsStateCmd = &cobra.Command{
	Use:   "state <suggestion-id>",
	Short: "Update a suggestion state from JSON (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postRoleMiningMutation(cmd, client.Path("/api/v1/role-mining/suggestions/%s/state", args[0]), body)
	},
}
var roleMiningSuggestionsUsersCmd = &cobra.Command{
	Use:   "users <suggestion-id>",
	Short: "Search users in a suggestion cohort from JSON (NDJSON output)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postRoleMiningList(cmd, client.Path("/api/v1/role-mining/suggestions/%s/users", args[0]), body, roleMiningUserRow)
	},
}

var roleMiningConfigCmd = &cobra.Command{Use: "config", Short: "Inspect and replace role mining configuration"}
var roleMiningConfigShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show role mining configuration (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return getRoleMiningResource(cmd, "/api/v1/role-mining/config", "minCohortSize")
	},
}
var roleMiningConfigUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Replace role mining configuration from JSON (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postRoleMiningMutation(cmd, "/api/v1/role-mining/config", body)
	},
}

var roleMiningRunsCmd = &cobra.Command{Use: "runs", Short: "Inspect role mining analysis runs"}
var roleMiningRunsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List role mining analysis runs (NDJSON output)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return getFixedList(cmd, "/api/v1/role-mining/runs", roleMiningRunRow)
	},
}
var roleMiningRunsLatestCmd = &cobra.Command{
	Use:   "latest",
	Short: "Get the latest role mining analysis run (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return getRoleMiningResource(cmd, "/api/v1/role-mining/runs/latest", "id")
	},
}

var roleMiningCustomAnalysisCmd = &cobra.Command{Use: "custom-analysis", Short: "Run and inspect role mining analyses"}
var roleMiningCustomAnalysisListCmd = &cobra.Command{
	Use:   "list",
	Short: "List role mining custom analyses (NDJSON output)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return getFixedList(cmd, "/api/v1/role-mining/custom-analysis", roleMiningAnalysisRow)
	},
}
var roleMiningCustomAnalysisLatestCmd = &cobra.Command{
	Use:   "latest",
	Short: "Get the latest personal custom analysis (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return getRoleMiningResource(cmd, "/api/v1/role-mining/custom-analysis/latest", "id")
	},
}
var roleMiningCustomAnalysisGetCmd = &cobra.Command{
	Use:   "get <analysis-id>",
	Short: "Get a custom analysis result (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getRoleMiningResource(cmd, client.Path("/api/v1/role-mining/custom-analysis/%s", args[0]), "id")
	},
}
var roleMiningCustomAnalysisTriggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Queue a personal custom analysis from JSON (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postRoleMiningMutation(cmd, "/api/v1/role-mining/custom-analysis/trigger", body)
	},
}
var roleMiningCustomAnalysisEvaluateCmd = &cobra.Command{
	Use:   "evaluate <analysis-id>",
	Short: "Evaluate an entitlement selection from JSON (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postRoleMiningMutation(cmd, client.Path("/api/v1/role-mining/custom-analysis/%s/evaluate-entitlement-selection", args[0]), body)
	},
}

var roleMiningAccessProfilesCmd = &cobra.Command{Use: "access-profiles", Short: "Create access profiles from role mining cohorts"}
var roleMiningAccessProfilesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an access profile from JSON (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		return postRoleMiningMutation(cmd, "/api/v1/role-mining/access-profiles", body)
	},
}

func postRoleMiningList(cmd *cobra.Command, path string, request map[string]any, row listRowFn) error {
	return listJSONPages(cmd, row, func(c *client.Client, size int, token string) ([]byte, error) {
		body := cloneJSONObject(request)
		body["pageSize"] = size
		if token != "" {
			body["pageToken"] = token
		}
		return c.Post(cmd.Context(), path, body)
	})
}

func getRoleMiningResource(cmd *cobra.Command, path, key string) error {
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
	return writeResource(cmd, data, key)
}

func postRoleMiningMutation(cmd *cobra.Command, path string, body map[string]any) error {
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

func roleMiningSuggestionRow(item map[string]any) map[string]any {
	return map[string]any{
		"id": jsonString(item, "id"), "suggested_name": jsonString(item, "suggestedName"), "state": jsonString(item, "suggestionState"), "confidence": item["confidence"], "cohort_size": item["cohortSize"], "users_with_all": item["usersWithAll"], "run_id": jsonString(item, "runId"), "updated_at": item["updatedAt"],
	}
}

func roleMiningRunRow(item map[string]any) map[string]any {
	return map[string]any{
		"id": jsonString(item, "id"), "status": jsonString(item, "status"), "trigger_type": jsonString(item, "triggerType"), "total_users": item["totalUsers"], "cohorts_analyzed": item["cohortsAnalyzed"], "suggestions_generated": item["suggestionsGenerated"], "completed_at": item["completedAt"], "error_message": jsonString(item, "errorMessage"),
	}
}

func roleMiningAnalysisRow(item map[string]any) map[string]any {
	return map[string]any{
		"id": jsonString(item, "id"), "status": jsonString(item, "status"), "cohort_size": item["cohortSize"], "suggestions_generated": item["suggestionsGenerated"], "created_at": item["createdAt"], "completed_at": item["completedAt"], "error_message": jsonString(item, "errorMessage"),
	}
}

func roleMiningUserRow(item map[string]any) map[string]any {
	return map[string]any{
		"id": jsonString(item, "id"), "display_name": jsonString(item, "displayName"), "email": jsonString(item, "email"), "department": jsonString(item, "department"), "job_title": jsonString(item, "jobTitle"), "status": jsonString(item, "status"),
	}
}

func init() {
	for _, command := range []*cobra.Command{roleMiningSuggestionsSearchCmd, roleMiningSuggestionsStateCmd, roleMiningSuggestionsUsersCmd, roleMiningConfigUpdateCmd, roleMiningCustomAnalysisTriggerCmd, roleMiningCustomAnalysisEvaluateCmd, roleMiningAccessProfilesCreateCmd} {
		command.Flags().String("body-file", "", "JSON request object file (or \"-\" for stdin)")
	}
	addPaginationFlags(roleMiningSuggestionsSearchCmd)
	addPaginationFlags(roleMiningSuggestionsUsersCmd)
	addLimitFlag(roleMiningSuggestionsListCmd)
	addLimitFlag(roleMiningRunsListCmd)
	addLimitFlag(roleMiningCustomAnalysisListCmd)

	roleMiningSuggestionsCmd.AddCommand(roleMiningSuggestionsListCmd, roleMiningSuggestionsSearchCmd, roleMiningSuggestionsGetCmd, roleMiningSuggestionsStateCmd, roleMiningSuggestionsUsersCmd)
	roleMiningConfigCmd.AddCommand(roleMiningConfigShowCmd, roleMiningConfigUpdateCmd)
	roleMiningRunsCmd.AddCommand(roleMiningRunsListCmd, roleMiningRunsLatestCmd)
	roleMiningCustomAnalysisCmd.AddCommand(roleMiningCustomAnalysisListCmd, roleMiningCustomAnalysisLatestCmd, roleMiningCustomAnalysisGetCmd, roleMiningCustomAnalysisTriggerCmd, roleMiningCustomAnalysisEvaluateCmd)
	roleMiningAccessProfilesCmd.AddCommand(roleMiningAccessProfilesCreateCmd)
	roleMiningCmd.AddCommand(roleMiningTriggerCmd, roleMiningSuggestionsCmd, roleMiningConfigCmd, roleMiningRunsCmd, roleMiningCustomAnalysisCmd, roleMiningAccessProfilesCmd)
	rootCmd.AddCommand(roleMiningCmd)
}
