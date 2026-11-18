package cmd

import (
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsResourcesOwnersCmd = &cobra.Command{
	Use:   "owners <resource-id>",
	Short: "List a resource's owners (NDJSON output)",
	Long: `List the owners of an app resource via GET .../owners -- the read that
reflects what "apps resources set-owners"/"add-owner"/"remove-owner" write.
` + ownerAsyncNote + `
An owner added moments ago may not appear yet.

Rows have the same shape as "apps owners". The response also carries
immutableUserIds, owners the API says cannot be removed, which these rows
leave out; read it with
  c1i api --path /api/v1/apps/<app-id>/resource_types/<resource-type-id>/resource/<resource-id>/owners

Auto-paginates to completion like every other list command.

The server ignores --resource-type-id and returns the owners of the resource
id (a bogus type id still lists them, observed). A wrong or unknown --app-id
does not: it returns zero rows and exit 0, like an unknown resource id. Use
"apps resources get" with the same ids to validate them; it exits 4.

` + entitlementOwnersListNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if entitlementsFlag(cmd) {
			return listEntitlementOwners(cmd, resourceScopeOf(cmd).ownerV2(args[0]))
		}
		return listOwners(cmd, resourceScopeOf(cmd).ownersPath(args[0]))
	},
}

var appsResourcesSetOwnersCmd = &cobra.Command{
	Use:   "set-owners <resource-id>",
	Short: "Set the owners of a resource (replaces the full owner list)",
	Long: `Set the complete list of owners for an app resource via PUT .../owners,
replacing any existing owners. Provide one or more --user-id.

` + setOwnersLookupNote + `
The API answers an unknown resource, resource type or app 200 {} and stores
the owners anyway, so this command looks the resource up first: an unknown
one exits 4, --dry-run included. The server accepts owners on a deleted
resource.

` + ownerAsyncWaitNote + `
Verify with "c1i apps resources owners <resource-id> --app-id <app-id>
--resource-type-id <resource-type-id>".

` + entitlementOwnerSetNote + `

` + setOwnersWaitNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, id := resourceScopeOf(cmd), args[0]
		return runOwnerSetPut(cmd, s.ownersPath(id), s.ownerV2(id),
			func(c *client.Client, done func([]string) bool, timeout time.Duration) waitOp[[]string] {
				return resourceOwnersWaitOp(c, s, id, done, timeout)
			},
			withPreRead(resourceExistsCheck(s, id)))
	},
}

var appsResourcesAddOwnerCmd = &cobra.Command{
	Use:   "add-owner <resource-id>",
	Short: "Add one owner to a resource",
	Long: `Add the --user-id user as an owner of an app resource via POST .../owners.
Unlike "apps resources set-owners", which replaces the whole list with exactly
the ids you pass, this adds one owner without touching the rest of the list.
Adding a user who is already an owner exits 0.

An unknown resource, resource type or app exits 4.

` + ownerAsyncWaitNote + `
Verify with "c1i apps resources owners <resource-id> --app-id <app-id>
--resource-type-id <resource-type-id>".
--wait polls GET .../ownerids until the owner appears (or --wait-timeout
elapses; a timeout exits 1, though the write was accepted).
Prints the API's JSON response; --wait's progress lines go to stderr.

Honors --dry-run (with --wait, dry-run still only previews the POST; it never
polls).

One owner per call; to change several at once use "apps resources set-owners".

` + entitlementOwnerAddNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}
		s, id := resourceScopeOf(cmd), args[0]
		if ref, ok, err := ownerEntitlement(cmd, s.ownerV2(id)); err != nil || ok {
			if err != nil {
				return err
			}
			return addEntitlementOwner(cmd, s.ownerV2(id), ref)
		}
		userID := mustGetString(cmd, "user-id")
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: s.ownersPath(id), Body: map[string]any{"userId": userID}},
			withPreRead(existingUsersCheck("user-id", []string{userID})),
			withWait(func(c *client.Client) waitOp[[]string] {
				return resourceOwnersWaitOp(c, s, id, untilPresent([]string{userID}), waitTimeout)
			}, wait))
	},
}

var appsResourcesRemoveOwnerCmd = &cobra.Command{
	Use:   "remove-owner <resource-id>",
	Short: "Remove one owner from a resource",
	Long: `Remove the --user-id user as an owner of an app resource via DELETE
.../owners. Unlike "apps resources set-owners", this removes one owner without
touching the rest of the list. Removing a user who is not an owner exits 0.

An unknown resource, resource type, app or user exits 4.
` + ownerAsyncNote + `
Verify with "c1i apps resources owners <resource-id> --app-id <app-id>
--resource-type-id <resource-type-id>".

Prints the API's response, which is {}; exit 0 is the success signal.
Honors --dry-run.

One owner per call; to change several at once use "apps resources set-owners".

` + entitlementOwnerRemoveNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if ref, ok, err := ownerEntitlement(cmd, resourceScopeOf(cmd).ownerV2(args[0])); err != nil || ok {
			if err != nil {
				return err
			}
			return removeEntitlementOwner(cmd, resourceScopeOf(cmd).ownerV2(args[0]), ref)
		}
		return sendMutation(cmd, mutation{
			Method: http.MethodDelete,
			Path:   resourceScopeOf(cmd).ownersPath(args[0]),
			Body:   map[string]any{"userId": mustGetString(cmd, "user-id")},
		})
	},
}

func init() {
	addResourceScopeFlags(appsResourcesOwnersCmd)
	addPaginationFlags(appsResourcesOwnersCmd)
	addEntitlementsListFlag(appsResourcesOwnersCmd)
	appsResourcesCmd.AddCommand(appsResourcesOwnersCmd)

	addResourceScopeFlags(appsResourcesSetOwnersCmd)
	addSetOwnersFlags(appsResourcesSetOwnersCmd)
	markMutating(appsResourcesSetOwnersCmd)
	markUserRefChecked(appsResourcesSetOwnersCmd, "user-id")
	appsResourcesCmd.AddCommand(appsResourcesSetOwnersCmd)

	addResourceScopeFlags(appsResourcesAddOwnerCmd)
	addOnceStringFlag(appsResourcesAddOwnerCmd, "user-id", "C1 user ID to add as owner; this or --owner-entitlement-id is required",
		"use \"apps resources set-owners\" to set several owners")
	addEntitlementOwnerFlags(appsResourcesAddOwnerCmd, false)
	addWaitFlags(appsResourcesAddOwnerCmd, "GET .../ownerids until the owner appears", 4*time.Minute)
	markMutating(appsResourcesAddOwnerCmd)
	markUserRefChecked(appsResourcesAddOwnerCmd, "user-id")
	appsResourcesCmd.AddCommand(appsResourcesAddOwnerCmd)

	addResourceScopeFlags(appsResourcesRemoveOwnerCmd)
	addOnceStringFlag(appsResourcesRemoveOwnerCmd, "user-id", "C1 user ID to remove as owner; this or --owner-entitlement-id is required",
		"use \"apps resources set-owners\" to set several owners")
	addEntitlementOwnerFlags(appsResourcesRemoveOwnerCmd, false)
	markMutating(appsResourcesRemoveOwnerCmd)
	appsResourcesCmd.AddCommand(appsResourcesRemoveOwnerCmd)
}
