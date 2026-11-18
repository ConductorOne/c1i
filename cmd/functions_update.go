package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var functionsUpdateCmd = &cobra.Command{
	Use:   "update <function-id>",
	Short: "Update a function's metadata, push new code, or publish a commit",
	Long: `Update a function. The update mask is derived from the flags you pass; an
omitted flag is left alone.

--file pushes a new commit (the function's new head) from local files, named
by base name as in "functions create"; it does not publish it. Publishing is
a separate call: pass the new commit's id (.commit.id in this command's
output) to --publish-commit. --commit-message names the pushed commit; without
it the server writes "Updated function content".

The server validates the function's display name on every update, even when
it is not being changed, so without --display-name c1i first reads the
function and resends its current name; that read also runs on --dry-run.

For any other field (networkConfig, outboundNetworkAllowlist,
provisionedConcurrency, scopedRoleIds, secret, ...), pass a Function object
via --body-file plus an explicit --update-mask (both required together,
mutually exclusive with --display-name, --description and --publish-commit;
--file still applies). The object is sent with "id" set to <function-id>.
Mask paths are camelCase: "published_commit_id" is a 400, "invalid path".
Secret values come back as "".

Prints the function, unwrapped like "functions get", with the pushed commit
as the sibling .commit (null without --file). Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildFunctionUpdateBody(cmd, args[0])
		if err != nil {
			return err
		}
		m := mutation{Method: http.MethodPost, Path: "/api/v1/functions/update", Body: body, Unwrap: named("function")}
		if _, ok := body["function"].(map[string]any)["displayName"]; ok {
			return sendMutation(cmd, m)
		}
		return sendMutation(cmd, m, functionDisplayNamePreRead(args[0]))
	},
}

var functionUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		strUpdate("publish-commit", "publishedCommitId"),
	},
	alt:   "--file, or --body-file with --update-mask",
	scope: []string{"body-file", "update-mask", "file", "commit-message"},
}

func buildFunctionUpdateBody(cmd *cobra.Command, id string) (map[string]any, error) {
	f := cmd.Flags()
	mask, usingBody, err := bodyFileMask(cmd, functionUpdateSpec.flagNames()...)
	if err != nil {
		return nil, err
	}
	files, err := functionFileMap(cmd)
	if err != nil {
		return nil, err
	}
	if flagGiven(cmd, "commit-message") && files == nil {
		return nil, &usageError{fmt.Errorf("--commit-message requires --file: it names the commit --file pushes")}
	}

	var function map[string]any
	switch {
	case usingBody:
		bodyFile, _ := f.GetString("body-file")
		if function, err = readJSONFile[map[string]any](cmd, "body-file", bodyFile); err != nil {
			return nil, err
		}
	case files != nil && !anyFlagGiven(cmd, functionUpdateSpec.flagNames()...):
		// A code push alone: content is independent of the mask, and the
		// server refuses an update with no mask at all.
		function = map[string]any{}
		mask = ""
	default:
		function = map[string]any{}
		if mask, err = functionUpdateSpec.buildUpdateMask(cmd, function); err != nil {
			return nil, err
		}
	}
	function["id"] = id

	body := wrapUpdate("function", function, mask)
	if files != nil {
		body["content"] = files
	}
	if flagGiven(cmd, "commit-message") {
		message, _ := f.GetString("commit-message")
		body["commitMessage"] = message
	}
	return body, nil
}

// anyFlagGiven reports whether any of names was set on the command line.
func anyFlagGiven(cmd *cobra.Command, names ...string) bool {
	for _, n := range names {
		if flagGiven(cmd, n) {
			return true
		}
	}
	return false
}

// functionDisplayNamePreRead sends the function's current display name, for a
// request that sets none: the server validates it on every update.
func functionDisplayNamePreRead(id string) mutationOpt {
	return withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
		function, _ := m.Body.(map[string]any)["function"].(map[string]any)
		data, err := c.Get(ctx, client.Path("/api/v1/functions/%s", id), nil)
		if err != nil {
			return nil, fmt.Errorf("API error reading function %s: %w", id, err)
		}
		var got struct {
			Function struct {
				DisplayName string `json:"displayName"`
			} `json:"function"`
		}
		if err := json.Unmarshal(data, &got); err != nil || got.Function.DisplayName == "" {
			return nil, &nonJSONResponseError{fmt.Errorf("reading function %s: the response held no function.displayName", id)}
		}
		function["displayName"] = got.Function.DisplayName
		return nil, nil
	}})
}

func init() {
	f := functionsUpdateCmd.Flags()
	f.String("display-name", "", "New display name (omit to leave unchanged)")
	f.String("description", "", `New description; "" is sent as an empty value (omit to leave unchanged)`)
	allowEmpty(functionsUpdateCmd, emptyIsSent, "description")
	f.String("publish-commit", "", "Commit id to publish as the function's runnable version (omit to leave unchanged)")
	addRepeatableStringFlag(functionsUpdateCmd, "file", "Local source file for a new commit, uploaded under its base name (repeatable); not published")
	f.String("commit-message", "", "Message of the pushed commit (omit for the server's default)")
	f.String("body-file", "", "Function JSON object, sent with the id set (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with --display-name, --description and --publish-commit")
	f.String("update-mask", "", bodyFileMaskUsage)
	setBlankHint(functionsUpdateCmd, "update-mask", updateMaskBlankHint)
	ownBodyKeys(functionsUpdateCmd, "body-file", argBodyKeys(0, "<function-id>", "id"))
	registerUpdateSpec(functionsUpdateCmd, functionUpdateSpec)
	markMutating(functionsUpdateCmd)
	functionsCmd.AddCommand(functionsUpdateCmd)
}
