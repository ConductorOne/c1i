package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var exportExportersCmd = &cobra.Command{
	Use:   "exporters",
	Short: "Manage system log exporters (continuous audit-log export to a datasource)",
	Long: `A system log exporter continuously writes the tenant's audit events, as
OCSF JSON, to a datasource. This is separate from "export events", which
dumps events to stdout once.

The datasource is not managed through this API: "exporters create" needs the
datasource id of an existing exporter ("exporters list" or "get" shows it as
.datasource.datasourceId).`,
}

var exportFormatFlag = enumFlag{flag: "format", prefix: "EXPORT_FORMAT_", unknown: enumReject, values: []enumValue{
	{name: "zstd", api: "EXPORT_FORMAT_OCSF_JSON_ZSTD"},
	{name: "gzip", api: "EXPORT_FORMAT_OCSF_JSON_GZIP"},
}}

func exporterPath(id string) string { return client.Path("/api/v1/systemlog/exports/%s", id) }

// exporterIDKeys identify an exporter row for the stuck-cursor guard.
var exporterIDKeys = []string{"exportId"}

func identityRow(m map[string]any) map[string]any { return m }

var exportExportersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List system log exporters (NDJSON output)",
	Long: `List system log exporters, one row per exporter as the API returns it
(exportId, displayName, state, datasource, ...). A deleted exporter is not
listed; "exporters get" still returns it with deletedAt set.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listMapPages(cmd, "GET /api/v1/systemlog/exports", "list", exporterIDKeys, identityRow,
			getPage(cmd.Context(), "/api/v1/systemlog/exports", nil))
	},
}

var exportExportersSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search system log exporters (NDJSON output)",
	Long: `Search system log exporters by free text (--query) and/or id (--export-id,
repeatable). Rows are shaped like "exporters list". An unknown --export-id
matches nothing; it does not widen the result.

There is no --name filter: the API's displayName search field was observed to
return an exporter whatever the name, so c1i does not expose it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ids, err := repeatableStringFlag(cmd, "export-id")
		if err != nil {
			return err
		}
		body := map[string]any{}
		if q, _ := cmd.Flags().GetString("query"); q != "" {
			body["query"] = q
		}
		if len(ids) > 0 {
			refs := make([]any, len(ids))
			for i, id := range ids {
				refs[i] = map[string]any{"exportId": id}
			}
			body["refs"] = refs
		}
		return postBodyList(cmd, "/api/v1/search/systemlog/exports", body, identityRow, exporterIDKeys...)
	},
}

var exportExportersGetCmd = &cobra.Command{
	Use:   "get <export-id>",
	Short: "Get a system log exporter (pretty JSON)",
	Long: `Get one exporter by id. A deleted exporter is still returned, with deletedAt
set. Prints the exporter unwrapped: the id is .exportId.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := getExporter(cmd, args[0])
		if err != nil {
			return err
		}
		return writeResource(cmd, data, "exportId")
	},
}

func getExporter(cmd *cobra.Command, id string) ([]byte, error) {
	baseURL, err := GetBaseURL()
	if err != nil {
		return nil, err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Get(cmd.Context(), exporterPath(id), nil)
	if err != nil {
		return nil, fmt.Errorf("API error: %w", err)
	}
	return data, nil
}

var exportExportersEventsCmd = &cobra.Command{
	Use:   "events <export-id>",
	Short: "List the events one exporter has delivered (NDJSON output)",
	Long: `List the OCSF events belonging to one system log exporter, one JSON event per
line, auto-paginating. c1i first reads the exporter so an unknown id exits 4:
the API answers a well-formed unknown id with an empty 200.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		if _, err := c.Get(cmd.Context(), exporterPath(args[0]), nil); err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		path := exporterPath(args[0]) + "/events"
		return listPages(cmd, c, pageSpec[json.RawMessage]{
			Route:    "POST " + path,
			Uncapped: true,
			Fetch:    postPage(cmd.Context(), path, map[string]any{}),
			Parse:    parseList[json.RawMessage],
		})
	},
}

var exportExportersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a system log exporter (pretty JSON)",
	Long: `Create an exporter that writes audit events to an existing datasource, under
--prefix, in --format. The new exporter starts in state EXPORT_STATE_EXPORTING:
it begins writing to the datasource, so choose --prefix deliberately.

--datasource-id must name an existing datasource; the API answers an unknown
one with a 400, "datasource not found". No datasource API is exposed, so c1i
cannot check it first. Omitting --format leaves it
EXPORT_FORMAT_UNSPECIFIED, as the API accepted it.

Prints the exporter unwrapped like "exporters get": the id is .exportId.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildExporterCreateBody(cmd)
		if err != nil {
			return &usageError{err}
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/systemlog/exports", Body: body, Unwrap: byID("exportId")})
	},
}

func buildExporterCreateBody(cmd *cobra.Command) (map[string]any, error) {
	ds := map[string]any{"datasourceId": mustGetString(cmd, "datasource-id")}
	if flagGiven(cmd, "prefix") {
		ds["prefix"] = mustGetString(cmd, "prefix")
	}
	if flagGiven(cmd, "format") {
		f, err := exportFormatFlag.parse(mustGetString(cmd, "format"))
		if err != nil {
			return nil, err
		}
		ds["format"] = f
	}
	return map[string]any{"displayName": mustGetString(cmd, "display-name"), "datasource": ds}, nil
}

var exportExportersDeleteCmd = &cobra.Command{
	Use:   "delete <export-id>",
	Short: "Delete a system log exporter",
	Long: `Delete an exporter by id. The delete is soft: "exporters get" still returns it
