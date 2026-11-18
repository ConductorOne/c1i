package cmd

import (
	"github.com/spf13/cobra"
)

var settingsNotificationsCmd = &cobra.Command{Use: "notifications", Short: "Notification settings for the organization or the calling user"}

var settingsNotificationsOrgCmd = &cobra.Command{Use: "org", Short: "Organization-level notification settings"}
var settingsNotificationsUserCmd = &cobra.Command{Use: "user", Short: "The calling user's notification preferences"}

// notificationsUpdateLong is the update help both scopes share.
const notificationsUpdateLong = `--body-file is the whole request: {"channelSettings": {...}}, the object
"show" prints. There is no update mask, so a body without a non-empty
channelSettings object exits 2 and nothing is sent.

In the runs checked, a partial body on either scope changed only the
settings it named (one of 86 leaves on org). Sending settings back can
materialize an event the read showed as null (e.g. "system") as its default.

Prints the updated settings as pretty JSON, unwrapped like "show".`

func notificationsBodyCheck(body map[string]any) error {
	return requireNonEmptyBodyKey(body, "channelSettings")
}

var settingsNotificationsOrgShowCmd = newSettingsShowCmd("Show organization notification settings (pretty JSON)",
	`Show the organization-level notification settings: per-channel (email,
slack, teams) preferences and admin-locked defaults.`,
	"/api/v1/settings/notifications/org", "orgNotificationSettings")

var settingsNotificationsOrgUpdateCmd = newSettingsBodyUpdateCmd("Update organization notification settings from JSON (pretty JSON)",
	"Update the organization-level notification settings, which apply to every\nuser of the tenant.\n\n"+notificationsUpdateLong,
	"/api/v1/settings/notifications/org", named("orgNotificationSettings"), notificationsBodyCheck)

var settingsNotificationsUserShowCmd = newSettingsShowCmd("Show your notification preferences (pretty JSON)",
	`Show the calling user's notification preferences, merged with the
organization-level defaults.`,
	"/api/v1/settings/notifications/user", "userNotificationSettings")

var settingsNotificationsUserUpdateCmd = newSettingsBodyUpdateCmd("Update your notification preferences from JSON (pretty JSON)",
	"Update the calling user's personal notification preferences.\n\n"+notificationsUpdateLong,
	"/api/v1/settings/notifications/user", named("userNotificationSettings"), notificationsBodyCheck)

func init() {
	settingsNotificationsOrgCmd.AddCommand(settingsNotificationsOrgShowCmd, settingsNotificationsOrgUpdateCmd)
	settingsNotificationsUserCmd.AddCommand(settingsNotificationsUserShowCmd, settingsNotificationsUserUpdateCmd)
	settingsNotificationsCmd.AddCommand(settingsNotificationsOrgCmd, settingsNotificationsUserCmd)
	settingsCmd.AddCommand(settingsNotificationsCmd)
}
