package cmd

import (
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const (
	syncHistoryMaxRuns = 50
	syncErrorsMaxTopK  = 20
)

// boundedCountFlag reads a plain count flag: negative is a usage error before
// any request, over max is clamped, and 0 or unset means omit (server default).
// These flags are not pagination (one window, no cursor), so they bypass the
// --page-size registrar.
func boundedCountFlag(cmd *cobra.Command, name string, max int) (int, error) {
	n := getIntFlag(cmd, name)
	if n < 0 {
		return 0, &usageError{fmt.Errorf("--%s cannot be negative (got %d); 0 means the server's default", name, n)}
	}
	if n > max {
		return max, nil
	}
	return n, nil
}

func syncDiagPath(cmd *cobra.Command, connectorID, tail string) string {
	appID, _ := cmd.Flags().GetString("app-id")
	return connectorPath(appID, connectorID) + "/sync_diagnostics/" + tail
}

func syncDiagClient(cmd *cobra.Command) (*client.Client, error) {
	baseURL, err := GetBaseURL()
	if err != nil {
		return nil, err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	return c, nil
}

var connectorsSyncHistoryCmd = &cobra.Command{
	Use:   "sync-history <connector-id>",
	Short: "Show a connector's recent sync runs and failure streak (pretty JSON)",
	Long: `Show a connector's recent sync runs, newest first, and the failure trend
within that window (.streak: failedRuns, finishedRuns, failureRatio, and the
first failure after the last success). Each run has its status, timing,
errorClass, a redacted errorSummary and, when recorded, syncRunStats object
counts; the counts are int64 values printed as JSON strings. A run's
.lifecycleId is what "connectors sync-compare" takes.

--runs asks for up to 50 runs (the server's documented max; a larger value is
clamped to 50, a negative one exits 2); unset, the server returned 20 in the
runs checked. There is no next page.

Output is the API's response object as returned.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n, err := boundedCountFlag(cmd, "runs", syncHistoryMaxRuns)
		if err != nil {
			return err
		}
		var q map[string]string
		if n > 0 {
			q = map[string]string{"page_size": strconv.Itoa(n)}
		}
		return getSyncDiag(cmd, syncDiagPath(cmd, args[0], "history"), q)
	},
}

var connectorsSyncErrorsCmd = &cobra.Command{
	Use:   "sync-errors <connector-id>",
	Short: "Show a connector's most frequent recent sync errors (pretty JSON)",
	Long: `Show a bounded digest of a connector's recent sync errors: distinct redacted
messages with errorClass, count, firstSeenAt, lastSeenAt and step. Per the API
description, counts come from a bounded scan, not an exhaustive log count.

--top-k limits the distinct messages (max 20, the server's documented bound; a
larger value is clamped to 20, a negative one exits 2).

Output is the API's response object as returned.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n, err := boundedCountFlag(cmd, "top-k", syncErrorsMaxTopK)
		if err != nil {
			return err
		}
		var q map[string]string
		if n > 0 {
			q = map[string]string{"top_k": strconv.Itoa(n)}
		}
		return getSyncDiag(cmd, syncDiagPath(cmd, args[0], "error_digest"), q)
	},
}

func getSyncDiag(cmd *cobra.Command, path string, query map[string]string) error {
	c, err := syncDiagClient(cmd)
	if err != nil {
		return err
	}
	data, err := c.Get(cmd.Context(), path, query)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	return writeObject(cmd, data)
}

func postSyncDiag(cmd *cobra.Command, path string, body map[string]any) error {
	c, err := syncDiagClient(cmd)
	if err != nil {
		return err
	}
	data, err := c.Post(cmd.Context(), path, body)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	return writeObject(cmd, data)
}

var connectorsSyncChangesCmd = &cobra.Command{
	Use:   "sync-changes <connector-id>",
	Short: "Show metadata changes around a connector's sync failures (pretty JSON)",
	Long: `Show a timeline of configuration, release and deployment changes for a
connector. Per the API description it is metadata only (changed field names,
never values), oldest first, capped to the most recent 100 events in the
window. .truncatedSources names any source that was incomplete.

--since (an RFC 3339 timestamp, e.g. 2026-01-01T00:00:00Z) starts the window;
unset, the server chose the 7 days before now in the run checked. A --since
in the future was a 400 ("timeline: since is in the future") in the run
checked, and a value that is not a timestamp a 400 (both exit 2).

Output is the API's response object as returned.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		// A blank --since is refused centrally, so non-empty means set.
		if since, _ := cmd.Flags().GetString("since"); since != "" {
			body["since"] = since
		}
		return postSyncDiag(cmd, syncDiagPath(cmd, args[0], "change_timeline"), body)
	},
}

var connectorsSyncCompareCmd = &cobra.Command{
	Use:   "sync-compare <connector-id>",
	Short: "Compare record counts between two sync runs (pretty JSON)",
	Long: `Compare the recorded aggregate counts of two production sync runs of a
connector: --left is the before run, --right the after run (lifecycle ids from
"connectors sync-history"). Output is the API's response: .perType (resource,
entitlement and grant before/after/delta per resource type, counts as JSON
strings), .typesAdded, .typesRemoved, .largeDecrease (types whose count fell
by at least 30 percent, per the API description) and .c1zSizeDelta. It holds
counts only, no synced records. A run compared with itself was accepted in
the run checked (all deltas 0).

The API also compares a run against a draft test run of a tenant-authored
connector: send that request with --body-file (a JSON object, mutually
exclusive with --left/--right) using the draftTestRun object (catalogId,
revisionId, testRunId). That form was not exercised live.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := syncDiagPath(cmd, args[0], "compare_stats")
		file, _ := cmd.Flags().GetString("body-file")
		if file != "" {
			left, _ := cmd.Flags().GetString("left")
			right, _ := cmd.Flags().GetString("right")
			if left != "" || right != "" {
				return &usageError{fmt.Errorf("--body-file is mutually exclusive with --left/--right")}
			}
			body, err := readJSONFile[map[string]any](cmd, "body-file", file)
			if err != nil {
				return err
			}
			return postSyncDiag(cmd, path, body)
		}
		if err := requireNonEmpty(cmd, "left", "right"); err != nil {
			return err
		}
		left, _ := cmd.Flags().GetString("left")
		right, _ := cmd.Flags().GetString("right")
		return postSyncDiag(cmd, path, map[string]any{"leftLifecycleId": left, "rightLifecycleId": right})
	},
}