with deletedAt set and "exporters list" omits it. A repeat delete exits 0.
Prints the API's response, {}. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: exporterPath(args[0])})
	},
}

// exporterUpdateSpec lists the fields "exporters update" can change. The mask
// paths are the API's; the datasource id is not changeable here.
var exporterUpdateSpec = &updateSpec{fields: []updateField{
	strUpdate("display-name", "displayName"),
	hookUpdate([]string{"prefix"}, exporterDatasourceField("prefix", func(cmd *cobra.Command) (string, error) { return mustGetString(cmd, "prefix"), nil })),
	hookUpdate([]string{"format"}, exporterDatasourceField("format", func(cmd *cobra.Command) (string, error) {
		return exportFormatFlag.parse(mustGetString(cmd, "format"))
	})),
}}

func exporterDatasourceField(key string, value func(*cobra.Command) (string, error)) func(*cobra.Command, map[string]any) ([]string, error) {
	return func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		if !flagGiven(cmd, key) {
			return nil, nil
		}
		v, err := value(cmd)
		if err != nil {
			return nil, err
		}
		ds, _ := obj["datasource"].(map[string]any)
		if ds == nil {
			ds = map[string]any{}
			obj["datasource"] = ds
		}
		ds[key] = v
		return []string{"datasource." + key}, nil
	}
}

var exportExportersUpdateCmd = &cobra.Command{
	Use:   "update <export-id>",
	Short: "Update a system log exporter (pretty JSON)",
	Long: `Change an exporter's --display-name, or its datasource --prefix or --format.
The mask is derived from the flags you pass; an omitted flag is left alone.

The API refuses an update whose exporter has no datasource ("value is
required"), so c1i first reads the exporter and sends its current datasource
back with only your flags changed; --dry-run does the read too. The read and
the write are not atomic. With the mask set, the API changed only the masked
fields (a differing unmasked name, prefix or format in the same request was
ignored). The datasource id cannot be changed here.

Prints the exporter unwrapped like "exporters get": the id is .exportId.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		changed := false
		for _, n := range exporterUpdateSpec.flagNames() {
			changed = changed || flagGiven(cmd, n)
		}
		if !changed {
			return exporterUpdateSpec.nothingToUpdate()
		}
		path := exporterPath(args[0])
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Unwrap: byID("exportId")},
			withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
				data, err := c.Get(ctx, path, nil)
				if err != nil {
					return nil, fmt.Errorf("API error: %w", err)
				}
				body, err := buildExporterUpdateBody(cmd, data)
				if err != nil {
					return nil, err
				}
				m.Body = body
				return nil, nil
			}}))
	},
}

// buildExporterUpdateBody builds the update from the exporter read back: its
// datasource is required by the API, and only the writable fields are kept.
func buildExporterUpdateBody(cmd *cobra.Command, current []byte) (map[string]any, error) {
	var env struct {
		Exporter struct {
			DisplayName string         `json:"displayName"`
			Datasource  map[string]any `json:"datasource"`
		} `json:"exporter"`
	}
	if err := json.Unmarshal(current, &env); err != nil || env.Exporter.Datasource == nil {
		return nil, unreadablePage(fmt.Errorf("exporter read-back has no datasource; cannot build an update"))
	}
	obj := map[string]any{"displayName": env.Exporter.DisplayName, "datasource": env.Exporter.Datasource}
	mask, err := exporterUpdateSpec.buildUpdateMask(cmd, obj)
	if err != nil {
		return nil, err
	}
	return wrapUpdate("exporter", obj, mask), nil
}

func init() {
	addPaginationFlags(exportExportersListCmd)
	addPaginationFlags(exportExportersSearchCmd)
	addPaginationFlags(exportExportersEventsCmd)
	exportExportersSearchCmd.Flags().String("query", "", "Free-text search")
	addRepeatableStringFlag(exportExportersSearchCmd, "export-id", "Match this exporter id (repeatable)")

	f := exportExportersCreateCmd.Flags()
	f.String("display-name", "", "Display name for the exporter ("+displayNameRule+")")
	f.String("datasource-id", "", "Existing datasource to write to")
	f.String("prefix", "", "Path prefix under the datasource (omit for none; cannot be blank)")
	f.String("format", "", "Output format: "+exportFormatFlag.usage())
	markRequired(exportExportersCreateCmd, "display-name", "datasource-id")
	markMutating(exportExportersCreateCmd)

	u := exportExportersUpdateCmd.Flags()
	u.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	u.String("prefix", "", "New path prefix under the datasource (omit to leave unchanged; cannot be blank)")
	u.String("format", "", "New output format: "+exportFormatFlag.usage()+" (omit to leave unchanged)")
	registerUpdateSpec(exportExportersUpdateCmd, exporterUpdateSpec)
	markMutating(exportExportersUpdateCmd)
	markMutating(exportExportersDeleteCmd)

	exportExportersCmd.AddCommand(exportExportersListCmd, exportExportersSearchCmd, exportExportersGetCmd,
		exportExportersCreateCmd, exportExportersUpdateCmd, exportExportersDeleteCmd, exportExportersEventsCmd)
	exportCmd.AddCommand(exportExportersCmd)
}
