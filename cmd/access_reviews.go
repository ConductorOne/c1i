package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var accessReviewsCmd = &cobra.Command{
	Use:   "access-reviews",
	Short: "Manage access-review campaigns",
	Long: `Manage access-review campaigns, the governance campaigns called access reviews by the API.

Campaign create and update use JSON files because campaign scope is a large nested
oneof shape. Create takes the complete request body. Update takes a partial
AccessReview object, inserts the campaign id, and wraps it with the required
updateMask.`,
}

var accessReviewReportsCmd = &cobra.Command{
	Use:   "reports",
	Short: "Manage an access-review campaign's generated reports",
}

var accessReviewsListCmd = newAccessReviewListCmd("list", "List access-review campaigns (NDJSON output)", "/api/v1/access_reviews", false)

var accessReviewsGetCmd = &cobra.Command{
	Use:   "get <campaign-id>",
	Short: "Get an access-review campaign (pretty JSON)",
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
		data, err := c.Get(cmd.Context(), client.Path("/api/v1/access_review/%s", args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "id")
	},
}

var accessReviewsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an access-review campaign (pretty JSON)",
	Long: `Create an access-review campaign from a complete JSON request object.

The request must contain at least one owner id in ownerIds. Provide the JSON
with --body-file; use "-" to read it from standard input.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		if err := validateCampaignOwners(body); err != nil {
			return err
		}
		return postAccessReview(cmd, "/api/v1/access_review", body)
	},
}

var accessReviewsUpdateCmd = &cobra.Command{
	Use:   "update <campaign-id>",
	Short: "Update an access-review campaign (pretty JSON)",
	Long: `Update an access-review campaign from a partial AccessReview JSON object.

Provide the JSON with --body-file and name its fields with --update-mask; both
are required. The command inserts the campaign id and wraps the request.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		campaign, err := readRequiredJSONObject(cmd, "body-file")
		if err != nil {
			return err
		}
		mask, _ := cmd.Flags().GetString("update-mask")
		if strings.TrimSpace(mask) == "" {
			return &usageError{fmt.Errorf("--update-mask is required")}
		}
		campaign["id"] = args[0]
		return postAccessReview(cmd, client.Path("/api/v1/access_review/%s", args[0]), map[string]any{
			"accessReview": campaign,
			"updateMask":   mask,
		})
	},
}

var accessReviewReportsListCmd = newAccessReviewListCmd("list <campaign-id>", "List generated access-review reports (NDJSON output)", "/api/v1/access_review/%s/report", true)

