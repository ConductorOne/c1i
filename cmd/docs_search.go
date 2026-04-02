package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"github.com/spf13/cobra"
)

const (
	mintlifySearchURL = "https://api.mintlify.com/discovery/v1/search/conductorone"
	mintlifyPageURL   = "https://api.mintlify.com/discovery/v1/page/conductorone"
	docsBaseURL       = "https://conductorone.com/docs/"
	// Mintlify assistant keys are public client-side keys.
	mintlifyKey = "mint_dsc_UzPuRdxe4P9NF2x387upse"
)

var docsSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search ConductorOne documentation",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		query := args[0]
		for _, a := range args[1:] {
			query += " " + a
		}

		reqBody, _ := json.Marshal(map[string]any{
			"query":    query,
			"pageSize": 10,
		})

		req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, mintlifySearchURL, bytes.NewReader(reqBody))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mintlifyKey)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("search request failed: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("search API returned %d: %s", resp.StatusCode, string(body))
		}

		var results []struct {
			Path     string `json:"path"`
			Content  string `json:"content"`
			Metadata struct {
				Title string `json:"title"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(body, &results); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		enc := json.NewEncoder(cmd.OutOrStdout())
		for _, r := range results {
			_ = enc.Encode(map[string]string{
				"title":   r.Metadata.Title,
				"path":    r.Path,
				"url":     docsBaseURL + r.Path,
				"content": r.Content,
			})
		}

		return nil
	},
}

var docsPageCmd = &cobra.Command{
	Use:   "page <path>",
	Short: "Fetch a documentation page by path",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		reqBody, _ := json.Marshal(map[string]string{
			"path": args[0],
		})

		req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, mintlifyPageURL, bytes.NewReader(reqBody))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mintlifyKey)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("page request failed: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("page API returned %d: %s", resp.StatusCode, string(body))
		}

		var page struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), page.Content)
		return nil
	},
}

func init() {
	docsCmd.AddCommand(docsSearchCmd)
	docsCmd.AddCommand(docsPageCmd)
}
