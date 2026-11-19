package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var connectorsSyncCmd = &cobra.Command{
	Use:   "sync <connector-id>",
	Short: "Start a sync of a connector now",
	Long: `Start a sync of a connector now. The API queues the sync; exit 0 means it
was accepted, not that it finished. Follow progress with
"c1i connectors sync-status". On a paused connector the API can accept the call
but queue nothing; resume the connector first.

Prints the API's response, which is {}. Honors --dry-run.

--wait polls the connector (the same read as "c1i connectors get") until its
sync status reaches a terminal state with a .status.completedAt later than the
moment this command read it, just before sending the request -- so a sync that
was already done when this command ran, and hasn't advanced yet, cannot look
like this sync finished. It then prints the connector's whole .status object
as this command's document, in place of the API's empty {}; a 200 that does
not hold connectorView.connector exits 6 rather than waiting (a connector
that has never synced reports SYNC_STATUS_UNSPECIFIED with null timestamps, or
omits .status; either way it is waited on normally). A sync
that ends in SYNC_STATUS_ERROR or with .status.lastError set exits 1 naming
it; the sync was still accepted (applied: true). A status that never advances
.status.completedAt (an unconfigured connector stays SYNC_STATUS_DONE with
null timestamps, and a sync the server refuses before it starts stays
RUNNING) is reported at the timeout, not as a sync error: the wait cannot
tell it from a sync that has not started yet. A timeout also exits 1 with
applied: true, and the sync may still be running. --dry-run never polls or
reads the baseline.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID := args[0]
		path := connectorPath(appID, connectorID)
		opts := []mutationOpt{}
		var baseline string
		if wait && !dryRunActive() {
			opts = append(opts, withPreRead(connectorSyncBaselinePreRead(path, &baseline)))
		}
		opts = append(opts,
			withResponse(func(c *client.Client, _ []byte) writeOutcome {
				return writeOutcome{Wait: connectorSyncWaitOp(c, path, connectorID, appID, &baseline, waitTimeout)}
			}, wait),
			withWaitResultOutput())
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path + "/force_sync", Body: map[string]any{}}, opts...)
	},
}

// connectorSyncWaitInterval is how often "connectors sync --wait" re-reads
// the connector. A var so a test can poll fast.
var connectorSyncWaitInterval = 5 * time.Second

// connectorSyncStatus is what the wait reads from a connector's .status
// (connectorView.connector.status) to decide whether the sync this command
// started has finished. The printed document is the server's whole .status
// object, not this struct.
type connectorSyncStatus struct {
	Status      string `json:"status"`
	CompletedAt string `json:"completedAt"`
	LastError   string `json:"lastError"`
}

// connectorSyncStatusError is a terminal failure even when lastError is
// empty. errorKind cannot stand in for it: the API sends
// "ERROR_KIND_UNSPECIFIED" (its enum's zero value) on a clean DONE, not "".
const connectorSyncStatusError = "SYNC_STATUS_ERROR"

// connectorSyncRead is one --wait read: the fields the wait decides on, and
// the server's .status object as sent, which --wait prints on success.
type connectorSyncRead struct {
	status connectorSyncStatus
	raw    json.RawMessage
}

// readConnectorSyncStatus GETs the connector at path and returns its .status
// object, the same read "connectors get" prints as .status. A response that
// does not hold connectorView.connector is a C1 failure, not "still syncing":
// otherwise a changed envelope would pass for a 15-minute wait. A connector
// with no .status yet (never synced) reads as nothing completed, so the first
// sync can still be waited on.
func readConnectorSyncStatus(ctx context.Context, c *client.Client, path string) (connectorSyncRead, error) {
	data, err := c.Get(ctx, path, nil)
	if err != nil {
		return connectorSyncRead{}, fmt.Errorf("reading the connector: %w", err)
	}
	var env struct {
		ConnectorView struct {
			Connector json.RawMessage `json:"connector"`
		} `json:"connectorView"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return connectorSyncRead{}, &nonJSONResponseError{fmt.Errorf("the connector response is not the expected shape: %w", err)}
	}
	rawConnector := bytes.TrimSpace(env.ConnectorView.Connector)
	if len(rawConnector) == 0 || string(rawConnector) == "null" {
		return connectorSyncRead{}, &nonJSONResponseError{fmt.Errorf("the connector response held no connectorView.connector")}
	}
	var conn struct {
		Status json.RawMessage `json:"status"`
	}
	if err := json.Unmarshal(rawConnector, &conn); err != nil {
		return connectorSyncRead{}, &nonJSONResponseError{fmt.Errorf("connectorView.connector is not an object: %w", err)}
	}
	raw := bytes.TrimSpace(conn.Status)
	if len(raw) == 0 || string(raw) == "null" {
		return connectorSyncRead{}, nil
	}
	var st connectorSyncStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return connectorSyncRead{}, &nonJSONResponseError{fmt.Errorf("the connector's status is not JSON: %w", err)}
	}
	return connectorSyncRead{status: st, raw: raw}, nil
}

