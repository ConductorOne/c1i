package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var decoysUpdateSpec = &updateSpec{fields: []updateField{
	strUpdate("display-name", "displayName"),
	strUpdate("description", "description"),
	boolUpdate("disabled", "disabled"),
	hookUpdate([]string{"annotation"}, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		if !flagGiven(cmd, "annotation") {
			return nil, nil
		}
		pairs, err := repeatableStringFlag(cmd, "annotation")
		if err != nil {
			return nil, err
		}
		annotations, err := parseKeyValueFlag(pairs, "annotation", jsonKeyValues)
		if err != nil {
			return nil, err
		}
		obj["annotations"] = annotations
		return []string{"annotations"}, nil
	}),
}}

var decoysUpdateCmd = &cobra.Command{
	Use:   "update <decoy-id>",
	Short: "Update a decoy's display name, description, annotations, or admin-disabled state",
	Long: `Update a decoy. Only the flags you pass are sent, and each is named in the
update mask; with no field flag it exits 2.

--annotation key=value (repeatable) follows the server's own per-key PATCH
semantics on annotations: a key you pass overwrites that key, a key you omit
stays unchanged, and key= (an empty value) deletes that key.

Never changes the decoy's kind, and never returns secret material — only
"decoys rotate" vends a new one. Prints the decoy, unwrapped like "decoys
get": the id is .id. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		fields := map[string]any{}
		mask, err := decoysUpdateSpec.buildUpdateMask(cmd, fields)
		if err != nil {
			return err
		}
		fields["id"] = id
		return sendMutation(cmd, mutation{Method: http.MethodPatch, Path: decoyPath(id), Body: wrapUpdate("decoy", fields, mask)})
	},
}

func init() {
	c := decoysUpdateCmd
	f := c.Flags()
	f.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	f.String("description", "", `New description (omit to leave unchanged; "" is sent as an empty value)`)
	allowEmpty(c, emptyIsSent, "description")
	f.Bool("disabled", false, "Admin-disable (or re-enable with --disabled=false) the decoy")
	addRepeatableStringFlag(c, "annotation", decoyAnnotationFlagUsage+"; overwrites that key, key= deletes it, an omitted key is unchanged")
	registerUpdateSpec(c, decoysUpdateSpec)
	markMutating(c)
	decoysCmd.AddCommand(c)
}
