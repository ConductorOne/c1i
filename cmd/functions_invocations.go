package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var functionsInvocationsCmd = &cobra.Command{
	Use:   "invocations",
	Short: "Inspect a function's invocations",
}

var functionsInvocationsListCmd = &cobra.Command{
	Use:   "list <function-id>",
	Short: "List a function's invocations, newest first (NDJSON output)",
	Long: `List a function's invocations, newest first, one NDJSON row each: id,
function_id, commit_id, status, error, created_at and updated_at. Input and
output are left out of the rows; read them with "functions invocations get".

A busy function can have tens of thousands of invocations and this command
pages through all of them; pass --limit for the most recent N. The search
response carries each invocation's full input and output, and the API asks for
a small page size; use --page-size 10 when payloads may be large.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		// The search route, not GET .../invocations: the GET ignores
		// page_size and lists oldest first.
		path := client.Path("/api/v1/functions/%s/invocations/search", args[0])
		return listPages(cmd, c, pageSpec[functionInvocation]{
			Route: "POST " + path,
			Fetch: postPage(cmd.Context(), path, map[string]any{}),
			Parse: parseList[functionInvocation],
			ID:    func(it functionInvocation) string { return it.ID },
			Rows: rowFn(func(it functionInvocation) map[string]any {
				return map[string]any{
					"id":          nilIfEmpty(it.ID),
					"function_id": nilIfEmpty(it.FunctionID),
					"commit_id":   nilIfEmpty(it.CommitID),
					"status":      nilIfEmpty(it.Status),
					"error":       nilIfEmpty(it.Error),
					"created_at":  nilIfEmpty(it.CreatedAt),
					"updated_at":  nilIfEmpty(it.UpdatedAt),
				}
			}),
		})
	},
}

// functionInvocation is the subset of an invocation that the list prints.
type functionInvocation struct {
	ID         string `json:"id"`
	FunctionID string `json:"functionId"`
	CommitID   string `json:"commitId"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

var functionsInvocationsGetCmd = &cobra.Command{
	Use:   "get <invocation-id>",
	Short: "Get one invocation, with its input, output and error (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		functionID, _ := cmd.Flags().GetString("function-id")
		return getResource(cmd, client.Path("/api/v1/functions/%s/invocations/%s", functionID, args[0]), nil, "id", "invocation")
	},
}

func init() {
	addPaginationFlags(functionsInvocationsListCmd)
	functionsInvocationsGetCmd.Flags().String("function-id", "", "Function the invocation belongs to (required)")
	markRequired(functionsInvocationsGetCmd, "function-id")
	functionsInvocationsCmd.AddCommand(functionsInvocationsListCmd, functionsInvocationsGetCmd)
	functionsCmd.AddCommand(functionsInvocationsCmd)
}
