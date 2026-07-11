package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// mapSortDirection converts the user-facing --sort value to the API enum.
// Empty defaults to ascending (chronological), which is the natural order for
// archiving and for incremental sync via --since-event-uid.
func mapSortDirection(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "asc":
		return "SORT_DIRECTION_ASC", nil
	case "desc":
		return "SORT_DIRECTION_DESC", nil
	default:
		return "", fmt.Errorf(`--sort must be "asc" or "desc"`)
	}
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
  c1i export events --since-event-uid <last-event-uid>`,
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
		sortDirection, err := mapSortDirection(sortFlag)
		if err != nil {
			return &usageError{err}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd.OutOrStdout())
		emitted := 0
		for !limitReached(emitted, limit) {
			body := exportEventsBody(
				effectivePageSize(requestedPageSize, limit, emitted),
				pageToken, since, until, sinceEventUID, sortDirection,
			)

			data, err := c.Post(cmd.Context(), "/api/v1/systemlog/events", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []json.RawMessage `json:"list"`
				NextPageToken string            `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, event := range resp.List {
				// Emit each OCSF event verbatim (one NDJSON line). The emitter
				// still honors --fields for callers who want to trim events.
				_ = enc.Encode(event)
				emitted++
				if limitReached(emitted, limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			pageToken = resp.NextPageToken
		}

		return nil
	},
}

func init() {
	exportEventsCmd.Flags().String("since", "", "Only events at or after this RFC3339 time (e.g. 2026-07-01T00:00:00Z)")
	exportEventsCmd.Flags().String("until", "", "Only events before this RFC3339 time")
	exportEventsCmd.Flags().String("since-event-uid", "", "Resume after this event UID (incremental sync)")
	exportEventsCmd.Flags().String("sort", "asc", "Chronological order: asc (oldest first) or desc")
	exportEventsCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	exportEventsCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(exportEventsCmd)
	exportCmd.AddCommand(exportEventsCmd)
}
