package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

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

		return writeResource(cmd, data, "id", "userView", "user")
	},
}

// userPath is GET /api/v1/users/{id}, shared with the owner-set existence check.
func userPath(id string) string { return client.Path("/api/v1/users/%s", id) }

// requireExistingUsers GETs each distinct id once and turns a 404 into a
// not-found error naming it, so a write that adds or binds a user never reaches
// the server with an id it would store or drop (an owner-set PUT stores an
// unknown id with 200). A 200 that is not JSON is a C1 fault, not "exists".
func requireExistingUsers(ctx context.Context, c *client.Client, flag string, ids []string) error {
	for _, id := range dedupe(ids) {
		data, err := c.Get(ctx, userPath(id), nil)
		if err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return refNotFound(flag, id, "user", apiErr)
			}
			return fmt.Errorf("checking --%s %q: %w", flag, id, err)
		}
		if !json.Valid(data) {
			return &nonJSONResponseError{fmt.Errorf("checking --%s %q: the user lookup did not return JSON", flag, id)}
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

// userRefCheckedAnnotation lists, comma-joined, the flags of a mutating
// command whose C1 user ids are looked up by existingUsersCheck before the
// send. TestEveryUserRefFlagIsCheckedOrExempt holds every user-id flag to it.
const userRefCheckedAnnotation = "c1i/user-ref-checked"

// userRefHelp is appended to the help of every command marked as checking user
// ids, so the contract is stated once.
const userRefHelp = "Looks up each referenced user first: an unknown id exits 4 before anything is sent, and the credential needs user read (else exit 3), --dry-run included."

// markUserRefChecked records that cmd runs existingUsersCheck on each flag and
// documents it.
func markUserRefChecked(cmd *cobra.Command, flags ...string) {
	cmd.Long = strings.TrimRight(cmd.Long, "\n") + "\n\n" + userRefHelp
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[userRefCheckedAnnotation] = strings.Join(flags, ",")
}

func init() {
	usersCmd.AddCommand(usersGetCmd)
}
