package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var xaaCmd = &cobra.Command{
	Use:   "xaa",
	Short: "Manage cross-app access (XAA): resource servers, scopes, access profiles",
	Long: `Manage cross-app access (XAA) objects of an app: resource servers (the
third-party authorization servers C1 mints grants for), their scopes, and
access profiles (requestable bundles of scopes).

Every per-app command takes --app-id. The tenant-wide switch and token
settings are under "c1i settings cross-app-access".

Mutations print the object unwrapped like the matching get (id: .id); deletes
and resync-scopes print {}. --fields is a usage error on a mutation.`,
}

// xaaKind is one app-scoped XAA service: the same routes under
// /api/v1/apps/{app}/xaa/<segment>, the same get/list/delete/history shapes.
type xaaKind struct {
	noun     string // "resource-server": positional name and wording
	label    string // "resource server": error wording
	segment  string // "resource_servers"
	envelope string // single-object response key: "resourceServer"
	listKey  string // List response key: "resourceServers"
	row      listRowFn
	// required are the object fields the server validates on every update,
	// whatever the mask names; xaaRequiredFill copies them from a read.
	required []string
}

func (k xaaKind) collectionPath(appID string) string {
	return client.Path("/api/v1/apps/%s/xaa/"+k.segment, appID)
}

func (k xaaKind) itemPath(appID, id string) string {
	return client.Path("/api/v1/apps/%s/xaa/"+k.segment+"/%s", appID, id)
}

const xaaDescriptionUpdateUsage = `New description; "" clears it (omit to leave unchanged)`

func xaaAppID(cmd *cobra.Command) string {
	id, _ := cmd.Flags().GetString("app-id")
	return id
}

// addXAAAppFlag registers the required parent --app-id.
func addXAAAppFlag(c *cobra.Command) {
	c.Flags().String("app-id", "", "ID of the app the object belongs to")
	markRequired(c, "app-id")
}

func (k xaaKind) listCmd(short, long string) *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: short,
		Long:  long,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := k.collectionPath(xaaAppID(cmd))
			return listMapPages(cmd, "GET "+path, k.listKey, []string{"id"}, k.row, getPage(cmd.Context(), path, nil))
		},
	}
	addXAAAppFlag(c)
	addPaginationFlags(c)
	return c
}

func (k xaaKind) getCmd(short, long string) *cobra.Command {
	c := &cobra.Command{
		Use:   "get <" + k.noun + "-id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getXAAObject(cmd, k.itemPath(xaaAppID(cmd), args[0]), k.envelope)
		},
	}
	addXAAAppFlag(c)
	return c
}

func getXAAObject(cmd *cobra.Command, path, envelope string) error {
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Get(cmd.Context(), path, nil)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	return writeNamedResource(cmd, data, envelope)
}

func (k xaaKind) deleteCmd(short, long string) *cobra.Command {
	c := &cobra.Command{
		Use:   "delete <" + k.noun + "-id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: k.itemPath(xaaAppID(cmd), args[0])})
		},
	}
	addXAAAppFlag(c)
	markMutating(c)
	return c
}

func (k xaaKind) historyCmd(short, long string) *cobra.Command {
	c := &cobra.Command{
		Use:   "history <" + k.noun + "-id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := listClientFor(cmd)
			if err != nil {
				return err
			}
			path := k.itemPath(xaaAppID(cmd), args[0]) + "/history"
			// Rows carry no id of their own, so only the token and idle-page checks apply.
			return listPages(cmd, c, pageSpec[json.RawMessage]{
				Route: "GET " + path,
				Fetch: getPage(cmd.Context(), path, nil),
				Parse: parseList[json.RawMessage],
			})
		},
	}
	addXAAAppFlag(c)
	addPaginationFlags(c)
	return c
}

// xaaHistoryLong is the shared history help; an unknown id lists nothing.
func xaaHistoryLong(label, getCmd string) string {
	return `One NDJSON record per change to the ` + label + `, newest first as the
server returns them: {"snapshot": {...}, "metadata": {...}}. The snapshot has
the shape "` + getCmd + `" prints; the metadata carries id, changeKind,
createdAt, actor, annotations, traceId and syslogEventId.

An unknown id lists nothing and exits 0.`
}

// xaaAppCheck refuses an unknown --app-id (exit 4) before a create the server
// would accept, storing an object under an app that does not exist.
func xaaAppCheck(appID string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		if _, err := c.Get(ctx, client.Path("/api/v1/apps/%s", appID), nil); err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return nil, refNotFound("app-id", appID, "app", apiErr)
			}
			return nil, fmt.Errorf("checking --app-id %q: %w", appID, err)
		}
		return nil, nil
	}}
}

