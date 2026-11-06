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
left alone; fields outside the mask (steps, triggers) are kept. The server
accepts --enabled on a draft automation; isDraft is not changed by it.

For anything else (triggers, steps, draft fields, circuit breaker), pass an
Automation object via --body-file plus an explicit --update-mask (both
required together, mutually exclusive with the flags above). The object is
sent verbatim, with "id" set to <automation-id>; only masked fields change,
so e.g. masking "triggers" leaves "draftTriggers" as it was. A mask path the
server doesn't know is rejected with "invalid update_mask" (exit 2).`,
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
		bodyFile, _ := f.GetString("body-file")
		var err error
		automation, err = readConfigFile(cmd, bodyFile)
		if err != nil {
			return nil, err
		}
	} else {
		automation = map[string]any{}
		var paths []string
		if f.Changed("display-name") {
			v, _ := f.GetString("display-name")
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
	f.String("display-name", "", "New display name (omit to leave unchanged)")
	f.String("description", "", "New description; pass an empty value to clear it (omit to leave unchanged)")
	f.Bool("enabled", false, "Enable (--enabled) or disable (--enabled=false) the automation (omit to leave unchanged)")
	f.String("body-file", "", "Automation JSON object, sent verbatim (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the flags above")
	f.String("update-mask", "", "Comma-separated Automation field paths to update (required with --body-file; derived otherwise)")
	automationsCmd.AddCommand(automationsUpdateCmd)
}
