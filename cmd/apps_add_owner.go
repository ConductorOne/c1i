package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsAddOwnerCmd = &cobra.Command{
	Use:   "add-owner <user-id>",
	Short: "Add one owner to an app",
	Long: `Add a single user as an app owner via POST .../owners/{user_id}. Unlike
"apps set-owners", which replaces the whole list with exactly the ids you
pass, this adds one owner without touching the rest of the list. Two
add-owner calls issued at the same time were both observed to land.
"set-owners" is the one to be careful with: build its list from a read
taken moments earlier and any owner added in between is silently removed
-- including the creator "apps create" auto-assigns (see below).

"apps create" auto-assigns its caller as an owner, so most apps already have
at least one before you ever run this -- this adds to that set, it doesn't
start one.

Owner changes are provisioned ASYNCHRONOUSLY: this call returns as soon as
the request is accepted, but the new owner takes roughly 45-150s to show
up. Verify with "c1i apps owners <app-id>".

Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		userID := args[0]

		path := client.Path("/api/v1/apps/%s/owners/%s", appID, userID)
		// Everything the endpoint needs is in the path, but it still parses
		// the body as a protobuf message: a nil body marshals to `null` and
		// earns a 400 ("unexpected token null"), verified live. Send `{}`.
		body := addOwnerEmptyBody()
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
	appsAddOwnerCmd.Flags().String("app-id", "", "Application ID to add the owner to")
	markRequired(appsAddOwnerCmd, "app-id")
	appsCmd.AddCommand(appsAddOwnerCmd)
}

// addOwnerEmptyBody is the empty payload POST .../owners/{user_id} requires.
// A function, not a shared package-level map, so a caller cannot mutate it.
func addOwnerEmptyBody() map[string]any { return map[string]any{} }
