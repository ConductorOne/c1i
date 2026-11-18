package cmd

import (
	"errors"
	"net/http"

	"github.com/spf13/cobra"
)

var connectorsSyncCmd = &cobra.Command{
	Use:   "sync <connector-id>",
	Short: "Start a sync of a connector now",
	Long: `Start a sync of a connector now. The API queues the sync; exit 0 means it
was accepted, not that it finished. Follow progress with
"c1i connectors sync-status".

Prints the API's response, which is {}. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendConnectorAction(cmd, args[0], "/force_sync")
	},
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
	f := connectorsScheduleCmd.Flags()
	f.String("cron", "", "Cron expression for the sync schedule (required unless --clear)")
	f.String("timezone", "", "IANA timezone for --cron, e.g. America/Chicago")
	f.Bool("clear", false, "Remove the sync schedule")
	f.String("justification", "", connectorJustificationUsage)
}
