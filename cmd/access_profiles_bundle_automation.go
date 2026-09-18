package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var accessProfilesBundleAutomationCmd = &cobra.Command{
	Use:   "bundle-automation",
	Short: "Manage an access profile's bundle automation",
	Long: `Manage the automation that grants an access profile's bundled entitlements.

Create and set take a JSON object through --body-file (or "-" for stdin). The
supported fields are createTasks, disableCircuitBreaker, enabled, and
entitlements, containing entitlementRefs (an array of objects with exactly
appId and id). Run optionally takes --refs-file with the entitlement reference
array.`,
}

var accessProfilesBundleAutomationGetCmd = &cobra.Command{
	Use:   "get <access-profile-id>",
	Short: "Get an access profile's bundle automation (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), bundleAutomationPath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "requestCatalogId")
	},
}

var accessProfilesBundleAutomationCreateCmd = &cobra.Command{
	Use:   "create <access-profile-id>",
	Short: "Create an access profile's bundle automation (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readBundleAutomationBody(cmd)
		if err != nil {
			return err
		}
		return postBundleAutomation(cmd, args[0], "/create", body)
	},
}

var accessProfilesBundleAutomationSetCmd = &cobra.Command{
	Use:   "set <access-profile-id>",
	Short: "Set an access profile's bundle automation (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := readBundleAutomationBody(cmd)
		if err != nil {
			return err
		}
		return postBundleAutomation(cmd, args[0], "", body)
	},
}

var accessProfilesBundleAutomationDeleteCmd = &cobra.Command{
	Use:   "delete <access-profile-id>",
	Short: "Delete an access profile's bundle automation (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := bundleAutomationPath(args[0])
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

var accessProfilesBundleAutomationResumeCmd = &cobra.Command{
	Use:   "resume <access-profile-id>",
	Short: "Resume a paused access profile bundle automation (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return postBundleAutomation(cmd, args[0], "/resume", map[string]any{})
	},
}

var accessProfilesBundleAutomationRunCmd = &cobra.Command{
	Use:   "run <access-profile-id>",
	Short: "Run an access profile's bundle automation (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		refsFile, _ := cmd.Flags().GetString("refs-file")
		if refsFile != "" {
			refs, err := readJSONArrayFile(cmd, refsFile)
			if err != nil {
				return fmt.Errorf("reading --refs-file: %w", err)
			}
			if refs == nil {
				return &usageError{fmt.Errorf("--refs-file must contain a JSON array")}
			}
			if err := validateAppEntitlementRefs(refs, "refs"); err != nil {
				return err
			}
			body["refs"] = refs
		}
		return postBundleAutomation(cmd, args[0], "/run", body)
	},
}

func bundleAutomationPath(accessProfileID string) string {
	return client.Path("/api/v1/catalogs/%s/bundle_automation", accessProfileID)
}

func postBundleAutomation(cmd *cobra.Command, accessProfileID, action string, body map[string]any) error {
	path := bundleAutomationPath(accessProfileID) + action
	if dryRunActive() {
		return printDryRun(cmd, "POST", path, body)
	}

	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Post(cmd.Context(), path, body)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	return writeRawObject(cmd, data)
}

func readBundleAutomationBody(cmd *cobra.Command) (map[string]any, error) {
	bodyFile, _ := cmd.Flags().GetString("body-file")
	if bodyFile == "" {
		return nil, &usageError{fmt.Errorf("--body-file is required")}
	}
	body, err := readConfigFile(cmd, bodyFile)
	if err != nil {
		return nil, fmt.Errorf("reading --body-file: %w", err)
	}
	if err := validateBundleAutomationBody(body); err != nil {
		return nil, err
	}
	return body, nil
}

func validateBundleAutomationBody(body map[string]any) error {
	if body == nil {
		return &usageError{fmt.Errorf("bundle automation body must be a JSON object")}
	}

	for key, value := range body {
		switch key {
		case "createTasks", "disableCircuitBreaker", "enabled":
			if _, ok := value.(bool); !ok {
				return &usageError{fmt.Errorf("%s must be a JSON boolean", key)}
			}
		case "entitlements":
			if value == nil {
				continue
			}
			entitlements, ok := value.(map[string]any)
			if !ok {
				return &usageError{fmt.Errorf("entitlements must be a JSON object")}
			}
			for nestedKey, nestedValue := range entitlements {
				if nestedKey != "entitlementRefs" {
					return &usageError{fmt.Errorf("entitlements contains unsupported field %q", nestedKey)}
				}
				if nestedValue == nil {
					continue
				}
				refs, ok := nestedValue.([]any)
				if !ok {
					return &usageError{fmt.Errorf("entitlements.entitlementRefs must be a JSON array")}
				}
				if err := validateAppEntitlementRefs(refs, "entitlements.entitlementRefs"); err != nil {
					return err
				}
			}
		default:
			return &usageError{fmt.Errorf("unsupported bundle automation field %q", key)}
		}
	}
	return nil
}

func init() {
	accessProfilesBundleAutomationCreateCmd.Flags().String("body-file", "", "Bundle automation JSON object (file, or \"-\" for stdin)")
	accessProfilesBundleAutomationSetCmd.Flags().String("body-file", "", "Bundle automation JSON object (file, or \"-\" for stdin)")
	accessProfilesBundleAutomationRunCmd.Flags().String("refs-file", "", "JSON array of AppEntitlementRef objects to run (file, or \"-\" for stdin)")

	accessProfilesBundleAutomationCmd.AddCommand(
		accessProfilesBundleAutomationGetCmd,
		accessProfilesBundleAutomationCreateCmd,
		accessProfilesBundleAutomationSetCmd,
		accessProfilesBundleAutomationDeleteCmd,
		accessProfilesBundleAutomationResumeCmd,
		accessProfilesBundleAutomationRunCmd,
	)
	accessProfilesCmd.AddCommand(accessProfilesBundleAutomationCmd)
}
