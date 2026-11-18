package cmd

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/timing"
	"github.com/spf13/cobra"
)

// Entitlement owners exist only on the v2 owner routes, which key each owner
// by a role slug. "primary" is the only role: other slugs read back empty at
// 200, and the API docs call it the only supported value. The v2 "primary"
// user owners are the v1 owners, so user mode stays on v1.
const ownerRoleSlug = "primary"

// ownerV2 is one owned object's v2 owner routes.
type ownerV2 struct {
	base     string // the owned object's v2 path, e.g. /api/v2/apps/{id}
	ownerIDs string // its v1 GET .../ownerids, which --wait reads for users
	appID    string // the --owner-entitlement-app-id default
	envelope string // the key a create response wraps the owner in
	target   string // names the object in --wait progress lines
	recheck  string // the read to suggest on a --wait timeout
}

func appOwnerV2(appID string) ownerV2 {
	return ownerV2{
		base:     client.Path("/api/v2/apps/%s", appID),
		ownerIDs: client.Path("/api/v1/apps/%s/ownerids", appID),
		appID:    appID,
		envelope: "appOwnerEntitlement",
		target:   "app " + appID,
		recheck:  "c1i apps owners " + appID,
	}
}

func entitlementOwnerV2(appID, entitlementID string) ownerV2 {
	return ownerV2{
		base:     client.Path("/api/v2/apps/%s/entitlements/%s", appID, entitlementID),
		ownerIDs: client.Path("/api/v1/apps/%s/entitlements/%s/ownerids", appID, entitlementID),
		appID:    appID,
		envelope: "appEntitlementOwnerEntitlement",
		target:   "entitlement " + entitlementID + " of app " + appID,
		recheck:  "c1i entitlements owners " + entitlementID + " --app-id " + appID,
	}
}

// ownerV2 uses the v2 routes' plural "resources", unlike ownersPath.
func (s resourceScope) ownerV2(id string) ownerV2 {
	return ownerV2{
		base:     client.Path("/api/v2/apps/%s/resource_types/%s/resources/%s", s.appID, s.typeID, id),
		ownerIDs: s.ownerIDsPath(id),
		appID:    s.appID,
		envelope: "appResourceOwnerEntitlement",
		target:   "resource " + id,
		recheck:  "c1i apps resources owners " + id + " --app-id " + s.appID + " --resource-type-id " + s.typeID,
	}
}

// entitlementRef is the API's AppEntitlementRef.
type entitlementRef struct {
	AppID string `json:"appId"`
	ID    string `json:"id"`
}

func (r entitlementRef) waitKey() string { return "entitlement:" + r.AppID + "/" + r.ID }

func (o ownerV2) entitlementPath(r entitlementRef) string {
	return o.base + client.Path("/owners/entitlements/%s/%s/%s", ownerRoleSlug, r.AppID, r.ID)
}

// entitlementOwner is one item of GET .../owners/entitlements.
type entitlementOwner struct {
	AppEntitlement struct {
		ID          string `json:"id"`
		AppID       string `json:"appId"`
		DisplayName string `json:"displayName"`
		Slug        string `json:"slug"`
		Alias       string `json:"alias"`
		DeletedAt   string `json:"deletedAt"`
	} `json:"appEntitlement"`
	RoleSlug  string `json:"roleSlug"`
	CreatedAt string `json:"createdAt"`
}

// entitlementOwnerRow: id is the owning entitlement's, as a user owner row's
// is the user's; added_at is when it became an owner.
func entitlementOwnerRow(o entitlementOwner) map[string]any {
	e := o.AppEntitlement
	return map[string]any{
		"id":           nilIfEmpty(e.ID),
		"app_id":       nilIfEmpty(e.AppID),
		"display_name": nilIfEmpty(e.DisplayName),
		"slug":         nilIfEmpty(e.Slug),
		"alias":        nilIfEmpty(e.Alias),
		"role_slug":    nilIfEmpty(o.RoleSlug),
		"added_at":     nilIfEmpty(o.CreatedAt),
		"deleted_at":   nilIfEmpty(e.DeletedAt),
	}
}

func (o ownerV2) entitlementOwnersSpec(ctx context.Context) pageSpec[entitlementOwner] {
	path := o.base + "/owners/entitlements"
	return pageSpec[entitlementOwner]{
		Route:     "GET " + path,
		Fetch:     getPage(ctx, path, map[string]string{"role_slug": ownerRoleSlug}),
		Parse:     parseList[entitlementOwner],
		ID:        func(e entitlementOwner) string { return e.AppEntitlement.AppID + "/" + e.AppEntitlement.ID },
		Rows:      rowFn(entitlementOwnerRow),
		Composite: true,
	}
}

