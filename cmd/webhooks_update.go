package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var webhooksUpdateCmd = &cobra.Command{
	Use:   "update <webhook-id>",
	Short: "Update a webhook's display name, URL, description or callback timeout",
	Long: `Update a webhook. Only the flags you pass are sent, and each is named in the
update mask; an empty --description clears it.

Refused with exit 2 before a request is sent:
  - no field flag at all
  - a blank --display-name (empty, whitespace or invisible-only; the server
    would store it on update)
  - a --destination-url that is not https, has no host, or uses an IPv6
    literal host

Updating a soft-deleted webhook exits 0 and changes it; it stays deleted.
Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		fields, paths, err := webhookFields(cmd)
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			return &usageError{fmt.Errorf("nothing to update: pass --display-name, --destination-url, --description or --callback-timeout")}
		}
		fields["id"] = id

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body := map[string]any{
			"webhook":    fields,
			"updateMask": strings.Join(paths, ","),
		}
		path := webhookPath(id)
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

func init() {
	webhookFieldFlags(webhooksUpdateCmd, true)
	webhooksCmd.AddCommand(webhooksUpdateCmd)
}
