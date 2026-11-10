package cmd

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var automationsUpdateCmd = &cobra.Command{
	Use:   "update <automation-id>",
	Short: "Update an automation (pretty JSON)",
	Long: `Update an automation's display name, description, or enabled state.

The API requires {"automation": {...}, "updateMask": "..."}; this command
builds it. The mask is derived exactly from the flags you pass: an explicit
--enabled=false or an empty --description is a change, an omitted flag is
left alone. A blank --display-name (empty, whitespace or invisible-only)
exits 2, nothing sent (the server answers "" with a 500, "display_name is
required").
Updating these fields leaves steps and triggers unchanged. The server
accepts --enabled on an automation with isDraft true, and leaves isDraft as
it was.

For anything else (triggers, steps, draft fields, circuit breaker), pass an
Automation object via --body-file plus an explicit --update-mask (both
required together, mutually exclusive with the flags above). The object is
sent verbatim, with "id" set to <automation-id>. Mask paths are camelCase,
comma-separated; spaces around them are trimmed. Masking "triggers" replaces
the published triggers and increments currentVersion; isDraft and
draftTriggers stay as they were. An unknown camelCase path is a 400,
"invalid update_mask"; a snake_case path is a 400, "FieldMask.paths contains
invalid path" (both exit 2).

Prints the automation as pretty JSON, unwrapped like "automations get": the
id is .id. The response's webhookCapabilityUrl and webhookHmacSecret stay
top-level siblings.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildAutomationUpdateBody(cmd, args[0])
		if err != nil {
			return &usageError{err}
		}

		path := client.Path("/api/v1/automations/%s", args[0])
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body},
			secretBearing(secretAutomationWebhook))
	},
}

func buildAutomationUpdateBody(cmd *cobra.Command, id string) (map[string]any, error) {
	f := cmd.Flags()
	mask, usingBody, err := bodyFileMask(cmd, "display-name", "description", "enabled")
	if err != nil {
		return nil, err
	}

	var automation map[string]any
	if usingBody {
		bodyFile, _ := f.GetString("body-file")
		automation, err = readJSONFile[map[string]any](cmd, "body-file", bodyFile)
		if err != nil {
			return nil, err
		}
	} else {
		automation = map[string]any{}
		var paths []string
		if f.Changed("display-name") {
			v, err := requireNonEmptyIfSet(cmd, "display-name")
			if err != nil {
				// The server answers an empty name with a 500.
				return nil, fmt.Errorf("%w: the server rejects it with \"display_name is required\"", err)
			}
			automation["displayName"] = v
			paths = append(paths, "displayName")
		}
		if f.Changed("description") {
			v, _ := f.GetString("description")
			automation["description"] = v
			paths = append(paths, "description")
		}
		if f.Changed("enabled") {
			v, _ := f.GetBool("enabled")
			automation["enabled"] = v
			paths = append(paths, "enabled")
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("nothing to update: pass --display-name, --description, --enabled, or --body-file with --update-mask")
		}
		mask = strings.Join(paths, ",")
	}

	automation["id"] = id
	return map[string]any{"automation": automation, "updateMask": mask}, nil
}

func init() {
	f := automationsUpdateCmd.Flags()
	f.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	f.String("description", "", "New description; pass an empty value to clear it (omit to leave unchanged)")
	f.Bool("enabled", false, "Enable (--enabled) or disable (--enabled=false) the automation (omit to leave unchanged)")
	f.String("body-file", "", "Automation JSON object, sent verbatim (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the flags above")
	f.String("update-mask", "", bodyFileMaskUsage)
	markMutating(automationsUpdateCmd)
	automationsCmd.AddCommand(automationsUpdateCmd)
}
