package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var automationsGetCmd = &cobra.Command{
	Use:   "get <automation-id>",
	Short: "Get a single automation (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Get(cmd.Context(), client.Path("/api/v1/automations/%s", args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var pretty bytes.Buffer
		if err := json.Indent(&pretty, data, "", "  "); err != nil {
			_, _ = cmd.OutOrStdout().Write(data)
			return nil
		}
		pretty.WriteByte('\n')
		_, _ = cmd.OutOrStdout().Write(pretty.Bytes())
		return nil
	},
}

func init() {
	automationsCmd.AddCommand(automationsGetCmd)
}
