package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var functionsCommitsCmd = &cobra.Command{
	Use:   "commits <function-id>",
	Short: "List a function's commit history (NDJSON output)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		functionID := args[0]
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		path := client.Path("/api/v1/functions/%s/commits", functionID)
		return listPages(cmd, c, pageSpec[functionCommit]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[functionCommit],
			ID:    func(it functionCommit) string { return it.ID },
			Rows: rowFn(func(c functionCommit) map[string]any {
				return map[string]any{
					"id":          nilIfEmpty(c.ID),
					"function_id": nilIfEmpty(c.FunctionID),
					"author":      nilIfEmpty(c.Author),
					"message":     nilIfEmpty(c.Message),
					"created_at":  nilIfEmpty(c.CreatedAt),
				}
			}),
		})
	},
}

// functionCommit is the subset of a commit that functions commits prints.
type functionCommit struct {
	ID         string `json:"id"`
	FunctionID string `json:"functionId"`
	Author     string `json:"author"`
	Message    string `json:"message"`
	CreatedAt  string `json:"createdAt"`
}

func init() {
	addPaginationFlags(functionsCommitsCmd)
	functionsCmd.AddCommand(functionsCommitsCmd)
}