// listEntitlementOwners is "owners --entitlements".
func listEntitlementOwners(cmd *cobra.Command, o ownerV2) error {
	c, err := listClientFor(cmd)
	if err != nil {
		return err
	}
	return listPages(cmd, c, o.entitlementOwnersSpec(cmd.Context()))
}

// entitlementOwnerWaitInterval is how often the entitlement form of
// set-owners --wait re-polls.
const entitlementOwnerWaitInterval = 12 * time.Second

// ownerKeys reads the owner set --wait compares: "user:<id>" for each id in
// the v1 GET .../ownerids, which "owners" agrees with and which a v2 set was
// seen to update minutes late or not at all, and entitlementRef.waitKey for
// every entitlement owner, all pages.
func (o ownerV2) ownerKeys(ctx context.Context, c *client.Client) ([]string, error) {
	userIDs, err := fetchOwnerIDs(ctx, c, o.ownerIDs)
	if err != nil {
		return nil, fmt.Errorf("API error: %w", err)
	}
	spec := o.entitlementOwnersSpec(ctx)
	spec.PageSize = maxPageSize
	ents, err := collectPages(c, spec)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(userIDs)+len(ents))
	for _, id := range userIDs {
		keys = append(keys, "user:"+id)
	}
	for _, e := range ents {
		keys = append(keys, entitlementRef{AppID: e.AppEntitlement.AppID, ID: e.AppEntitlement.ID}.waitKey())
	}
	return keys, nil
}

func (o ownerV2) waitOp(c *client.Client, done func([]string) bool, timeout time.Duration) waitOp[[]string] {
	return waitOp[[]string]{
		Poll:     func(ctx context.Context) ([]string, error) { return o.ownerKeys(ctx, c) },
		Done:     done,
		Interval: timing.Scale(entitlementOwnerWaitInterval),
		Timeout:  timeout,
		Subject:  "owners to provision on " + o.target,
		Success:  "Owners provisioned on " + o.target,
		Slow:     "owner changes " + ownerAsyncLag,
		Recheck:  o.recheck + " and " + o.recheck + " --entitlements",
	}
}

// Help shared by the owner verbs of every family.
const (
	entitlementOwnerNote = `With --owner-entitlement-id instead of --user-id, the owner is an
entitlement, whose grantees own the object. --owner-entitlement-app-id names
the entitlement's app when it is not the owned object's. Entitlement owners
use the v2 owner routes and the role "primary", the only role the API
supports; list them with "owners --entitlements".`

	entitlementOwnerAddNote = entitlementOwnerNote + `
Adding an entitlement that is already an owner exits 0; an unknown
entitlement exits 4. --wait does not apply to this form (exit 2): the
entitlement owner is in place when add-owner returns.`

	entitlementOwnerRemoveNote = entitlementOwnerNote + `
Removing an entitlement owner a second time exits 0; an unknown entitlement
exits 4.`

	entitlementOwnerSetNote = `With --owner-entitlement-id, entitlements own the object too (their
grantees are its owners), and the set goes through the v2 owner route, which
replaces the entitlement owners AND is meant to replace the user owners with
the --user-id values. Repeat --user-id for each user owner to keep, or pass
--no-user-owners to remove them all; with neither, c1i refuses (exit 2) and
sends nothing. The user part was not reliably reflected by the v1 owner
reads: for an app they caught up 1-2 minutes later, and for a resource a
user owner the set removed was still listed by "owners" and GET
.../ownerids more than 8 minutes later. --wait therefore polls GET
.../ownerids for the users and the v2 list for the entitlements until both
equal the request, and times out (exit 1, the write applied) if they never
do. Without --owner-entitlement-id, set-owners replaces user owners only and
leaves entitlement owners in place. --owner-entitlement-app-id names the
entitlements' app when it is not the owned object's. In this form an unknown
entitlement or owned object exits 4.`

	entitlementOwnersListNote = `--entitlements lists the entitlements that own it instead of its users,
from the v2 owner route (role "primary"): id is the entitlement's and
added_at is when it became an owner. There an unknown id exits 4.`
)

// addEntitlementOwnerFlags registers --owner-entitlement-id (repeatable when
// several) and --owner-entitlement-app-id.
func addEntitlementOwnerFlags(cmd *cobra.Command, several bool) {
	if several {
		addRepeatableStringFlag(cmd, "owner-entitlement-id", "Entitlement whose grantees own it (repeatable; replaces user AND entitlement owners)")
	} else {
		addOnceStringFlag(cmd, "owner-entitlement-id", "Entitlement whose grantees own it; this or --user-id is required",
			"one owner per call")
	}
	addOnceStringFlag(cmd, "owner-entitlement-app-id", "App of --owner-entitlement-id (default: the owned object's app)",
		"the entitlements of one call share an app")
}

