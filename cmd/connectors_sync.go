package cmd

import (
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
like this sync finished. The connector's status object is then printed as a
second document on stdout. A sync that ends with .status.lastError set exits 1
naming it; the sync was still accepted (applied: true). A timeout also exits 1
with applied: true, and the sync may still be running. --dry-run never polls
or reads the baseline.`,
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

// connectorSyncStatus is a connector's .status object
// (connectorView.connector.status): the sync run this command cares about.
type connectorSyncStatus struct {
	Status      string `json:"status"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
	LastError   string `json:"lastError"`
	ErrorKind   string `json:"errorKind"`
}

// readConnectorSyncStatus GETs the connector at path and returns its status
// object, the same read "connectors get" prints as .status.
func readConnectorSyncStatus(ctx context.Context, c *client.Client, path string) (connectorSyncStatus, error) {
	data, err := c.Get(ctx, path, nil)
	if err != nil {
		return connectorSyncStatus{}, fmt.Errorf("reading the connector: %w", err)
	}
	var env struct {
		ConnectorView struct {
			Connector struct {
				Status connectorSyncStatus `json:"status"`
			} `json:"connector"`
		} `json:"connectorView"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return connectorSyncStatus{}, &nonJSONResponseError{fmt.Errorf("the connector response has no connectorView.connector: %w", err)}
	}
	return env.ConnectorView.Connector.Status, nil
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
		st, err := readConnectorSyncStatus(ctx, c, path)
		if err != nil {
			return nil, fmt.Errorf("reading the connector's current sync status before the request: %w", err)
		}
		*dst = st.CompletedAt
		return nil, nil
	}}
}

// connectorSyncWaitOp polls the connector until a sync started after
// *baseline completes. A terminal LastError stops the wait with that error
// immediately -- ErrorKind is never a reliable signal on its own: the API
// sends "ERROR_KIND_UNSPECIFIED" (its enum zero value) on a clean DONE, not
// "". A terminal success's status object is returned as the satisfying value
// so sendMutation's withWaitResultOutput can print it.
func connectorSyncWaitOp(c *client.Client, path, connectorID, appID string, baseline *string, timeout time.Duration) waitOp[[]string] {
	return waitOp[[]string]{
		Poll: func(ctx context.Context) ([]string, error) {
			st, err := readConnectorSyncStatus(ctx, c, path)
			if err != nil {
				return nil, err
			}
			if !completedAtAdvanced(*baseline, st.CompletedAt) {
				return []string{}, nil
			}
			if st.LastError != "" {
				return nil, fmt.Errorf("connector %s sync ended in status %s: %s", connectorID, st.Status, st.LastError)
			}
			doc, err := json.Marshal(st)
			if err != nil {
				return nil, fmt.Errorf("encoding connector %s status: %w", connectorID, err)
			}
			return []string{string(doc)}, nil
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
