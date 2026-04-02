package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/ductone/c1i/internal/client"
	"github.com/spf13/cobra"
)

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Make a raw ConductorOne API request and pretty-print the JSON response",
	RunE: func(cmd *cobra.Command, args []string) error {
		tenant, err := GetTenant()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), tenant)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		path, _ := cmd.Flags().GetString("path")
		method, _ := cmd.Flags().GetString("method")
		body, _ := cmd.Flags().GetString("body")
		paginate, _ := cmd.Flags().GetBool("paginate")

		method = strings.ToUpper(method)
		if method == "" {
			if body != "" {
				method = "POST"
			} else {
				method = "GET"
			}
		}

		out := cmd.OutOrStdout()
		pageToken := ""

		for {
			var data []byte
			switch method {
			case "GET":
				reqPath := path
				if paginate && pageToken != "" {
					reqPath = setQueryParam(reqPath, "page_token", pageToken)
				}
				data, err = c.Get(cmd.Context(), reqPath, nil)
			case "POST":
				var bodyObj map[string]any
				if body != "" {
					if err := json.Unmarshal([]byte(body), &bodyObj); err != nil {
						return fmt.Errorf("invalid JSON body: %w", err)
					}
				} else {
					bodyObj = map[string]any{}
				}
				if paginate && pageToken != "" {
					bodyObj["pageToken"] = pageToken
				}
				data, err = c.Post(cmd.Context(), path, bodyObj)
			default:
				return fmt.Errorf("unsupported method: %s (use GET or POST)", method)
			}
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var pretty bytes.Buffer
			if err := json.Indent(&pretty, data, "", "  "); err != nil {
				_, _ = out.Write(data)
			} else {
				pretty.WriteByte('\n')
				_, _ = out.Write(pretty.Bytes())
			}

			if !paginate {
				break
			}

			var page struct {
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &page); err != nil || page.NextPageToken == "" {
				break
			}
			pageToken = page.NextPageToken
		}

		return nil
	},
}

func init() {
	apiCmd.Flags().String("path", "", "API path (e.g. /api/v1/search/app_users)")
	apiCmd.Flags().String("method", "", "HTTP method: GET or POST (default: GET, or POST if --body is set)")
	apiCmd.Flags().String("body", "", "JSON request body (implies POST)")
	apiCmd.Flags().Bool("paginate", false, "Automatically follow pagination to fetch all pages")
	_ = apiCmd.MarkFlagRequired("path")
	rootCmd.AddCommand(apiCmd)
}

// setQueryParam adds or replaces a query parameter on a URL path.
func setQueryParam(rawPath, key, value string) string {
	u, err := url.Parse(rawPath)
	if err != nil {
		return rawPath
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}
