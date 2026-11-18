package cmd

import (
	"github.com/spf13/cobra"
)

var settingsNotificationsCmd = &cobra.Command{Use: "notifications", Short: "Notification settings for the organization or the calling user"}

var settingsNotificationsOrgCmd = &cobra.Command{Use: "org", Short: "Organization-level notification settings"}
var settingsNotificationsUserCmd = &cobra.Command{Use: "user", Short: "The calling user's notification preferences"}

// notificationsUpdateLong is the update help both scopes share.
const notificationsUpdateLong = `--body-file is the whole request: {"channelSettings": {...}}, the object
"show" prints. There is no update mask. On the user scope a partial body
changed only the settings it named; editing the "show" output and sending it
back is the safe way. Sending settings back can materialize
an event the read showed as null (e.g. "system") as its default.

Prints the updated settings as pretty JSON, unwrapped like "show".`

var settingsNotificationsOrgShowCmd = newSettingsShowCmd("Show organization notification settings (pretty JSON)",
	`Show the organization-level notification settings: per-channel (email,
slack, teams) preferences and admin-locked defaults.`,
	"/api/v1/settings/notifications/org", "orgNotificationSettings")

var settingsNotificationsOrgUpdateCmd = newSettingsBodyUpdateCmd("Update organization notification settings from JSON (pretty JSON)",
	"Update the organization-level notification settings, which apply to every\nuser of the tenant.\n\n"+notificationsUpdateLong,
	"/api/v1/settings/notifications/org", named("orgNotificationSettings"))

var settingsNotificationsUserShowCmd = newSettingsShowCmd("Show your notification preferences (pretty JSON)",
	`Show the calling user's notification preferences, merged with the
organization-level defaults.`,
	"/api/v1/settings/notifications/user", "userNotificationSettings")

var settingsNotificationsUserUpdateCmd = newSettingsBodyUpdateCmd("Update your notification preferences from JSON (pretty JSON)",
	"Update the calling user's personal notification preferences.\n\n"+notificationsUpdateLong,
	"/api/v1/settings/notifications/user", named("userNotificationSettings"))

func init() {
	settingsNotificationsOrgCmd.AddCommand(settingsNotificationsOrgShowCmd, settingsNotificationsOrgUpdateCmd)
	settingsNotificationsUserCmd.AddCommand(settingsNotificationsUserShowCmd, settingsNotificationsUserUpdateCmd)
	settingsNotificationsCmd.AddCommand(settingsNotificationsOrgCmd, settingsNotificationsUserCmd)
	settingsCmd.AddCommand(settingsNotificationsCmd)
}
