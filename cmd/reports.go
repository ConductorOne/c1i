package cmd

import (
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var reportsCmd = &cobra.Command{
	Use:   "reports",
	Short: "Manage saved reports (list, get, update, delete, run, program, provenance)",
	Long: `Saved reports of the C1.ai reporting service: a question, the program that
answers it, and the parameters a re-run may vary. There is no create command:
the API saves a report only from an already-rendered reporting surface, which
needs a session in the C1.ai UI. Per the API, reports are not scoped by author.

Not to be confused with "access-reviews reports".

These commands were checked against a tenant with no reports, so only the
unknown-id and validation paths were exercised live; the rest follow the API
description.`,
}

func reportPath(id string) string { return client.Path("/api/v1/reporting/reports/%s", id) }

var reportIDKeys = []string{"id"}

var reportsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved reports (NDJSON output)",
	Long: `List saved reports, newest first as the API describes it, one row per report
as returned (including latestRunSummary, whose artifactUrl may be a signed
URL; not observed).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listMapPages(cmd, "GET /api/v1/reporting/reports", "list", reportIDKeys, identityRow,
			getPage(cmd.Context(), "/api/v1/reporting/reports", nil))
	},
}

var reportsGetCmd = &cobra.Command{
	Use:   "get <report-id>",
	Short: "Get a saved report with its latest runs (pretty JSON)",
	Long: `Get one report, including its latest run and latest successful run. A run's
artifactUrl may be a signed URL (not observed); treat it as a secret. Prints
the report unwrapped: the id is .id; latestRun, latestSuccessfulRun and
promptDrifted stay top-level siblings.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), reportPath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeNamedResource(cmd, data, "report")
	},
}

var reportsDeleteCmd = &cobra.Command{
	Use:   "delete <report-id>",
	Short: "Delete a saved report",
	Long: `Delete a report by id. Per the API, its saved program is removed with it, so it
can no longer be re-run, and any caller holding the permission may delete any
report in the tenant. Whether the delete is soft was not observed. Prints the
API's response, {}. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: reportPath(args[0])})
	},
}

var reportsRunCmd = &cobra.Command{
	Use:   "run <report-id>",
	Short: "Re-run a saved report's program (pretty JSON)",
	Long: `Re-run the report's pinned program against current data. Per the API it never
re-plans, and returns as soon as the invocation starts: the printed run has a
phase and status that are not final. Poll with "reports get <report-id>".
The run's artifactUrl may be a signed URL (not observed): if writing the
response fails, c1i says it was not delivered. Honors --dry-run.

Prints the run unwrapped: the id is .id.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: reportPath(args[0]) + "/run", Body: map[string]any{}, Unwrap: named("run")},
			secretBearing(secretReportRun))
	},
}

var reportsProgramCmd = &cobra.Command{
	Use:   "program <report-id>",
	Short: "Show a report's saved program source (pretty JSON)",
	Long: `Show the report's durable source: a programId and a map of file names to
contents (can be large). --program-id is required: the API answers its absence
with a 400 ("value length must be between 1 and 100 runes").`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), reportPath(args[0])+"/program", map[string]string{"program_id": mustGetString(cmd, "program-id")})
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeObject(cmd, data)
	},
}

var reportsProvenanceCmd = &cobra.Command{
	Use:   "provenance <run-id>",
	Short: "Explain one report run: what it read and its program (pretty JSON)",
	Long: `Explain a run of --report-id: its sources, pinned program id, input and program
source, as the API returns them.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		path := reportPath(mustGetString(cmd, "report-id")) + client.Path("/runs/%s/provenance", args[0])
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeObject(cmd, data)
	},
}

var reportUpdateSpec = &updateSpec{fields: []updateField{
	strUpdate("display-name", "displayName"),
	strUpdate("description", "description"),
	strUpdate("prompt", "prompt"),
}}

var reportsUpdateCmd = &cobra.Command{
	Use:   "update <report-id>",
	Short: "Update a report's name, description or prompt (pretty JSON)",
	Long: `Update --display-name, --description or --prompt. Per the API an empty field
leaves the stored value alone and at least one must be set, so an empty value
is a usage error here. Editing --prompt does not re-plan the report; it may
drift from the program's planned prompt. Parameter values are not editable
here. Prints the report unwrapped: the id is .id. The response's runs may
carry a signed artifactUrl (not observed).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		obj := map[string]any{}
		paths, err := reportUpdateSpec.apply(cmd, obj)
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			return reportUpdateSpec.nothingToUpdate()
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: reportPath(args[0]), Body: obj, Unwrap: named("report")},
			secretBearing(secretReportRun))
	},
}

func init() {
	addPaginationFlags(reportsListCmd)
	reportsProgramCmd.Flags().String("program-id", "", "Program id to show")
	markRequired(reportsProgramCmd, "program-id")
	reportsProvenanceCmd.Flags().String("report-id", "", "Report the run belongs to")
	markRequired(reportsProvenanceCmd, "report-id")

	u := reportsUpdateCmd.Flags()
	u.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	u.String("description", "", "New description (omit to leave unchanged)")
	u.String("prompt", "", "New prompt (omit to leave unchanged)")
	registerUpdateSpec(reportsUpdateCmd, reportUpdateSpec)
	markMutating(reportsUpdateCmd)
	markMutating(reportsDeleteCmd)
	markMutating(reportsRunCmd)

	reportsCmd.AddCommand(reportsListCmd, reportsGetCmd, reportsUpdateCmd, reportsDeleteCmd, reportsRunCmd, reportsProgramCmd, reportsProvenanceCmd)
	rootCmd.AddCommand(reportsCmd)
}