// xaaResourceServerCheck refuses a --resource-server-id that is unknown under
// --app-id (exit 4) or deleted (exit 2); the server accepts both on create.
// The lookup is under the app, so it also catches an unknown app.
func xaaResourceServerCheck(appID, rsID string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		data, err := c.Get(ctx, xaaResourceServers.itemPath(appID, rsID), nil)
		if err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return nil, refNotFound("resource-server-id", rsID, "resource server under --app-id "+strconv.Quote(appID), apiErr)
			}
			return nil, fmt.Errorf("checking --resource-server-id %q: %w", rsID, err)
		}
		var resp struct {
			RS *struct {
				DeletedAt *string `json:"deletedAt"`
			} `json:"resourceServer"`
		}
		if err := json.Unmarshal(data, &resp); err != nil || resp.RS == nil {
			return nil, &nonJSONResponseError{fmt.Errorf("checking --resource-server-id %q: the lookup did not return a resource server", rsID)}
		}
		if resp.RS.DeletedAt != nil {
			return nil, &usageError{fmt.Errorf("--resource-server-id %q: the resource server is deleted", rsID)}
		}
		return nil, nil
	}}
}

// xaaRequiredFill copies k.required from the current object into obj where
// the request leaves them unset: the server rejects an update without them,
// even when the mask names none. The read also makes an unknown id exit 4
// before the send, --dry-run included.
func (k xaaKind) xaaRequiredFill(appID, id string, obj map[string]any) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		missing := slices.DeleteFunc(slices.Clone(k.required), func(key string) bool { _, set := obj[key]; return set })
		if len(missing) == 0 {
			return nil, nil
		}
		data, err := c.Get(ctx, k.itemPath(appID, id), nil)
		if err != nil {
			return nil, fmt.Errorf("reading the %s before the update: %w", k.label, err)
		}
		var resp map[string]json.RawMessage
		var cur map[string]any
		if json.Unmarshal(data, &resp) != nil || json.Unmarshal(resp[k.envelope], &cur) != nil || cur == nil {
			return nil, &nonJSONResponseError{fmt.Errorf("reading the %s before the update: the response held no %s", k.label, k.envelope)}
		}
		for _, key := range missing {
			v, ok := cur[key].(string)
			if !ok || v == "" {
				return nil, &nonJSONResponseError{fmt.Errorf("reading the %s before the update: the response held no %s", k.label, key)}
			}
			obj[key] = v
		}
		return nil, nil
	}}
}

// refuseMaskedRequiredWithoutValue refuses a mask that names a required field
// the body does not set: the fill would send the current value, a silent no-op.
func (k xaaKind) refuseMaskedRequiredWithoutValue(mask string, obj map[string]any) error {
	for _, path := range strings.Split(mask, ",") {
		for _, key := range k.required {
			if _, set := obj[key]; !set && foldMaskName(path) == foldMaskName(key) {
				return &usageError{fmt.Errorf("--update-mask names %s but --body-file sets no %s", path, key)}
			}
		}
	}
	return nil
}

// buildXAAUpdate returns the wrapped update request and the object inside it
// (for xaaRequiredFill to complete), from the flags or, for a kind with
// --body-file, from the file and --update-mask.
func buildXAAUpdate(cmd *cobra.Command, k xaaKind, spec *updateSpec) (body, obj map[string]any, err error) {
	obj = map[string]any{}
	if cmd.Flags().Lookup("body-file") != nil {
		mask, usingBody, err := bodyFileMask(cmd, spec.flagNames()...)
		if err != nil {
			return nil, nil, err
		}
		if usingBody {
			if obj, err = readJSONFile[map[string]any](cmd, "body-file", mustGetString(cmd, "body-file")); err != nil {
				return nil, nil, err
			}
			if err := k.refuseMaskedRequiredWithoutValue(mask, obj); err != nil {
				return nil, nil, err
			}
			return wrapUpdate(k.envelope, obj, mask), obj, nil
		}
	}
	mask, err := spec.buildUpdateMask(cmd, obj)
	if err != nil {
		return nil, nil, err
	}
	return wrapUpdate(k.envelope, obj, mask), obj, nil
}

// xaaRowBase is the id/app/name/description head every XAA row starts with.
func xaaRowBase(item map[string]any) map[string]any {
	return map[string]any{
		"id":           optString(item, "id"),
		"app_id":       optString(item, "appId"),
		"display_name": optString(item, "displayName"),
		"description":  optString(item, "description"),
		"created_at":   optString(item, "createdAt"),
		"updated_at":   optString(item, "updatedAt"),
	}
}

func init() {
	rootCmd.AddCommand(xaaCmd)
}
