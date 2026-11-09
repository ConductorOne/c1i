package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/spf13/cobra"
)

// The Mintlify URLs are vars so tests can point them at an httptest server.
var (
	mintlifySearchURL = "https://api.mintlify.com/discovery/v1/search/conductorone"
	mintlifyPageURL   = "https://api.mintlify.com/discovery/v1/page/conductorone"
)

const (
	docsBaseURL = "https://conductorone.com/docs/"
	// Mintlify assistant keys are public client-side keys.
	mintlifyKey = "mint_dsc_UzPuRdxe4P9NF2x387upse"
)

// postDocs POSTs payload to a Mintlify endpoint and returns the 200 body. Any
// failure is a docs-site failure, not C1's, so it exits 8 whatever the status.
func postDocs(cmd *cobra.Command, url string, payload any, what string) ([]byte, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+mintlifyKey)

	// Both endpoints only read, so a 5xx is safe to retry.
	hc := transport.New(nil, append(globalTransportOptions(), transport.WithReadOnly())...)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, externalServiceError(err, "%s request failed", what)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, externalServiceError(fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Body), "%s API failed", what)
	}
	return resp.Body, nil
}

var docsSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search C1 documentation by keyword (no auth required)",
	Long: `Search C1 documentation by keyword. Returns NDJSON, one match per
line, with the doc page path, title, snippet, and URL.

Results are semantic nearest-matches with NO relevance threshold: every query
returns up to 10 hits, so a nonsense query still returns plausible-looking
ones. A returned hit is not proof a concept exists, and an unexpected hit is
not proof the thing you searched for is absent -- read the snippet, or fetch
the page with 'docs page', to judge. To check whether an API endpoint exists,
use 'docs endpoints --filter', which has a real no-match.

Examples:
  c1i docs search "access reviews"
  c1i docs search campaigns
  c1i docs search "current user" | jq -r '.url'`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		query := args[0]
		for _, a := range args[1:] {
			query += " " + a
		}

		body, err := postDocs(cmd, mintlifySearchURL, map[string]any{"query": query, "pageSize": 10}, "search")
		if err != nil {
			return err
		}

		var results []struct {
			Path     string `json:"path"`
			Content  string `json:"content"`
			Metadata struct {
				Title string `json:"title"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(body, &results); err != nil {
			return externalServiceError(err, "failed to parse response")
		}

		enc := newEmitter(cmd)
		for _, r := range results {
			if err := enc.Encode(map[string]string{
				"title":   r.Metadata.Title,
				"path":    r.Path,
				"url":     docsBaseURL + r.Path,
				"content": r.Content,
			}); err != nil {
				return err
			}
		}

		return nil
	},
}

var docsPageCmd = &cobra.Command{
	Use:   "page <path>",
	Short: "Fetch a full documentation page by path (no auth required)",
	Long: `Fetch a full C1 documentation page by its path. Output is the
plain-text rendering of the page, suitable for grepping or piping.

The path is the URL segment after https://conductorone.com/docs/, with
no leading slash. Find paths via 'c1i docs search' (each result has a
'path' field) or by browsing the docs site.

Examples:
  c1i docs page product/admin/campaigns
  c1i docs page baton/okta
  c1i docs search "access reviews" | jq -r '.path' | head -1 | xargs c1i docs page`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		body, err := postDocs(cmd, mintlifyPageURL, map[string]string{"path": args[0]}, "page")
		if err != nil {
			return err
		}

		var page struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return externalServiceError(err, "failed to parse response")
		}

		_, err = fmt.Fprintln(cmd.OutOrStdout(), page.Content)
		return wrapOutputErr(err)
	},
}

func init() {
	docsCmd.AddCommand(docsSearchCmd)
	docsCmd.AddCommand(docsPageCmd)
}
