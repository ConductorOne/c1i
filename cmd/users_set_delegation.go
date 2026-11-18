package cmd

import (
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var usersSetDelegationCmd = &cobra.Command{
	Use:   "set-delegation <user-id>",
	Short: "Set or clear a user's expiring delegation, as an admin",
	Long: `Designate a delegate for a user, optionally for a time window
(POST /api/v1/users/{user_id}/set-delegation-by-admin).
--delegate-user-id sets the delegate; --clear removes the delegation (it sends
an empty delegate id, which the API describes as removing it). --start-at and
--expire-at are RFC3339 timestamps and go with --delegate-user-id.

Observed against a tenant: the server answered an unknown delegate with 404
(exit 4), an expiry before the start with 400 "end date must be after start
date for temporary user delegation" (exit 2), and accepted a start or expiry in
the past and a self-delegation at exit 0. Clearing returned {"item": null},
repeatedly; on a user who had never had a delegation it returned 404 "provided
delegated user id is empty" (exit 4). "users get" showed delegatedUserId empty
while the start was still in the future, so the printed .item is the
confirmation.

Prints the binding unwrapped from the response's .item (the response as is,
{"item": null}, when clearing). Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		clear, _ := cmd.Flags().GetBool("clear")
		if clear {
			for _, n := range []string{"delegate-user-id", "start-at", "expire-at"} {
				if flagGiven(cmd, n) {
					return &usageError{fmt.Errorf("--clear is mutually exclusive with --delegate-user-id/--start-at/--expire-at")}
				}
			}
		} else if !flagGiven(cmd, "delegate-user-id") {
			return &usageError{fmt.Errorf("--delegate-user-id is required (or pass --clear)")}
		}
		body := map[string]any{"delegatedUserId": ""}
		if !clear {
			body["delegatedUserId"] = mustGetString(cmd, "delegate-user-id")
		}
		for _, f := range [][2]string{{"start-at", "delegationStartAt"}, {"expire-at", "delegationExpireAt"}} {
			if !flagGiven(cmd, f[0]) {
				continue
			}
			v := mustGetString(cmd, f[0])
			if err := validateRFC3339("--"+f[0], v); err != nil {
				return err
			}
			body[f[1]] = v
		}
		path := client.Path("/api/v1/users/%s/set-delegation-by-admin", args[0])
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body, Unwrap: named("item")})
	},
}

func init() {
	f := usersSetDelegationCmd.Flags()
	f.String("delegate-user-id", "", "User who acts as delegate (required unless --clear)")
	f.Bool("clear", false, "Remove the delegation (sends an empty delegate id; mutually exclusive with the other flags)")
	f.String("start-at", "", "RFC3339 time the delegation starts (omit for no start)")
	f.String("expire-at", "", "RFC3339 time the delegation expires (omit for no expiry)")
	markMutating(usersSetDelegationCmd)
	usersCmd.AddCommand(usersSetDelegationCmd)
}
