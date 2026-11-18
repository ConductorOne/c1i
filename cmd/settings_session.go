package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const settingsSessionPath = "/api/v1/settings/session"

var settingsSessionCmd = &cobra.Command{Use: "session", Short: "Session security settings (session length, idle timeout, source-IP allowlists)"}

var settingsSessionShowCmd = newSettingsShowCmd("Show the tenant's session security settings (pretty JSON)",
	`Show the tenant's session security settings: maximum session length, idle
timeout, external clients, the client ID metadata document policy, and the
per-surface source-IP allowlists (ssoAdminSource, ssoUserSource,
pccAdminSource, pccUserSource, connectorSource, externalClientSource).`,
	settingsSessionPath, "sessionSettings")

var clientIDMetadataPolicyFlag = enumFlag{flag: "client-id-metadata-document-policy", prefix: "CLIENT_ID_METADATA_DOCUMENT_POLICY_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "allow-all", api: "CLIENT_ID_METADATA_DOCUMENT_POLICY_ALLOW_ALL"},
	{name: "requestable", api: "CLIENT_ID_METADATA_DOCUMENT_POLICY_REQUESTABLE"},
	{name: "allowlist-only", api: "CLIENT_ID_METADATA_DOCUMENT_POLICY_ALLOWLIST_ONLY"},
}}

var settingsSessionUpdateSpec = settingsUpdateSpec(
	durationUpdate("max-session-length", "maxSessionLength"),
	boolUpdate("enable-idle-timeout", "enableIdleTimeout"),
	durationUpdate("idle-timeout", "idleTimeout"),
	boolUpdate("external-clients-enabled", "externalClientsEnabled"),
	enumUpdate("client-id-metadata-document-policy", "clientIdMetadataDocumentPolicy", &clientIDMetadataPolicyFlag),
)

var settingsSessionUpdateCmd = newSettingsUpdateCmd("Update session security settings (pretty JSON)",
	`Update the tenant's session security settings. Only the flags you pass
change. The server accepts only some session lengths and lists them when it
refuses one (exit 2), e.g. "value must be in list [seconds:1800 ...
seconds:72000]".

The source-IP allowlists and clientIdApprovalRequestPolicyId have no flags:
send them with --body-file and --update-mask (e.g. ssoAdminSource). An
allowlist that leaves out your own address can lock you out; check it first
with "settings session test-source-ip".

Prints the updated settings as pretty JSON, unwrapped like "settings session
show".`,
	settingsSessionPath, "sessionSettings", settingsSessionUpdateSpec)

var settingsSessionTestSourceIPCmd = &cobra.Command{
	Use:   "test-source-ip",
	Short: "Check whether an IP would pass a CIDR allowlist (pretty JSON)",
	Long: `Ask the server whether --ip would be allowed by the --allow-cidr rules. A
read: it changes nothing and does not look at the saved allowlists.

Prints the response as returned: "allowed" (true or false), "checkedIp", and
"details" (the reason when refused, e.g. "unauthorized source ip"). It exits 0
for either answer; the answer is in the output. With no --allow-cidr the
server answers allowed: false. A malformed IP is a 400 (exit 2).`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ip, _ := cmd.Flags().GetString("ip")
		cidrs, err := repeatableStringFlag(cmd, "allow-cidr")
		if err != nil {
			return err
		}
		body := map[string]any{"sourceIp": ip}
		if len(cidrs) > 0 {
			body["allowCidr"] = cidrs
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), settingsSessionPath+"/test-source-ip", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeObject(cmd, data)
	},
}

func init() {
	c := settingsSessionUpdateCmd
	f := c.Flags()
	addProtoDurationFlag(c, "max-session-length", "Maximum session length "+settingsDurationForm)
	f.Bool("enable-idle-timeout", false, "End idle sessions after --idle-timeout (--enable-idle-timeout=false to stop; omit to leave unchanged)")
	addProtoDurationFlag(c, "idle-timeout", "Idle timeout "+settingsDurationForm)
	f.Bool("external-clients-enabled", false, "Allow external clients (--external-clients-enabled=false to refuse them; omit to leave unchanged)")
	f.String("client-id-metadata-document-policy", "", "Client ID metadata document policy: "+clientIDMetadataPolicyFlag.usage())

	tf := settingsSessionTestSourceIPCmd.Flags()
	tf.String("ip", "", "IP address to check")
	addRepeatableStringFlag(settingsSessionTestSourceIPCmd, "allow-cidr", "CIDR rule to check against (repeatable)")
	markRequired(settingsSessionTestSourceIPCmd, "ip")

	settingsSessionCmd.AddCommand(settingsSessionShowCmd, settingsSessionUpdateCmd, settingsSessionTestSourceIPCmd)
	settingsCmd.AddCommand(settingsSessionCmd)
}
