package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsResourcesCmd = &cobra.Command{
	Use:   "resources",
	Short: "Manage an app's resources and their owners",
	Long: `Manage an app's resources: list, search, get, create, update and delete the
manually managed ones, and read or change a resource's owners. A resource
belongs to one resource type of one app, so every per-resource subcommand
takes --app-id and --resource-type-id; find the type with
"c1i apps resource-types list --app-id <app-id>".`,
}

// appResource is the AppResource message, as far as a row needs it.
type appResource struct {
	ID                   string    `json:"id"`
	AppID                string    `json:"appId"`
	ResourceTypeID       string    `json:"appResourceTypeId"`
	DisplayName          string    `json:"displayName"`
	Description          string    `json:"description"`
	CustomDescription    string    `json:"customDescription"`
	ExternalID           string    `json:"externalId"`
	GrantCount           flexInt64 `json:"grantCount"`
	ParentResourceTypeID string    `json:"parentAppResourceTypeId"`
	ParentResourceID     string    `json:"parentAppResourceId"`
	IsManuallyManaged    bool      `json:"isManuallyManaged"`
}

// resourceListItem is one row of the resources list and search: an
// AppResourceView.
type resourceListItem struct {
	AppResource appResource `json:"appResource"`
}

// resourceRow is the NDJSON row of "resources list" and "search". grant_count
// and is_manually_managed keep their JSON types. There is no deleted_at: both
// endpoints omit deleted resources.
func resourceRow(it resourceListItem) map[string]any {
	r := it.AppResource
	return map[string]any{
		"id":                      nilIfEmpty(r.ID),
		"app_id":                  nilIfEmpty(r.AppID),
		"resource_type_id":        nilIfEmpty(r.ResourceTypeID),
		"display_name":            nilIfEmpty(r.DisplayName),
		"description":             nilIfEmpty(r.Description),
		"custom_description":      nilIfEmpty(r.CustomDescription),
		"external_id":             nilIfEmpty(r.ExternalID),
		"grant_count":             int64(r.GrantCount),
		"parent_resource_type_id": nilIfEmpty(r.ParentResourceTypeID),
		"parent_resource_id":      nilIfEmpty(r.ParentResourceID),
		"is_manually_managed":     r.IsManuallyManaged,
	}
}

// resourceScope is the parent scope of one resource: its app and type.
type resourceScope struct{ appID, typeID string }

func resourceScopeOf(cmd *cobra.Command) resourceScope {
	return resourceScope{appID: mustGetString(cmd, "app-id"), typeID: mustGetString(cmd, "resource-type-id")}
}

// resourcesPath is the CRUD collection (plural "resources").
func (s resourceScope) resourcesPath() string {
	return client.Path("/api/v1/apps/%s/resource_types/%s/resources", s.appID, s.typeID)
}

func (s resourceScope) resourcePath(id string) string {
	return s.resourcesPath() + client.Path("/%s", id)
}

// ownersPath and ownerIDsPath use the owner routes' singular "resource".
func (s resourceScope) ownersPath(id string) string {
	return client.Path("/api/v1/apps/%s/resource_types/%s/resource/%s/owners", s.appID, s.typeID, id)
}

func (s resourceScope) ownerIDsPath(id string) string {
	return client.Path("/api/v1/apps/%s/resource_types/%s/resource/%s/ownerids", s.appID, s.typeID, id)
}

// addResourceScopeFlags registers the parent-scope flags every per-resource
// subcommand shares.
func addResourceScopeFlags(cmd *cobra.Command) {
	cmd.Flags().String("app-id", "", "ID of the app the resource belongs to")
	cmd.Flags().String("resource-type-id", "", "ID of the resource type the resource belongs to")
	markRequired(cmd, "app-id", "resource-type-id")
}

// resourceOwnersWaitOp is the --wait of resources add-owner (untilPresent) and
// set-owners (untilEqual).
func resourceOwnersWaitOp(c *client.Client, s resourceScope, id string, done func([]string) bool, timeout time.Duration) waitOp[[]string] {
	return ownerIDsWaitOp(c, s.ownerIDsPath(id), done, timeout,
		"resource "+id,
		"c1i apps resources owners "+id+" --app-id "+s.appID+" --resource-type-id "+s.typeID)
}

// resourceExistsCheck GETs the resource so a write the server would accept for
// an unknown resource exits 4 instead, --dry-run included.
func resourceExistsCheck(s resourceScope, id string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		if _, err := c.Get(ctx, s.resourcePath(id), nil); err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return nil, &refNotFoundError{fmt.Sprintf("<resource-id> %q: no such resource under --app-id %q and --resource-type-id %q", id, s.appID, s.typeID), apiErr}
			}
			return nil, fmt.Errorf("checking <resource-id> %q: %w", id, err)
		}
		return nil, nil
	}}
}

var appsResourcesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the resources of one resource type (NDJSON output)",
	Long: `List the resources of one resource type of an app (NDJSON output). Rows
have the same shape as "apps resources search"; grant_count is a number. A
deleted resource is not listed ("apps resources get" still returns it, with
deletedAt set), while a live resource of a deleted type still is. The list
can trail a write by a few seconds.

A well-formed but unknown resource type returns zero rows and exit 0.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		path := resourceScopeOf(cmd).resourcesPath()
		return listPages(cmd, c, pageSpec[resourceListItem]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[resourceListItem],
			ID:    func(it resourceListItem) string { return it.AppResource.ID },
			Rows:  rowFn(resourceRow),
		})
	},
}

func init() {
	addResourceScopeFlags(appsResourcesListCmd)
	addPaginationFlags(appsResourcesListCmd)
	appsResourcesCmd.AddCommand(appsResourcesListCmd)
	appsCmd.AddCommand(appsResourcesCmd)
}
