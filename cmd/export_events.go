package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var sortDirectionFlag = enumFlag{flag: "sort", prefix: "SORT_DIRECTION_", unknown: enumReject, values: []enumValue{
	{name: "asc", api: "SORT_DIRECTION_ASC"},
	{name: "desc", api: "SORT_DIRECTION_DESC"},
}}

// parseSortDirection reads --sort. An empty value (the flag omitted) is
// ascending (chronological), the natural order for archiving and for
// incremental sync via --since-event-uid; a whitespace-only value is refused
// by the parser instead of quietly falling back to it.
func parseSortDirection(s string) (string, error) {
	if s == "" {
		return "SORT_DIRECTION_ASC", nil
	}
	return sortDirectionFlag.parse(s)
}

// exportEventsBody builds the /api/v1/systemlog/events request body. Kept
// separate from flag handling so it can be unit-tested.
func exportEventsBody(pageSize int, pageToken, since, until, sinceEventUID, sortDirection string) map[string]any {
	body := map[string]any{"pageSize": pageSize}
	if pageToken != "" {
		body["pageToken"] = pageToken
	}
	if since != "" {
		body["since"] = since
	}
	if until != "" {
		body["until"] = until
	}
	if sinceEventUID != "" {
		body["sinceEventUid"] = sinceEventUID
	}
	if sortDirection != "" {
		body["sortDirection"] = sortDirection
	}
	return body
}

// validateRFC3339 rejects a non-empty timestamp that isn't RFC3339, turning a
// server-side 4xx into an upfront usage error with a helpful example.
func validateRFC3339(flag, value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return &usageError{fmt.Errorf("%s must be an RFC3339 timestamp (e.g. 2026-07-01T00:00:00Z): %q", flag, value)}
	}
	return nil
}

var exportEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Export system log (audit) events as NDJSON",
	Long: `Export C1 system log events — OCSF-formatted audit events — as an NDJSON
stream, one JSON event per line, auto-paginating through the full result set.

This is the bulk audit-log dump: redirect it to a file to archive events or ship
them to an external system.

  # Everything, oldest first, to a file
  c1i export events > audit.ndjson

  # A specific time window (RFC3339 timestamps)
  c1i export events --since 2026-07-01T00:00:00Z --until 2026-07-08T00:00:00Z

  # Incremental sync: resume after the last event you already stored
  c1i export events --since-event-uid <last-event-uid>

Both timestamps must sit within about one year of now: the server rejects a
future --until or a --since older than roughly 365 days with a 400 ("value must
be less than now within 8761h6m40s").`,
	RunE: func(cmd *cobra.Command, args []string) error {
		since, _ := cmd.Flags().GetString("since")
		until, _ := cmd.Flags().GetString("until")
		sinceEventUID, _ := cmd.Flags().GetString("since-event-uid")
		sortFlag, _ := cmd.Flags().GetString("sort")

		if err := validateRFC3339("--since", since); err != nil {
			return err
		}
		if err := validateRFC3339("--until", until); err != nil {
			return err
		}
		sortDirection, err := parseSortDirection(sortFlag)
		if err != nil {
			return err
		}

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		// An audit-log row carries no id, and a quiet stretch can page through many
		// empty results, so the guard is uncapped.
		return listPages(cmd, c, pageSpec[json.RawMessage]{
			Route:    "POST /api/v1/systemlog/events",
			Uncapped: true,
			Fetch: postPageBody(cmd.Context(), "/api/v1/systemlog/events", func(size int, token string) map[string]any {
				return exportEventsBody(size, token, since, until, sinceEventUID, sortDirection)
			}),
			Parse: parseList[json.RawMessage],
		})
	},
}

func init() {
	exportEventsCmd.Flags().String("since", "", "Only events at or after this RFC3339 time (e.g. 2026-07-01T00:00:00Z)")
	exportEventsCmd.Flags().String("until", "", "Only events before this RFC3339 time")
	exportEventsCmd.Flags().String("since-event-uid", "", "Resume after this event UID (incremental sync)")
	exportEventsCmd.Flags().String("sort", "asc", "Sort order (asc is oldest first): "+sortDirectionFlag.usage())
	allowEmpty(exportEventsCmd, emptyIsOmitted, "sort")
	addPaginationFlags(exportEventsCmd)
	exportCmd.AddCommand(exportEventsCmd)
}
