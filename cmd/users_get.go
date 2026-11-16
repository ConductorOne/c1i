package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var usersGetCmd = &cobra.Command{
	Use:   "get <user-id>",
	Short: "Get a single user by ID (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Get(cmd.Context(), userPath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "id")
	},
}

// userPath is GET /api/v1/users/{id}, shared with the owner-set existence check.
func userPath(id string) string { return client.Path("/api/v1/users/%s", id) }

// requireExistingUsers GETs each distinct id once and turns a 404 into a usage
// error naming it. An owner-set PUT accepts an unknown user id with 200 (the
// entitlement one drops it, the app one stores it), which can leave the
// target with no real owners.
func requireExistingUsers(ctx context.Context, c *client.Client, flag string, ids []string) error {
	for _, id := range dedupe(ids) {
		if _, err := c.Get(ctx, userPath(id), nil); err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return &usageError{fmt.Errorf("--%s %q: no such user", flag, id)}
			}
			return fmt.Errorf("checking --%s %q: %w", flag, id, err)
		}
	}
	return nil
}

// existingUsersCheck is requireExistingUsers as a mutation's pre-send read.
func existingUsersCheck(flag string, ids []string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		return nil, requireExistingUsers(ctx, c, flag, ids)
	}}
}

func init() {
	usersCmd.AddCommand(usersGetCmd)
}
