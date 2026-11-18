package cmd

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

var hooksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a hook",
	Long: `Create a hook. Without --body-file, --display-name, --event and --function-id
are required (a function hook). With --body-file, the file is the full Hook
object, sent verbatim and exclusive with the field flags: the only way to
create a built-in pattern hook. A JSON patch hook is refused by the server
with 403 when its preview is not enabled for the tenant.

--function-id and --function-commit-id are looked up first, --dry-run included:
an unknown one exits 4 and a deleted one 2 (the server answers an unknown
function with 400 "function ... not found" and stores an unknown commit id at
exit 0). A
--body-file is sent verbatim and not checked.

A hook that sets none of the type fields is refused by the server with 400
"hook function reference is missing function_id". The server does not
validate --filter-cel: it stored an uncompilable and a non-boolean expression
at exit 0.

A hook is created disabled unless --enabled is passed; an enabled hook
intercepts tool calls. Prints the hook, unwrapped like "hooks get": the id is
.id. Honors --dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildHookCreateBody(cmd)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: hooksPath, Body: body}, hookFunctionPreRead(cmd)...)
	},
}

func buildHookCreateBody(cmd *cobra.Command) (map[string]any, error) {
	if flagGiven(cmd, "body-file") {
		for _, name := range hookCreateFields.flagNames() {
			if flagGiven(cmd, name) {
				return nil, &usageError{fmt.Errorf("--body-file is mutually exclusive with the field flags (--%s)", strings.Join(hookCreateFields.flagNames(), "/--"))}
			}
		}
		path, _ := cmd.Flags().GetString("body-file")
		return readJSONFile[map[string]any](cmd, "body-file", path)
	}
	for _, name := range []string{"display-name", "event", "function-id"} {
		if !flagGiven(cmd, name) {
			return nil, &usageError{fmt.Errorf("--%s is required (or pass --body-file)", name)}
		}
	}
	body := map[string]any{}
	if _, err := hookCreateFields.apply(cmd, body); err != nil {
		return nil, err
	}
	return body, nil
}

func init() {
	hookFieldFlags(hooksCreateCmd, false)
	hooksCreateCmd.Flags().String("body-file", "", `Hook JSON object, sent verbatim (file, or "-" for stdin); mutually exclusive with the flags above`)
	markMutating(hooksCreateCmd)
	hooksCmd.AddCommand(hooksCreateCmd)
}