// completedAtAdvanced reports whether next is a later completedAt than base.
// An empty next is never advanced; a non-empty next beats an empty base.
// Unparsable timestamps fall back to a plain inequality check. This is what
// keeps a sync that was already DONE before this command ran -- whose
// completedAt hasn't moved yet -- from ending the wait early.
func completedAtAdvanced(base, next string) bool {
	if next == "" {
		return false
	}
	if base == "" {
		return true
	}
	bt, berr := time.Parse(time.RFC3339, base)
	nt, nerr := time.Parse(time.RFC3339, next)
	if berr == nil && nerr == nil {
		return nt.After(bt)
	}
	return next != base
}

// connectorSyncBaselinePreRead records the connector's current completedAt
// before the sync is requested, so --wait can require a result from THIS
// sync rather than one already on the connector.
func connectorSyncBaselinePreRead(path string, dst *string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		read, err := readConnectorSyncStatus(ctx, c, path)
		if err != nil {
			return nil, fmt.Errorf("reading the connector's current sync status before the request: %w", err)
		}
		*dst = read.status.CompletedAt
		return nil, nil
	}}
}

// connectorSyncWaitOp polls the connector until a sync started after
// *baseline completes. A terminal failure stops the wait with that error
// immediately: a non-empty lastError names what went wrong, and
// SYNC_STATUS_ERROR is terminal even without one. The satisfying value is the
// server's .status object as sent, which sendMutation's withWaitResultOutput
// prints as the command's document. A failure the server records without
// advancing completedAt is indistinguishable from a stale one and surfaces at
// the timeout instead.
func connectorSyncWaitOp(c *client.Client, path, connectorID, appID string, baseline *string, timeout time.Duration) waitOp[[]string] {
	return waitOp[[]string]{
		Poll: func(ctx context.Context) ([]string, error) {
			read, err := readConnectorSyncStatus(ctx, c, path)
			if err != nil {
				return nil, err
			}
			st := read.status
			if !completedAtAdvanced(*baseline, st.CompletedAt) {
				// A status whose completedAt never advances (an unconfigured
				// connector stays DONE with null timestamps) looks the same at
				// every poll as one whose queued sync has not started, so the
				// wait can only end at the timeout.
				return []string{}, nil
			}
			if st.Status == connectorSyncStatusError || st.LastError != "" {
				return nil, fmt.Errorf("connector %s sync ended in status %s%s", connectorID, st.Status, lastErrorSuffix(st.LastError))
			}
			return []string{string(read.raw)}, nil
		},
		Done:     func(v []string) bool { return len(v) == 1 },
		Interval: connectorSyncWaitInterval,
		Timeout:  timeout,
		Subject:  "connector " + connectorID + " to finish a new sync",
		Success:  "Connector " + connectorID + " sync finished",
		Slow:     "a full sync can take minutes depending on the app's size",
		Recheck:  "c1i connectors get " + connectorID + " --app-id " + appID,
	}
}

// lastErrorSuffix appends the server's error text when it reported one.
func lastErrorSuffix(lastError string) string {
	if lastError == "" {
		return ""
	}
	return ": " + lastError
}

