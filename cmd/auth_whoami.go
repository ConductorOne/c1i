package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the authenticated principal's user ID and tenant scope",
	Long: `Calls /api/v1/auth/introspect and returns a compact summary of the
authenticated principal: userId, principleId, and counts of roles,
permissions, and feature flags.

The full introspect payload can include hundreds of roles and over a
thousand permissions — pass --verbose to dump it all.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("not authenticated: %w", err)
		}
		body, err := c.Get(cmd.Context(), "/api/v1/auth/introspect", nil)
		if err != nil {
			return err
		}

		verbose, _ := cmd.Flags().GetBool("verbose")
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			return fmt.Errorf("parsing introspect response: %w", err)
		}

		var out []byte
		if verbose {
			out, err = json.MarshalIndent(payload, "", "  ")
		} else {
			out, err = json.MarshalIndent(summarize(payload), "", "  ")
		}
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

func summarize(p map[string]any) map[string]any {
	return map[string]any{
		"userId":      p["userId"],
		"principleId": p["principleId"],
		"counts": map[string]int{
			"roles":       sliceLen(p["roles"]),
			"permissions": sliceLen(p["permissions"]),
			"features":    sliceLen(p["features"]),
		},
	}
}

func sliceLen(v any) int {
	if s, ok := v.([]any); ok {
		return len(s)
	}
	return 0
}

func init() {
	authWhoamiCmd.Flags().BoolP("verbose", "v", false, "Include full roles, permissions, and features arrays")
	authCmd.AddCommand(authWhoamiCmd)
}
