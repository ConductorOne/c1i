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

func writeAccessReviewJSON(t *testing.T, name, content string) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

func enableAccessReviewDryRun(t *testing.T) {
	t.Helper()
	original := viper.GetBool("dry_run")
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", original) })
}

func TestAccessReviewMutationsDryRun(t *testing.T) {
	createFile := writeAccessReviewJSON(t, "create.json", `{"displayName":"Q4 review","ownerIds":["user-1"]}`)
	updateFile := writeAccessReviewJSON(t, "update.json", `{"description":"updated"}`)

	cases := []struct {
		name     string
		cmd      *cobra.Command
		args     []string
		setFlags func(t *testing.T)
		contains []string
	}{
		{
			name: "create", cmd: accessReviewsCreateCmd,
			setFlags: func(t *testing.T) { setAccessReviewFlag(t, accessReviewsCreateCmd, "body-file", createFile) },
			contains: []string{"[dry-run] POST /api/v1/access_review", `"ownerIds"`},
		},
		{
			name: "update", cmd: accessReviewsUpdateCmd, args: []string{"review/1"},
			setFlags: func(t *testing.T) {
				setAccessReviewFlag(t, accessReviewsUpdateCmd, "body-file", updateFile)
				setAccessReviewFlag(t, accessReviewsUpdateCmd, "update-mask", "description")
			},
			contains: []string{"[dry-run] POST /api/v1/access_review/review%2F1", `"accessReview"`, `"id": "review/1"`, `"updateMask": "description"`},
		},
		{
			name: "generate CSV report", cmd: accessReviewReportsGenerateCmd, args: []string{"review-1"},
			setFlags: func(t *testing.T) { setAccessReviewFlag(t, accessReviewReportsGenerateCmd, "format", "csv") },
			contains: []string{"[dry-run] POST /api/v1/access_review/review-1/report", `"format": "ACCESS_REVIEW_REPORT_FORMAT_CSV"`},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resetCmdFlags(t, tt.cmd)
			enableAccessReviewDryRun(t)
			t.Setenv("C1I_URL", "https://example.invalid")
			tt.setFlags(t)
			var out bytes.Buffer
			tt.cmd.SetOut(&out)
			t.Cleanup(func() { tt.cmd.SetOut(nil) })
			tt.cmd.SetContext(context.Background())
			if err := tt.cmd.RunE(tt.cmd, tt.args); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q does not contain %q", out.String(), want)
				}
			}
		})
	}
}

func TestAccessReviewCreateRejectsMissingOwners(t *testing.T) {
	resetCmdFlags(t, accessReviewsCreateCmd)
	enableAccessReviewDryRun(t)
	setAccessReviewFlag(t, accessReviewsCreateCmd, "body-file", writeAccessReviewJSON(t, "create.json", `{"displayName":"Q4 review"}`))
	accessReviewsCreateCmd.SetContext(context.Background())
	err := accessReviewsCreateCmd.RunE(accessReviewsCreateCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "ownerIds") {
		t.Errorf("error = %v, want an ownerIds usage error", err)
	}
}

func setAccessReviewFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("setting --%s: %v", name, err)
	}
}