var connectorsPauseCmd = &cobra.Command{
	Use:   "pause <connector-id>",
	Short: "Pause a connector's syncing and provisioning",
	Long: `Pause syncing and provisioning for a connector. The API documents that no
new syncs or grant/revoke operations run until "c1i connectors resume".

While paused, "connectors get" shows .syncDisabledAt set and .canResumeSync
true. Pausing a paused connector exits 0.

Prints the API's response, which is {}. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendConnectorAction(cmd, args[0], "/pause")
	},
}

var connectorsResumeCmd = &cobra.Command{
	Use:   "resume <connector-id>",
	Short: "Resume a paused connector",
	Long: `Resume syncing and provisioning for a paused connector. The API documents
that resuming also starts a new sync. Resuming a connector that is not paused
exits 0.

Prints the API's response, which is {}. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendConnectorAction(cmd, args[0], "/resume")
	},
}

// sendConnectorAction POSTs an empty body to a per-connector action route.
func sendConnectorAction(cmd *cobra.Command, connectorID, suffix string) error {
	appID, _ := cmd.Flags().GetString("app-id")
	path := connectorPath(appID, connectorID) + suffix
	return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: map[string]any{}})
}

var connectorsScheduleCmd = &cobra.Command{
	Use:   "schedule <connector-id>",
	Short: "Set or clear a connector's sync schedule",
	Long: `Set a connector's sync schedule to a cron expression, or clear it.

  c1i connectors schedule <connector-id> --app-id <app-id> --cron "0 */6 * * *" --timezone America/Chicago
  c1i connectors schedule <connector-id> --app-id <app-id> --clear

--timezone is an IANA name; omitted, none is sent and "connectors get" shows
it as "". c1i does not validate --cron or --timezone, and in the run checked
the server accepted values that are neither. --clear sends no schedule, and
"connectors get" then shows .connectorSyncCronSchedule as null.
--justification is sent as the change's justification.reason.

Prints the API's response, which is {}. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildConnectorScheduleBody(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		path := connectorPath(appID, args[0]) + "/schedule"
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body})
	},
}

func buildConnectorScheduleBody(cmd *cobra.Command) (map[string]any, error) {
	f := cmd.Flags()
	clearSchedule, _ := f.GetBool("clear")
	cronGiven, tzGiven := flagGiven(cmd, "cron"), flagGiven(cmd, "timezone")
	body := map[string]any{}
	switch {
	case clearSchedule && (cronGiven || tzGiven):
		return nil, &usageError{errors.New("--clear is mutually exclusive with --cron/--timezone")}
	case clearSchedule:
	case !cronGiven:
		return nil, &usageError{errors.New("pass --cron (with optional --timezone), or --clear")}
	default:
		spec, _ := f.GetString("cron")
		cron := map[string]any{"cronSpec": spec}
		if tzGiven {
			tz, _ := f.GetString("timezone")
			cron["timezone"] = tz
		}
		body["cron"] = cron
	}
	addConnectorJustification(cmd, body)
	return body, nil
}

// addConnectorJustification sets the change-control reason when given.
func addConnectorJustification(cmd *cobra.Command, body map[string]any) {
	if reason, _ := cmd.Flags().GetString("justification"); flagGiven(cmd, "justification") {
		body["justification"] = map[string]any{"reason": reason}
	}
}

const connectorJustificationUsage = "Reason recorded for the change (sent as justification.reason)"

func init() {
	for _, c := range []*cobra.Command{connectorsSyncCmd, connectorsPauseCmd, connectorsResumeCmd, connectorsScheduleCmd} {
		c.Flags().String("app-id", "", "Application ID")
		markRequired(c, "app-id")
		markMutating(c)
		connectorsCmd.AddCommand(c)
	}
	addWaitFlags(connectorsSyncCmd, "GET /api/v1/apps/{app-id}/connectors/{id} until a new sync's status.completedAt appears", 15*time.Minute)
	f := connectorsScheduleCmd.Flags()
	f.String("cron", "", "Cron expression for the sync schedule (required unless --clear)")
	f.String("timezone", "", "IANA timezone for --cron, e.g. America/Chicago")
	f.Bool("clear", false, "Remove the sync schedule")
	f.String("justification", "", connectorJustificationUsage)
}