var connectorsDebuggableCmd = &cobra.Command{
	Use:   "debuggable",
	Short: "List connectors that have sync diagnostics (NDJSON output)",
	Long: `List the connectors you may run sync diagnostics on, across all apps: one row
per connector with appId, connectorId, displayName, catalogId, authored,
syncStatus and lastCompletedAt, as the API returns them. Use the appId and
connectorId with "connectors sync-history" and the other sync-* commands.

The server's page size max here is 50 (it answered 400 above that). Per the
API description a page can be empty while more remain; c1i keeps following
the cursor.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		const path = "/api/v1/sync_diagnostics/connectors"
		return listMapPages(cmd, "GET "+path, "connectors", []string{"appId", "connectorId"}, identityRow, getPage(cmd.Context(), path, nil))
	},
}

func init() {
	for _, c := range []*cobra.Command{connectorsSyncHistoryCmd, connectorsSyncErrorsCmd, connectorsSyncChangesCmd, connectorsSyncCompareCmd} {
		c.Flags().String("app-id", "", "Application ID")
		markRequired(c, "app-id")
		connectorsCmd.AddCommand(c)
	}
	connectorsSyncHistoryCmd.Flags().Int("runs", 0, "Maximum runs to return (max 50; 0 = the server's default)")
	connectorsSyncErrorsCmd.Flags().Int("top-k", 0, "Maximum distinct error messages (max 20; 0 = the server's default)")
	connectorsSyncChangesCmd.Flags().String("since", "", "Start of the window, an RFC 3339 timestamp (default: the server's)")
	connectorsSyncCompareCmd.Flags().String("left", "", "Lifecycle id of the before run (required unless --body-file)")
	connectorsSyncCompareCmd.Flags().String("right", "", "Lifecycle id of the after run (required unless --body-file)")
	connectorsSyncCompareCmd.Flags().String("body-file", "", "Full CompareSyncStats request JSON object (file or \"-\" for stdin); replaces --left/--right, e.g. for draftTestRun")
	annotateRequired(connectorsSyncCompareCmd, "left", "right")
	addPaginationFlagsWithMax(connectorsDebuggableCmd, defaultPageSize, 50)
	connectorsCmd.AddCommand(connectorsDebuggableCmd)
}
