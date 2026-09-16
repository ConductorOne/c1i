package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsProxyBindingsCmd = &cobra.Command{
	Use:   "proxy-bindings",
	Short: "Manage directional entitlement proxy bindings",
	Long: `Manage directional entitlement-to-entitlement proxy bindings.

A proxy binding is a visibility and tracking link from a source entitlement to a
destination entitlement. It does not grant access or configure delegated
provisioning; use "c1i docs guide delegate-entitlement-provisioning" for that
separate, ordered workflow. The public REST API has no list endpoint. The C1
Console discovers bindings through its internal gRPC-web search service, which
is not part of the public API contract.`,
}

var entitlementsProxyBindingsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get an entitlement proxy binding (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := proxyBindingPath(cmd)
		if err != nil {
			return err
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "srcAppId")
	},
}

var entitlementsProxyBindingsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an entitlement proxy binding (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := proxyBindingPath(cmd)
		if err != nil {
			return err
		}
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, map[string]any{})
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), path, map[string]any{})
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeRawObject(cmd, data)
	},
}

var entitlementsProxyBindingsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an entitlement proxy binding (pretty JSON)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := proxyBindingPath(cmd)
		if err != nil {
			return err
		}
		if dryRunActive() {
			return printDryRun(cmd, "DELETE", path, nil)
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeRawObject(cmd, data)
	},
}

func proxyBindingPath(cmd *cobra.Command) (string, error) {
	for _, name := range []string{
		"source-app-id",
		"source-entitlement-id",
		"destination-app-id",
		"destination-entitlement-id",
	} {
		if err := requireNonEmpty(cmd, name); err != nil {
			return "", err
		}
	}

	sourceAppID, _ := cmd.Flags().GetString("source-app-id")
	sourceEntitlementID, _ := cmd.Flags().GetString("source-entitlement-id")
	destinationAppID, _ := cmd.Flags().GetString("destination-app-id")
	destinationEntitlementID, _ := cmd.Flags().GetString("destination-entitlement-id")
	return client.Path(
		"/api/v1/apps/%s/%s/bindings/%s/%s",
		sourceAppID,
		sourceEntitlementID,
		destinationAppID,
		destinationEntitlementID,
	), nil
}

func init() {
	for _, cmd := range []*cobra.Command{
		entitlementsProxyBindingsGetCmd,
		entitlementsProxyBindingsCreateCmd,
		entitlementsProxyBindingsDeleteCmd,
	} {
		cmd.Flags().String("source-app-id", "", "Application ID of the source entitlement (required)")
		cmd.Flags().String("source-entitlement-id", "", "Source entitlement ID (required)")
		cmd.Flags().String("destination-app-id", "", "Application ID of the destination entitlement (required)")
		cmd.Flags().String("destination-entitlement-id", "", "Destination entitlement ID (required)")
		markRequired(cmd, "source-app-id")
		markRequired(cmd, "source-entitlement-id")
		markRequired(cmd, "destination-app-id")
		markRequired(cmd, "destination-entitlement-id")
	}
	entitlementsProxyBindingsCmd.AddCommand(
		entitlementsProxyBindingsGetCmd,
		entitlementsProxyBindingsCreateCmd,
		entitlementsProxyBindingsDeleteCmd,
	)
	entitlementsCmd.AddCommand(entitlementsProxyBindingsCmd)
}
