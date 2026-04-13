package cmd

import "github.com/spf13/cobra"

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Explore the C1 API and documentation (no auth required)",
	Long: `Explore the C1 API reference and documentation — no credentials needed.

AGENTS: Use these commands FIRST to discover API endpoints and understand their
request/response schemas before making authenticated API calls.

  docs search <query>            Search documentation by keyword
  docs page <path>               Fetch a full documentation page
  docs endpoints [--filter <p>]  List all API endpoints (filterable)
  docs endpoint <path>           Show full schema for a specific endpoint
  docs openapi                   Dump the raw OpenAPI spec`,
}

func init() {
	rootCmd.AddCommand(docsCmd)
}
