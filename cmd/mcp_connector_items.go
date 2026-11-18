package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// mcpItemKind is one connector-scoped MCP item service (tools, prompts,
// resources): the same routes under /apps/{app}/connectors/{connector}/<segment>,
// the same scope flags and a state-only approval. Help text stays per kind.
type mcpItemKind struct {
	noun     string // "tool": positional name, batch wording
	segment  string // "mcp_tools"
	envelope string // Update request key: "tool"
	state    enumFlag
	// removedAdvice follows "--state=removed is system-managed by sync; ".
	removedAdvice string
}

func (k mcpItemKind) itemPath(appID, connectorID, id string) string {
	return client.Path("/api/v1/apps/%s/connectors/%s/"+k.segment+"/%s", appID, connectorID, id)
}

func (k mcpItemKind) stateAPI(name string) string {
	v, _ := k.state.parse(name)
	return v
}

func scopeIDs(cmd *cobra.Command) (appID, connectorID string) {
	appID, _ = cmd.Flags().GetString("app-id")
	connectorID, _ = cmd.Flags().GetString("connector-id")
	return appID, connectorID
}

func addMCPScopeFlags(c *cobra.Command) {
	c.Flags().String("app-id", "", "Application ID")
	c.Flags().String("connector-id", "", "Connector ID")
}

func (k mcpItemKind) getCmd(short, long string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <" + k.noun + "-id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL, err := GetBaseURL()
			if err != nil {
				return err
			}
			c, err := newClient(cmd, baseURL)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
			appID, connectorID := scopeIDs(cmd)
			data, err := c.Get(cmd.Context(), k.itemPath(appID, connectorID, args[0]), nil)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}
			return writeResource(cmd, data, "id")
		},
	}
}

func (k mcpItemKind) historyCmd(short, long string) *cobra.Command {
	return &cobra.Command{
		Use:   "history <" + k.noun + "-id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := listClientFor(cmd)
			if err != nil {
				return err
			}
			appID, connectorID := scopeIDs(cmd)
			path := k.itemPath(appID, connectorID, args[0]) + "/history"
			// Rows carry no id of their own, so only the token and idle-page checks apply.
			return listPages(cmd, c, pageSpec[json.RawMessage]{
				Route: "GET " + path,
				Fetch: getPage(cmd.Context(), path, nil),
				Parse: parseList[json.RawMessage],
			})
		},
	}
}

func (k mcpItemKind) approveCmd(short, long string) *cobra.Command {
	return &cobra.Command{
		Use:   "approve <" + k.noun + "-id>...",
		Short: short,
		Long:  long,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID, connectorID := scopeIDs(cmd)
			state, err := k.state.parse(mustGetString(cmd, "state"))
			if err != nil {
				return err
			}
			if state == "" {
				state = k.stateAPI("approved")
			}
			// REMOVED is sync's "gone upstream" signal; setting it by hand
			// would contaminate that meaning.
			if state == k.stateAPI("removed") {
				return &usageError{fmt.Errorf("--state=removed is system-managed by sync; %s", k.removedAdvice)}
			}
			ms := make([]mutation, len(args))
			for i, id := range args {
				ms[i] = mutation{
					Method: http.MethodPost,
					Path:   k.itemPath(appID, connectorID, id),
					Body: wrapUpdate(k.envelope, map[string]any{
						"id":          id,
						"appId":       appID,
						"connectorId": connectorID,
						"state":       state,
					}, "state"),
				}
			}
			return sendMutationBatch(cmd, ms, "id", func(i int) string { return args[i] }, withBatchWording("approved", k.noun))
		},
	}
}

// The register* methods add a builder's flags and attach it to parent; call
// them from init, after the package's flag registries exist.

func (k mcpItemKind) registerGet(parent, c *cobra.Command) { k.attach(parent, c) }

func (k mcpItemKind) registerHistory(parent, c *cobra.Command) {
	addPaginationFlagsWithMax(c, defaultPageSize, maxHistoryPageSize)
	k.attach(parent, c)
}

func (k mcpItemKind) registerApprove(parent, c *cobra.Command) {
	c.Flags().String("state", "", "Target state (default approved): "+k.state.without("removed").usage())
	allowEmpty(c, emptyIsOmitted, "state")
	markMutating(c)
	k.attach(parent, c)
}

func (k mcpItemKind) attach(parent, c *cobra.Command) {
	addMCPScopeFlags(c)
	markRequired(c, "app-id", "connector-id")
	parent.AddCommand(c)
}
