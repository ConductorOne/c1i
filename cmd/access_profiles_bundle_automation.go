package cmd

import (
	"fmt"
	"net/http"

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
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: bundleAutomationPath(args[0]) + "/create", Body: body, Unwrap: byID("requestCatalogId")})
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
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: bundleAutomationPath(args[0]), Body: body, Unwrap: byID("requestCatalogId")})
	},
}

var accessProfilesBundleAutomationDeleteCmd = &cobra.Command{
	Use:   "delete <access-profile-id>",
	Short: "Delete an access profile's bundle automation (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: bundleAutomationPath(args[0])})
	},
}

var accessProfilesBundleAutomationResumeCmd = &cobra.Command{
	Use:   "resume <access-profile-id>",
	Short: "Resume a paused access profile bundle automation (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: bundleAutomationPath(args[0]) + "/resume", Body: map[string]any{}, Unwrap: byID("requestCatalogId")})
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
			refs, err := readJSONFile[[]any](cmd, "refs-file", refsFile)
			if err != nil {
				return err
			}
			if err := validateAppEntitlementRefs(refs, "refs"); err != nil {
				return err
			}
			body["refs"] = refs
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: bundleAutomationPath(args[0]) + "/run", Body: body, Unwrap: byID("requestCatalogId")})
	},
}

func bundleAutomationPath(accessProfileID string) string {
	return client.Path("/api/v1/catalogs/%s/bundle_automation", accessProfileID)
}

func readBundleAutomationBody(cmd *cobra.Command) (map[string]any, error) {
	bodyFile, _ := cmd.Flags().GetString("body-file")
	if bodyFile == "" {
		return nil, &usageError{fmt.Errorf("--body-file is required")}
	}
	body, err := readJSONFile[map[string]any](cmd, "body-file", bodyFile)
	if err != nil {
		return nil, err
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
				if err := validateAppEntitlementRefs(refs, "--body-file entitlements.entitlementRefs"); err != nil {
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

	for _, c := range []*cobra.Command{
		accessProfilesBundleAutomationCreateCmd,
		accessProfilesBundleAutomationSetCmd,
		accessProfilesBundleAutomationDeleteCmd,
		accessProfilesBundleAutomationResumeCmd,
		accessProfilesBundleAutomationRunCmd,
	} {
		markMutating(c)
	}
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
