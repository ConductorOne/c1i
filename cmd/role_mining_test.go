package cmd

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestRoleMiningMutationsDryRun(t *testing.T) {
	body := func(name, content string) string {
		t.Helper()
		path := t.TempDir() + "/" + name
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}
	state := body("state.json", `{"state":"SUGGESTION_STATE_DISMISSED"}`)
	config := body("config.json", `{"minCohortSize":10}`)
	analysis := body("analysis.json", `{"profileFilters":[{"attribute":"department","values":["Engineering"]}]}`)
	evaluation := body("evaluation.json", `{"minimumCoverageBasisPoints":8000}`)
	profile := body("profile.json", `{"displayName":"Engineering","profileFilters":[{"attribute":"department","values":["Engineering"]}]}`)

	cases := []struct {
		name     string
		cmd      *cobra.Command
		args     []string
		bodyFile string
		want     []string
	}{
		{"trigger", roleMiningTriggerCmd, nil, "", []string{"[dry-run] POST /api/v1/role-mining/trigger", "{}"}},
		{"suggestion state", roleMiningSuggestionsStateCmd, []string{"suggestion/1"}, state, []string{"[dry-run] POST /api/v1/role-mining/suggestions/suggestion%2F1/state", `"state": "SUGGESTION_STATE_DISMISSED"`}},
		{"config update", roleMiningConfigUpdateCmd, nil, config, []string{"[dry-run] POST /api/v1/role-mining/config", `"minCohortSize": 10`}},
		{"custom analysis trigger", roleMiningCustomAnalysisTriggerCmd, nil, analysis, []string{"[dry-run] POST /api/v1/role-mining/custom-analysis/trigger", `"profileFilters"`}},
		{"selection evaluation", roleMiningCustomAnalysisEvaluateCmd, []string{"analysis/1"}, evaluation, []string{"[dry-run] POST /api/v1/role-mining/custom-analysis/analysis%2F1/evaluate-entitlement-selection", `"minimumCoverageBasisPoints": 8000`}},
		{"access profile create", roleMiningAccessProfilesCreateCmd, nil, profile, []string{"[dry-run] POST /api/v1/role-mining/access-profiles", `"displayName": "Engineering"`}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resetCmdFlags(t, tt.cmd)
			originalDryRun := viper.GetBool("dry_run")
			viper.Set("dry_run", true)
			t.Cleanup(func() { viper.Set("dry_run", originalDryRun) })
			t.Setenv("C1I_URL", "https://example.invalid")
			if tt.bodyFile != "" {
				if err := tt.cmd.Flags().Set("body-file", tt.bodyFile); err != nil {
					t.Fatalf("setting --body-file: %v", err)
				}
			}
			var out bytes.Buffer
			tt.cmd.SetOut(&out)
			t.Cleanup(func() { tt.cmd.SetOut(nil) })
			tt.cmd.SetContext(context.Background())
			if err := tt.cmd.RunE(tt.cmd, tt.args); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q does not contain %q", out.String(), want)
				}
			}
		})
	}
}
