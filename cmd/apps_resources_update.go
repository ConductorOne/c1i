package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var appsResourcesUpdateCmd = &cobra.Command{
	Use:   "update <resource-id>",
	Short: "Update a resource's name or descriptions (pretty JSON)",
	Long: `Update a manually managed resource. The API takes {"appResource": {...},
"updateMask": "..."}; this command builds it, with the mask derived from
exactly the flags you pass. An omitted flag is left alone.

--custom-description "" clears the custom description. The server answers an
empty display name or description 200 and leaves it unchanged, so a blank
--display-name or --description exits 2 here, nothing sent.

For other fields (annotations, for one), pass an AppResource object via
--body-file plus an explicit --update-mask (both required together, mutually
exclusive with the flags above). It is sent verbatim. Mask paths are
camelCase and comma-separated; a path the server does not know is a 400,
"invalid update_mask" (exit 2). Annotations sent this way were merged into the
existing ones, not replaced.

An unknown app, resource type or resource exits 4, and a deleted app 2. The
server accepts an update to a deleted resource.

Prints the updated resource, unwrapped like "apps resources get": the id is
.id. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildResourceUpdateBody(cmd)
		if err != nil {
			return err
		}
		s := resourceScopeOf(cmd)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: s.resourcePath(args[0]), Body: body},
			deletedAppPreRead(s.appID, "--app-id"))
	},
}

var resourceUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		strUpdate("custom-description", "customDescription"),
	},
	alt:   "--body-file with --update-mask",
	scope: []string{"app-id", "resource-type-id", "body-file", "update-mask"},
}

func buildResourceUpdateBody(cmd *cobra.Command) (map[string]any, error) {
	mask, usingBody, err := bodyFileMask(cmd, resourceUpdateSpec.flagNames()...)
	if err != nil {
		return nil, err
	}
	if usingBody {
		obj, err := readJSONFile[map[string]any](cmd, "body-file", mustGetString(cmd, "body-file"))
		if err != nil {
			return nil, err
		}
		return wrapUpdate("appResource", obj, mask), nil
	}
	obj := map[string]any{}
	if mask, err = resourceUpdateSpec.buildUpdateMask(cmd, obj); err != nil {
		return nil, err
	}
	return wrapUpdate("appResource", obj, mask), nil
}

func init() {
	c := appsResourcesUpdateCmd
	f := c.Flags()
	addResourceScopeFlags(c)
	f.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	setBlankHint(c, "display-name", "the server leaves the name unchanged for an empty value")
	f.String("description", "", "New description (omit to leave unchanged; cannot be blank: the server ignores an empty one)")
	setBlankHint(c, "description", "the server leaves the description unchanged for an empty value")
	f.String("custom-description", "", `New custom description; "" clears it (omit to leave unchanged)`)
	allowEmpty(c, emptyIsSent, "custom-description")
	f.String("body-file", "", "AppResource JSON object, sent verbatim (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the flags above")
	f.String("update-mask", "", bodyFileMaskUsage)
	setBlankHint(c, "update-mask", updateMaskBlankHint)
	ownBodyKeys(c, "body-file", argBodyKeys(0, "<resource-id>", "id"))
	registerUpdateSpec(c, resourceUpdateSpec)
	markMutating(c)
	appsResourcesCmd.AddCommand(c)
}
