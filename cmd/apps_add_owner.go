package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsAddOwnerCmd = &cobra.Command{
	Use:   "add-owner <app-id>",
	Short: "Add one owner to an app",
	Long: `Add the --user-id user as an owner of an app via POST .../owners/{user_id}. Unlike
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
An unknown app id exits 4.
Verify with "c1i apps owners <app-id>".

Prints the API's response, which is {}; exit 0 is the success signal.
Honors --dry-run.

One owner per call; to change several at once use "apps set-owners".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		userID, _ := cmd.Flags().GetString("user-id")
		// Everything the endpoint needs is in the path, but it still parses
		// the body as a protobuf message: a `null` body earns a 400
		// ("unexpected token null"), verified live. Send `{}`.
		return sendMutation(cmd, mutation{
			Method: http.MethodPost,
			Path:   client.Path("/api/v1/apps/%s/owners/%s", args[0], userID),
			Body:   addOwnerEmptyBody(),
		})
	},
}

func init() {
	appsAddOwnerCmd.Flags().String("user-id", "", "C1 user ID to add as owner")
	markRequired(appsAddOwnerCmd, "user-id")
	markMutating(appsAddOwnerCmd)
	appsCmd.AddCommand(appsAddOwnerCmd)
}

// addOwnerEmptyBody is the empty payload POST .../owners/{user_id} requires.
// A function, not a shared package-level map, so a caller cannot mutate it.
func addOwnerEmptyBody() map[string]any { return map[string]any{} }
