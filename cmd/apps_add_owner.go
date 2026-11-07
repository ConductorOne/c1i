package cmd

import (
	"net/http"

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

` + ownerAsyncNote + `
Verify with "c1i apps owners <app-id>".

Prints the API's response, which is {}; exit 0 is the success signal.
Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		// Everything the endpoint needs is in the path, but it still parses
		// the body as a protobuf message: a `null` body earns a 400
		// ("unexpected token null"), verified live. Send `{}`.
		return sendMutation(cmd, mutation{
			Method: http.MethodPost,
			Path:   client.Path("/api/v1/apps/%s/owners/%s", appID, args[0]),
			Body:   addOwnerEmptyBody(),
		})
	},
}

func init() {
	appsAddOwnerCmd.Flags().String("app-id", "", "Application ID to add the owner to")
	markRequired(appsAddOwnerCmd, "app-id")
	markMutating(appsAddOwnerCmd)
	appsCmd.AddCommand(appsAddOwnerCmd)
}

// addOwnerEmptyBody is the empty payload POST .../owners/{user_id} requires.
// A function, not a shared package-level map, so a caller cannot mutate it.
func addOwnerEmptyBody() map[string]any { return map[string]any{} }
