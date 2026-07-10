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

		// Best-effort: enrich with display_name + email from /api/v1/users/{id}.
		// If that call fails (permissions, network), we still return the
		// introspect summary — identity from introspect alone is the contract.
		var displayName, email string
		if uid, ok := payload["userId"].(string); ok && uid != "" {
			if userBody, uerr := c.Get(cmd.Context(), client.Path("/api/v1/users/%s", uid), nil); uerr == nil {
				var u struct {
					UserView struct {
						User struct {
							DisplayName string `json:"displayName"`
							Email       string `json:"email"`
						} `json:"user"`
					} `json:"userView"`
				}
				if json.Unmarshal(userBody, &u) == nil {
					displayName = u.UserView.User.DisplayName
					email = u.UserView.User.Email
				}
			}
		}

		var obj any = summarize(payload, displayName, email)
		if verbose {
			obj = payload
		}
		out, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		// Route through writeObject so --fields works (e.g. whoami --fields email).
		return writeObject(cmd, out)
	},
}

func summarize(p map[string]any, displayName, email string) map[string]any {
	out := map[string]any{
		"userId":      p["userId"],
		"principleId": p["principleId"],
		"counts": map[string]int{
			"roles":       sliceLen(p["roles"]),
			"permissions": sliceLen(p["permissions"]),
			"features":    sliceLen(p["features"]),
		},
	}
	if displayName != "" {
		out["displayName"] = displayName
	}
	if email != "" {
		out["email"] = email
	}
	return out
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
