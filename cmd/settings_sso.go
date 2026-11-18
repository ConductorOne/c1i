package cmd

import (
	"github.com/spf13/cobra"
)

const settingsSSOPath = "/api/v1/settings/sso"

var settingsSSOCmd = &cobra.Command{Use: "sso", Short: "SSO provider settings (C1 as an identity provider)"}

var settingsSSOShowCmd = newSettingsShowCmd("Show the tenant's SSO provider settings (pretty JSON)",
	`Show the tenant's SSO provider settings: enabled, defaultSubjectType,
defaultAssertionLifetime, defaultIdTokenSignedResponseAlg.`,
	settingsSSOPath, "settings")

var ssoSubjectTypeFlag = enumFlag{flag: "default-subject-type", prefix: "SSO_SUBJECT_TYPE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "pairwise", api: "SSO_SUBJECT_TYPE_PAIRWISE"},
	{name: "public", api: "SSO_SUBJECT_TYPE_PUBLIC"},
	{name: "compatibility", api: "SSO_SUBJECT_TYPE_COMPATIBILITY"},
}}

var oidcSigningAlgFlag = enumFlag{flag: "default-id-token-alg", prefix: "OIDC_SIGNING_ALGORITHM_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "eddsa", api: "OIDC_SIGNING_ALGORITHM_EDDSA"},
	{name: "es256", api: "OIDC_SIGNING_ALGORITHM_ES256"},
	{name: "rs256", api: "OIDC_SIGNING_ALGORITHM_RS256"},
}}

var settingsSSOUpdateSpec = settingsUpdateSpec(
	boolUpdate("enabled", "enabled"),
	enumUpdate("default-subject-type", "defaultSubjectType", &ssoSubjectTypeFlag),
	durationUpdate("default-assertion-lifetime", "defaultAssertionLifetime"),
	enumUpdate("default-id-token-alg", "defaultIdTokenSignedResponseAlg", &oidcSigningAlgFlag),
)

var settingsSSOUpdateCmd = newSettingsUpdateCmd("Update SSO provider settings (pretty JSON)",
	`Update the tenant's SSO provider settings. Only the flags you pass change.
On a tenant without the SSO provider feature the server refuses every update
with a 403, "feature not enabled" (exit 3).

Prints the updated settings as pretty JSON, unwrapped like "settings sso
show".`,
	settingsSSOPath, "settings", settingsSSOUpdateSpec)

var settingsSSOHistoryCmd = newSettingsHistoryCmd("List the SSO settings change history (NDJSON output)",
	`One NDJSON record per change to the SSO provider settings, newest first as
the server returns them. Each record carries the change metadata (id, actor,
changeKind, createdAt, traceId, ...) and the settings snapshot.`,
	settingsSSOPath+"/history")

func init() {
	c := settingsSSOUpdateCmd
	f := c.Flags()
	f.Bool("enabled", false, "Enable the SSO provider (--enabled=false to disable; omit to leave unchanged)")
	f.String("default-subject-type", "", "Default subject type: "+ssoSubjectTypeFlag.usage())
	addProtoDurationFlag(c, "default-assertion-lifetime", "Default assertion lifetime "+settingsDurationForm)
	f.String("default-id-token-alg", "", "Default ID token signing algorithm: "+oidcSigningAlgFlag.usage())
	settingsSSOCmd.AddCommand(settingsSSOShowCmd, settingsSSOUpdateCmd, settingsSSOHistoryCmd)
	settingsCmd.AddCommand(settingsSSOCmd)
}
