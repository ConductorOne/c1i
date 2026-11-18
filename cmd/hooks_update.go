package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var hooksUpdateCmd = &cobra.Command{
	Use:   "update <hook-id>",
	Short: "Update a hook's fields",
	Long: `Update a hook. Only the flags you pass are sent, and each is named in the
update mask. --function-id replaces the whole function reference;
--function-commit-id needs it; both are looked up first (an unknown one exits 4
and a deleted one 2, --dry-run included), unless --body-file is used. --clear-filter removes the CEL filter (the
server clears it when the mask names "filter" and the filter is null). A
built-in pattern or JSON patch config is edited with --body-file and
--update-mask (the Hook object, and the paths it changes).

Refused with exit 2 before a request is sent: no field flag at all, a blank
--display-name, --clear-filter with --filter-cel, and a --body-file priority
outside 0-1000 when the mask names priority (create is server-validated,
update is not).

The server leaves a field alone when its path is not in the mask, and accepts
an unknown mask path at exit 0 without changing anything. Updating a deleted
hook changes it (exit 0); it stays out of "hooks list". Prints the hook,
unwrapped like "hooks get": the id is .id. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, mask, err := buildHookUpdateBody(cmd, args[0])
		if err != nil {
			return err
		}
		if hook, ok := body["hook"].(map[string]any); ok {
			if err := checkHookPriorityValue(mask, hook); err != nil {
				return err
			}
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: hookPath(args[0]), Body: body}, hookFunctionPreRead(cmd)...)
	},
}

func buildHookUpdateBody(cmd *cobra.Command, id string) (map[string]any, string, error) {
	mask, usingBody, err := bodyFileMask(cmd, hookUpdateFields.flagNames()...)
	if err != nil {
		return nil, "", err
	}
	var hook map[string]any
	if usingBody {
		path, _ := cmd.Flags().GetString("body-file")
		if hook, err = readJSONFile[map[string]any](cmd, "body-file", path); err != nil {
			return nil, "", err
		}
	} else {
		hook = map[string]any{}
		if mask, err = hookUpdateFields.buildUpdateMask(cmd, hook); err != nil {
			return nil, "", err
		}
	}
	hook["id"] = id
	return wrapUpdate("hook", hook, mask), mask, nil
}

func init() {
	hookFieldFlags(hooksUpdateCmd, true)
	f := hooksUpdateCmd.Flags()
	f.Bool("clear-filter", false, "Remove the CEL filter (mutually exclusive with --filter-cel)")
	f.String("body-file", "", `Hook JSON object, sent verbatim (file, or "-" for stdin); requires --update-mask; mutually exclusive with the flags above`)
	f.String("update-mask", "", bodyFileMaskUsage)
	setBlankHint(hooksUpdateCmd, "update-mask", updateMaskBlankHint)
	ownBodyKeys(hooksUpdateCmd, "body-file", argBodyKeys(0, "<hook-id>", "id"))
	registerUpdateSpec(hooksUpdateCmd, hookUpdateFields)
	markMutating(hooksUpdateCmd)
	hooksCmd.AddCommand(hooksUpdateCmd)
}
