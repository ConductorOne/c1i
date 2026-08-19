package cmd

import "github.com/spf13/cobra"

var entitlementsCmd = &cobra.Command{
	Use:   "entitlements",
	Short: "Manage application entitlements",
	Long: `Manage application entitlements.

Note: some system-builtin entitlements — e.g. the base "Access" entitlement
("Local and federated users.") — use the identical entitlement ID across
every app that has one, by design (the same pattern MCP uses for its
"All approved tools" / "Read tools" system toolsets). This is not data
corruption. An entitlement ID is only unique per app: always key on
(app-id, id) together, never on id alone.`,
}

func init() {
	rootCmd.AddCommand(entitlementsCmd)
}
