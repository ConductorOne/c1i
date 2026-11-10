package cmd

import "github.com/spf13/cobra"

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Explore the C1 API and documentation (no auth required)",
	Long: `Explore the C1 API reference and documentation — no credentials needed.

AGENTS: start with "docs agents" for the conventions --help can't tell you.
Use the rest to explore endpoints and schemas when no first-class command
covers what you need yet.

  docs agents [-o FILE]          Bootstrap doc: output contracts, exit codes, when to use "api"
  docs readme [-o FILE]          Print the embedded README (the full reference)
  docs changelog [-o FILE]       Print the embedded CHANGELOG (migration notes)
  docs search <query>            Search documentation by keyword
  docs page <path>               Fetch a full documentation page
  docs endpoints [--filter <p>]  List all API endpoints (filterable)
  docs endpoint <path>           Show full schema for a specific endpoint
  docs openapi                   Dump the raw OpenAPI spec
  docs guide [name]              Print an embedded task-oriented runbook (list names if omitted)

"docs skill" is kept as an alias of "docs agents" for backward compatibility.`,
}

func init() {
	rootCmd.AddCommand(docsCmd)
}
