package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsManagedStatePromoteCmd = &cobra.Command{
	Use:   "promote <resource-id>",
	Short: "Promote a discovered app to a managed app",
	Long: `Promote a discovered app to a managed app via POST .../promote: C1 creates a new
app for <resource-id> (a "managed-state list" row's resource_id) and prints the
binding as JSON, unwrapped like "managed-state get"; its top-level appId is the
connector app, and the new app is .state.managed.appId.

WARNING: a promote creates an app, and nothing checks for an existing app of the
same name: promoting two discovered apps with the same name made two apps with
that name. Before promoting, run "apps list" and look for one; after any
failure, inspect with "managed-state get" and "apps list" before retrying.

Promoting an already-managed binding exits 0 and prints the binding with the
existing app id; it created no second app.

Without --user-id the new app's owners were the connector app's owners; with
it, exactly the --user-id users. Owner changes are applied asynchronously, so
"apps owners <app-id>" can lag.

Deleting the managed app afterwards soft-deletes the binding until the
connector's next sync: it leaves "managed-state list", "managed-state get"
still answers with deletedAt set, and promote exits 4. After the sync the
binding was back, unmanaged, under the same resource_id, and can be promoted
again, which makes a second app.

An unknown app, resource type, resource id or user exits 4, --dry-run included
(the binding is read first; a binding whose app was deleted still reads, so a
preview does not catch it).

Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		userIDs, err := repeatableStringFlag(cmd, "user-id")
		if err != nil {
			return err
		}
		userIDs = dedupe(userIDs)
		body := map[string]any{}
		if len(userIDs) > 0 {
			body["userIds"] = userIDs
		}
		bindingPath := managedStateBindingPath(cmd, args[0])
		return sendMutation(cmd, mutation{
			Method: http.MethodPost,
			Path:   bindingPath + "/promote",
			Body:   body,
			Unwrap: byID("resourceId"),
		},
			withPreRead(bindingExistsCheck(bindingPath, args[0])),
			withPreRead(existingUsersCheck("user-id", userIDs)))
	},
}

// bindingExistsCheck is the promote's pre-send read of the binding, so a
// --dry-run refuses an unknown binding as a real run does (exit 4).
func bindingExistsCheck(path, resourceID string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		if _, err := c.Get(ctx, path, nil); err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return nil, &refNotFoundError{fmt.Sprintf("<resource-id> %q: no such discovered app under that --app-id and --resource-type-id", resourceID), apiErr}
			}
			return nil, fmt.Errorf("checking <resource-id> %q: %w", resourceID, err)
		}
		return nil, nil
	}}
}

func init() {
	addManagedStateScopeFlags(appsManagedStatePromoteCmd)
	addRepeatableStringFlag(appsManagedStatePromoteCmd, "user-id", "C1 user ID to own the new app (repeatable); omit to inherit the connector app's owners")
	markMutating(appsManagedStatePromoteCmd)
	markUserRefChecked(appsManagedStatePromoteCmd, "user-id")
	appsManagedStateCmd.AddCommand(appsManagedStatePromoteCmd)
}
