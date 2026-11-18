package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsResourcesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a manually managed resource (pretty JSON)",
	Long: `Create a manually managed resource under one resource type of an app via
POST .../resource_types/{resource-type-id}/resources.

--owner-id makes the given C1 users the resource's owners (repeatable; a
repeated id is sent once). For annotations or matchBatonId, pass the whole
request with --body-file instead of --display-name/--description/--owner-id;
it is sent verbatim, and its owner ids are not looked up first.

The server enforces a --display-name length range; a name outside it is a 400
(exit 2) whose message states the range.

The resource type is looked up first: a type that does not exist exits 4,
and a deleted type exits 2 (the server answers that create with a 500, "resource type is
deleted"). --dry-run does the lookups too.

Prints the created resource, unwrapped like "apps resources get": the id is
.id. Honors --dry-run.

Example:
  c1i apps resources create --app-id <app-id> --resource-type-id <resource-type-id> \
    --display-name "Payroll admins" --owner-id <user-id>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s := resourceScopeOf(cmd)
		body, owners, err := buildResourceCreateBody(cmd)
		if err != nil {
			return err
		}
		opts := []mutationOpt{withPreRead(liveResourceTypeCheck(s))}
		if len(owners) > 0 {
			opts = append(opts, withPreRead(existingUsersCheck("owner-id", owners)))
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: s.resourcesPath(), Body: body}, opts...)
	},
}

// resourceCreateFieldFlags build the body; --body-file replaces all of them.
var resourceCreateFieldFlags = []string{"display-name", "description", "owner-id"}

// buildResourceCreateBody returns the request and the deduped --owner-id list.
func buildResourceCreateBody(cmd *cobra.Command) (map[string]any, []string, error) {
	if flagGiven(cmd, "body-file") {
		for _, n := range resourceCreateFieldFlags {
			if flagGiven(cmd, n) {
				return nil, nil, &usageError{fmt.Errorf("--body-file is mutually exclusive with --%s", n)}
			}
		}
		body, err := readJSONFile[map[string]any](cmd, "body-file", mustGetString(cmd, "body-file"))
		return body, nil, err
	}
	if err := requireNonEmpty(cmd, "display-name"); err != nil {
		return nil, nil, fmt.Errorf("%w (or pass --body-file)", err)
	}
	body := map[string]any{"displayName": mustGetString(cmd, "display-name")}
	if v := mustGetString(cmd, "description"); v != "" {
		body["description"] = v
	}
	owners, err := repeatableStringFlag(cmd, "owner-id")
	if err != nil {
		return nil, nil, err
	}
	if len(owners) > 0 {
		owners = dedupe(owners)
		body["resourceOwnerUserIds"] = owners
	}
	return body, owners, nil
}

// liveResourceTypeCheck refuses an unknown (exit 4) or deleted (exit 2)
// resource type before a create the server would answer with a 404 or a 500.
func liveResourceTypeCheck(s resourceScope) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		data, err := c.Get(ctx, resourceTypePath(s.appID, s.typeID), nil)
		if err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return nil, refNotFound("resource-type-id", s.typeID, "resource type under --app-id "+strconv.Quote(s.appID), apiErr)
			}
			return nil, fmt.Errorf("checking --resource-type-id %q: %w", s.typeID, err)
		}
		var resp struct {
			View struct {
				Type *struct {
					DeletedAt *string `json:"deletedAt"`
				} `json:"appResourceType"`
			} `json:"appResourceTypeView"`
		}
		if err := json.Unmarshal(data, &resp); err != nil || resp.View.Type == nil {
			return nil, &nonJSONResponseError{fmt.Errorf("checking --resource-type-id %q: the resource type lookup did not return a resource type", s.typeID)}
		}
		if d := resp.View.Type.DeletedAt; d != nil && *d != "" {
			return nil, &usageError{fmt.Errorf("--resource-type-id %q is deleted (at %s); create the resource under a live type", s.typeID, *d)}
		}
		return nil, nil
	}}
}

func init() {
	c := appsResourcesCreateCmd
	addResourceScopeFlags(c)
	c.Flags().String("display-name", "", "Display name of the new resource (required unless --body-file)")
	c.Flags().String("description", "", "Description of the new resource")
	addRepeatableStringFlag(c, "owner-id", "C1 user ID to make an owner of the new resource (repeatable)")
	c.Flags().String("body-file", "", "Full create request JSON object, sent verbatim (file, or \"-\" for stdin); mutually exclusive with --display-name, --description and --owner-id; must not set appId/appResourceTypeId (--app-id and --resource-type-id own them)")
	ownBodyKeys(c, "body-file",
		[]ownedKey{{key: "appId", owner: "--app-id"}, {key: "app_id", owner: "--app-id"}},
		[]ownedKey{{key: "appResourceTypeId", owner: "--resource-type-id"}, {key: "app_resource_type_id", owner: "--resource-type-id"}})
	annotateRequired(c, "display-name")
	markMutating(c)
	markUserRefChecked(c, "owner-id")
	appsResourcesCmd.AddCommand(c)
}