var accessReviewReportsGenerateCmd = &cobra.Command{
	Use:   "generate <campaign-id>",
	Short: "Generate an access-review report (pretty JSON)",
	Long: `Request asynchronous generation of an access-review report.

The response confirms that generation was requested, not that a report is ready.
Use "access-reviews reports list <campaign-id>" to find the report state and
its time-limited downloadUrl. Use --format for JSON, CSV, or XLSX, or
--body-file for the full request; they are mutually exclusive.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		bodyFile, _ := cmd.Flags().GetString("body-file")
		format, _ := cmd.Flags().GetString("format")
		if bodyFile != "" && format != "" {
			return &usageError{fmt.Errorf("--body-file and --format are mutually exclusive")}
		}
		body := map[string]any{}
		if bodyFile != "" {
			var err error
			body, err = readRequiredJSONObject(cmd, "body-file")
			if err != nil {
				return err
			}
		} else if format != "" {
			mapped, err := mapAccessReviewReportFormat(format)
			if err != nil {
				return &usageError{err}
			}
			body["format"] = mapped
		}
		return postAccessReview(cmd, client.Path("/api/v1/access_review/%s/report", args[0]), body)
	},
}

func newAccessReviewListCmd(use, short, pathTemplate string, requiresID bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args: func(cmd *cobra.Command, args []string) error {
			if requiresID {
				return cobra.ExactArgs(1)(cmd, args)
			}
			return cobra.NoArgs(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			baseURL, err := GetBaseURL()
			if err != nil {
				return err
			}
			c, err := newListClient(cmd, baseURL)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
			requestedPageSize := pageSizeFlag(cmd)
			pageToken, _ := cmd.Flags().GetString("page-token")
			manualPaging := cmd.Flags().Changed("page-token")
			limit := getIntFlag(cmd, "limit")
			enc := newEmitter(cmd)
			for !limitReached(enc.Written(), limit) {
				pageSize := requestedPageSize
				if !enc.Filtered() {
					pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
				}
				params := map[string]string{"page_size": strconv.Itoa(pageSize)}
				if pageToken != "" {
					params["page_token"] = pageToken
				}
				path := pathTemplate
				if len(args) == 1 {
					path = client.Path(pathTemplate, args[0])
				}
				data, err := c.Get(cmd.Context(), path, params)
				if err != nil {
					return fmt.Errorf("API error: %w", err)
				}
				var response struct {
					List          []json.RawMessage `json:"list"`
					NextPageToken string            `json:"nextPageToken"`
				}
				if err := json.Unmarshal(data, &response); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}
				for _, item := range response.List {
					_ = enc.Encode(json.RawMessage(unwrapEnvelope(item, "id")))
					if limitReached(enc.Written(), limit) {
						return nil
					}
				}
				if response.NextPageToken == "" || manualPaging {
					break
				}
				pageToken = response.NextPageToken
			}
			return nil
		},
	}
	addPaginationFlags(cmd)
	return cmd
}

func readRequiredJSONObject(cmd *cobra.Command, flag string) (map[string]any, error) {
	file, _ := cmd.Flags().GetString(flag)
	if file == "" {
		return nil, &usageError{fmt.Errorf("--%s is required", flag)}
	}
	body, err := readConfigFile(cmd, file)
	if err != nil {
		return nil, &usageError{fmt.Errorf("reading --%s: %w", flag, err)}
	}
	if body == nil {
		return nil, &usageError{fmt.Errorf("--%s must contain a JSON object", flag)}
	}
	return body, nil
}

func validateCampaignOwners(body map[string]any) error {
	owners, ok := body["ownerIds"].([]any)
	if !ok || len(owners) == 0 {
		return &usageError{fmt.Errorf("ownerIds must contain at least one user ID")}
	}
	for _, owner := range owners {
		if id, ok := owner.(string); !ok || id == "" {
			return &usageError{fmt.Errorf("ownerIds must contain only non-empty user IDs")}
		}
	}
	return nil
}

func postAccessReview(cmd *cobra.Command, path string, body map[string]any) error {
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

func mapAccessReviewReportFormat(format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return "ACCESS_REVIEW_REPORT_FORMAT_JSON", nil
	case "csv":
		return "ACCESS_REVIEW_REPORT_FORMAT_CSV", nil
	case "xlsx":
		return "ACCESS_REVIEW_REPORT_FORMAT_XLSX", nil
	default:
		return "", fmt.Errorf(`--format must be "json", "csv", or "xlsx"`)
	}
}

func init() {
	accessReviewsCreateCmd.Flags().String("body-file", "", "Full CreateAccessReview request JSON object (required; file or \"-\" for stdin)")
	accessReviewsUpdateCmd.Flags().String("body-file", "", "Partial AccessReview JSON object (required; file or \"-\" for stdin)")
	accessReviewsUpdateCmd.Flags().String("update-mask", "", "Comma-separated campaign fields to update (required)")
	accessReviewReportsGenerateCmd.Flags().String("format", "", "Report format: json, csv, or xlsx (mutually exclusive with --body-file)")
	accessReviewReportsGenerateCmd.Flags().String("body-file", "", "Full GenerateAccessReviewReport request JSON object (file or \"-\" for stdin; mutually exclusive with --format)")

	accessReviewReportsCmd.AddCommand(accessReviewReportsListCmd, accessReviewReportsGenerateCmd)
	accessReviewsCmd.AddCommand(accessReviewsListCmd, accessReviewsGetCmd, accessReviewsCreateCmd, accessReviewsUpdateCmd, accessReviewReportsCmd)
	rootCmd.AddCommand(accessReviewsCmd)
}
