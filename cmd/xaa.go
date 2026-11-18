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

var xaaCmd = &cobra.Command{
	Use:   "xaa",
	Short: "Manage cross-app access (XAA): resource servers, scopes, access profiles, client mappings",
	Long: `Manage cross-app access (XAA) objects of an app: resource servers (the
third-party authorization servers C1 mints grants for), their scopes, access
profiles (requestable bundles of scopes) and the scopes bound to each profile,
plus each resource server's client audience mappings.

Resource servers, scopes and access profiles take --app-id. Client audience
mappings are scoped by --resource-server-id instead: their routes carry no
app. The tenant-wide switch and token settings are under
"c1i settings cross-app-access".

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
	// whatever the mask names; requiredFieldsFill copies them from a read.
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
			return getNamedObject(cmd, k.itemPath(xaaAppID(cmd), args[0]), nil, k.envelope)
		},
	}
	addXAAAppFlag(c)
	return c
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

// refCheck looks up the object of k that --flag names under --app-id: unknown
// exits 4, and with refuseDeleted a deleted one exits 2. The server stores a
// create under an unknown or deleted resource server, and answers an unknown
// scope with 400 (bind) or a silent 200 (unbind). The lookup is under the
// app, so it also catches an unknown app.
func (k xaaKind) refCheck(appID, flag, id string, refuseDeleted bool) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		data, err := c.Get(ctx, k.itemPath(appID, id), nil)
		if err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				return nil, refNotFound(flag, id, k.label+" under --app-id "+strconv.Quote(appID), apiErr)
			}
			return nil, fmt.Errorf("checking --%s %q: %w", flag, id, err)
		}
		deleted, _, ok := deletedAtPath(data, k.envelope)
		if !ok {
			return nil, &nonJSONResponseError{fmt.Errorf("checking --%s %q: the lookup did not return a %s", flag, id, k.label)}
		}
		if refuseDeleted && deleted {
			return nil, &usageError{fmt.Errorf("--%s %q: the %s is deleted", flag, id, k.label)}
		}
		return nil, nil
	}}
}

// claimsHookCheck refuses a modifyClaimsHook whose functionId, or commitId
// under it, does not exist (exit 4): the server stores either, and the API
// spec says the hook then denies every grant. It reads nothing when obj names
// no function. A field set in both spellings is an error before any request.
func claimsHookCheck(obj map[string]any) (preRead, error) {
	hook, _, err := bodyFieldAs[map[string]any](obj, "modifyClaimsHook")
	if err != nil {
		return preRead{}, err
	}
	fnID, _, err := bodyFieldAs[string](hook, "functionId")
	if err != nil {
		return preRead{}, err
	}
	commitID, _, err := bodyFieldAs[string](hook, "commitId")
	if err != nil {
		return preRead{}, err
	}
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		if fnID == "" {
			return nil, nil
		}
		check := func(path, label, id, what string) error {
			if _, err := c.Get(ctx, path, nil); err != nil {
				var apiErr *client.APIError
				if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
					return refNotFound(label, id, what, apiErr)
				}
				return fmt.Errorf("checking --%s %q: %w", label, id, err)
			}
			return nil
		}
		if err := check(client.Path("/api/v1/functions/%s", fnID), "body-file modifyClaimsHook.functionId", fnID, "function"); err != nil {
			return nil, err
		}
		if commitID == "" {
			return nil, nil
		}
		return nil, check(client.Path("/api/v1/functions/%s/commits/%s", fnID, commitID), "body-file modifyClaimsHook.commitId", commitID, "commit of function "+strconv.Quote(fnID))
	}}, nil
}

// requiredFill is requiredFieldsFill for one object of k.
func (k xaaKind) requiredFill(appID, id string, obj map[string]any) preRead {
	return requiredFieldsFill(k.itemPath(appID, id), nil, k.envelope, k.label, k.required, obj)
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
			if err := refuseMaskedFieldWithoutValue(mask, obj, k.required); err != nil {
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
