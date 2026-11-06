package cmd

import (
	"fmt"
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
left alone. An empty or whitespace-only --display-name is refused before
sending (the server answers "" with a 500, "display_name is required").
Updating these fields leaves steps and triggers unchanged. The server accepts --enabled on an
automation with isDraft true, and leaves isDraft as it was.

For anything else (triggers, steps, draft fields, circuit breaker), pass an
Automation object via --body-file plus an explicit --update-mask (both
required together, mutually exclusive with the flags above). The object is
sent verbatim, with "id" set to <automation-id>. Mask paths are camelCase,
comma-separated; spaces around them are trimmed. Masking "triggers" replaces
the published triggers and increments currentVersion; isDraft and
draftTriggers stay as they were. An unknown camelCase path is a 400,
"invalid update_mask"; a snake_case path is a 400, "FieldMask.paths contains
invalid path" (both exit 2).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildAutomationUpdateBody(cmd, args[0])
		if err != nil {
			return &usageError{err}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/automations/%s", args[0])
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
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
	},
}

func buildAutomationUpdateBody(cmd *cobra.Command, id string) (map[string]any, error) {
	f := cmd.Flags()
	usingFlags := f.Changed("display-name") || f.Changed("description") || f.Changed("enabled")
	mask, _ := f.GetString("update-mask")
	if !f.Changed("body-file") && f.Changed("update-mask") {
		return nil, fmt.Errorf("--update-mask requires --body-file; the mask is derived from the other flags")
	}

	var automation map[string]any
	if f.Changed("body-file") {
		if usingFlags {
			return nil, fmt.Errorf("--body-file is mutually exclusive with --display-name/--description/--enabled")
		}
		if strings.TrimSpace(mask) == "" {
			return nil, fmt.Errorf("--update-mask is required together with --body-file")
		}
		// The server rejects " description" as an invalid path.
		paths := strings.Split(mask, ",")
		for i, p := range paths {
			paths[i] = strings.TrimSpace(p)
			if paths[i] == "" {
				return nil, fmt.Errorf("--update-mask %q has an empty path", mask)
			}
		}
		mask = strings.Join(paths, ",")
		bodyFile, _ := f.GetString("body-file")
		var err error
		automation, err = readConfigFile(cmd, "body-file", bodyFile)
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
	f.String("display-name", "", "New display name (omit to leave unchanged; cannot be empty or whitespace-only)")
	f.String("description", "", "New description; pass an empty value to clear it (omit to leave unchanged)")
	f.Bool("enabled", false, "Enable (--enabled) or disable (--enabled=false) the automation (omit to leave unchanged)")
	f.String("body-file", "", "Automation JSON object, sent verbatim (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the flags above")
	f.String("update-mask", "", "Comma-separated Automation field paths to update (required with --body-file; derived otherwise)")
	automationsCmd.AddCommand(automationsUpdateCmd)
}
