package cmd

import (
	"github.com/spf13/cobra"
)

const settingsXAAPath = "/api/v1/settings/cross-app-access"

var settingsXAACmd = &cobra.Command{Use: "cross-app-access", Short: "Cross-app access (XAA) settings"}

var settingsXAAShowCmd = newSettingsShowCmd("Show the tenant's cross-app access settings (pretty JSON)",
	`Show the tenant's cross-app access settings: enabled, grant and ID token
lifetimes, refresh-token subjects, and signing algorithms.`,
	settingsXAAPath, "settings")

var xaaSigningAlgFlag = enumFlag{flag: "default-signing-algorithm", prefix: "XAA_SIGNING_ALGORITHM_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "eddsa", api: "XAA_SIGNING_ALGORITHM_EDDSA"},
	{name: "rs256", api: "XAA_SIGNING_ALGORITHM_RS256"},
	{name: "es256", api: "XAA_SIGNING_ALGORITHM_ES256"},
}}

var settingsXAAUpdateSpec = settingsUpdateSpec(
	boolUpdate("enabled", "enabled"),
	durationUpdate("default-grant-lifetime", "defaultGrantLifetime"),
	boolUpdate("allow-refresh-token-subjects", "allowRefreshTokenSubjects"),
	enumUpdate("default-signing-algorithm", "defaultSigningAlgorithm", &xaaSigningAlgFlag),
	listUpdate("enabled-signing-algorithm", "enabledSigningAlgorithms", &xaaSigningAlgFlag),
	durationUpdate("xaa-id-token-lifetime", "xaaIdTokenLifetime"),
)

var settingsXAAUpdateCmd = newSettingsUpdateCmd("Update cross-app access settings (pretty JSON)",
	`Update the tenant's cross-app access settings. Only the flags you pass
change; --enabled-signing-algorithm replaces the whole list (repeat it).

To clear a lifetime back to null, use --body-file with an object that omits
it and --update-mask naming it (e.g. {} with --update-mask
defaultGrantLifetime).

Prints the updated settings as pretty JSON, unwrapped like "settings
cross-app-access show".`,
	settingsXAAPath, "settings", settingsXAAUpdateSpec)

var settingsXAAHistoryCmd = newSettingsHistoryCmd("List the cross-app access settings change history (NDJSON output)",
	`One NDJSON record per change to the cross-app access settings, newest first
as the server returns them. Each record carries the change metadata (id,
actor, changeKind, createdAt, traceId, ...) and the settings snapshot.`,
	settingsXAAPath+"/history")

func init() {
	c := settingsXAAUpdateCmd
	f := c.Flags()
	f.Bool("enabled", false, "Enable cross-app access (--enabled=false to disable; omit to leave unchanged)")
	addProtoDurationFlag(c, "default-grant-lifetime", "Default grant lifetime "+settingsDurationForm)
	f.Bool("allow-refresh-token-subjects", false, "Set allowRefreshTokenSubjects (omit to leave unchanged)")
	f.String("default-signing-algorithm", "", "Default signing algorithm: "+xaaSigningAlgFlag.usage())
	addRepeatableStringFlag(c, "enabled-signing-algorithm", "Enabled signing algorithm (repeatable; replaces the list): "+xaaSigningAlgFlag.usage())
	addProtoDurationFlag(c, "xaa-id-token-lifetime", "XAA ID token lifetime "+settingsDurationForm)
	settingsXAACmd.AddCommand(settingsXAAShowCmd, settingsXAAUpdateCmd, settingsXAAHistoryCmd)
	settingsCmd.AddCommand(settingsXAACmd)
}