// entitlementAppID is --owner-entitlement-app-id, or def when it is not given.
func entitlementAppID(cmd *cobra.Command, def string) (string, error) {
	if !flagGiven(cmd, "owner-entitlement-app-id") {
		return def, nil
	}
	if !flagGiven(cmd, "owner-entitlement-id") {
		return "", &usageError{fmt.Errorf("--owner-entitlement-app-id requires --owner-entitlement-id")}
	}
	return mustGetString(cmd, "owner-entitlement-app-id"), nil
}

// ownerEntitlement picks add-owner's or remove-owner's mode: ok is true for an
// entitlement owner, false for --user-id. Exactly one of the two is required.
func ownerEntitlement(cmd *cobra.Command, o ownerV2) (entitlementRef, bool, error) {
	userGiven, entGiven := flagGiven(cmd, "user-id"), flagGiven(cmd, "owner-entitlement-id")
	appID, err := entitlementAppID(cmd, o.appID)
	switch {
	case err != nil:
		return entitlementRef{}, false, err
	case userGiven && entGiven:
		return entitlementRef{}, false, &usageError{fmt.Errorf("--user-id and --owner-entitlement-id are mutually exclusive")}
	case !userGiven && !entGiven:
		return entitlementRef{}, false, &usageError{fmt.Errorf("one of --user-id or --owner-entitlement-id is required")}
	case userGiven:
		return entitlementRef{}, false, nil
	}
	return entitlementRef{AppID: appID, ID: mustGetString(cmd, "owner-entitlement-id")}, true, nil
}

// addEntitlementOwner POSTs one entitlement owner. The ref is in the path; the
// body repeats it, as the schema asks.
func addEntitlementOwner(cmd *cobra.Command, o ownerV2, ref entitlementRef, extra ...mutationOpt) error {
	if wait, _ := cmd.Flags().GetBool("wait"); wait {
		return &usageError{fmt.Errorf("--wait does not apply with --owner-entitlement-id: the entitlement owner is in place when add-owner returns")}
	}
	return sendMutation(cmd, mutation{
		Method: http.MethodPost,
		Path:   o.entitlementPath(ref),
		Body:   map[string]any{"appEntitlementRef": ref},
		Unwrap: named(o.envelope),
	}, extra...)
}

// removeEntitlementOwner DELETEs one entitlement owner; the route takes no
// body.
func removeEntitlementOwner(cmd *cobra.Command, o ownerV2, ref entitlementRef, extra ...mutationOpt) error {
	return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: o.entitlementPath(ref)}, extra...)
}

// setOwnersV2 PUTs both owner lists. userIDs is empty only with
// --no-user-owners; the explicit [] shows that in the preview.
func setOwnersV2(cmd *cobra.Command, o ownerV2, userIDs, entitlementIDs []string, wait bool, waitTimeout time.Duration, extra ...mutationOpt) error {
	appID, err := entitlementAppID(cmd, o.appID)
	if err != nil {
		return err
	}
	userRefs := make([]map[string]string, 0, len(userIDs))
	want := make([]string, 0, len(userIDs)+len(entitlementIDs))
	for _, id := range userIDs {
		userRefs = append(userRefs, map[string]string{"id": id})
		want = append(want, "user:"+id)
	}
	entRefs := make([]entitlementRef, 0, len(entitlementIDs))
	for _, id := range entitlementIDs {
		r := entitlementRef{AppID: appID, ID: id}
		entRefs = append(entRefs, r)
		want = append(want, r.waitKey())
	}
	opts := append([]mutationOpt{}, extra...)
	if len(userIDs) > 0 {
		opts = append(opts, withPreRead(existingUsersCheck("user-id", userIDs)))
	}
	opts = append(opts, withWait(func(c *client.Client) waitOp[[]string] {
		return o.waitOp(c, untilEqual(want), waitTimeout)
	}, wait))
	return sendMutation(cmd, mutation{
		Method: http.MethodPut,
		Path:   o.base + "/owners",
		Body:   map[string]any{"roleSlug": ownerRoleSlug, "userRefs": userRefs, "appEntitlementRefs": entRefs},
	}, opts...)
}

// addEntitlementsListFlag registers "owners --entitlements".
func addEntitlementsListFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("entitlements", false, "List the entitlements that own it instead of its users")
}

func entitlementsFlag(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("entitlements")
	return v
}
