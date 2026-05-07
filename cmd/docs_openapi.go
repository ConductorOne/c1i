package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	openapiURL      = "https://conductorone.com/docs/openapi.yaml"
	cacheMaxAge     = 24 * time.Hour
	cacheDirName    = ".c1i"
	cacheFileName   = "openapi.yaml"
)

var docsOpenapiCmd = &cobra.Command{
	Use:   "openapi",
	Short: "Dump the raw C1 OpenAPI spec (no auth required)",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := fetchOpenAPISpec(cmd)
		if err != nil {
			return err
		}
		_, _ = cmd.OutOrStdout().Write(data)
		return nil
	},
}

var docsEndpointsCmd = &cobra.Command{
	Use:   "endpoints [--filter <pattern>]",
	Short: "List all API endpoints, filterable by keyword (no auth required)",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := fetchOpenAPISpec(cmd)
		if err != nil {
			return err
		}

		filter, _ := cmd.Flags().GetString("filter")
		filter = strings.ToLower(filter)

		var spec struct {
			Paths map[string]map[string]struct {
				Summary     string `yaml:"summary"`
				OperationID string `yaml:"operationId"`
				Description string `yaml:"description"`
			} `yaml:"paths"`
		}
		if err := yaml.Unmarshal(data, &spec); err != nil {
			return fmt.Errorf("failed to parse OpenAPI spec: %w", err)
		}

		type endpoint struct {
			Method      string `json:"method"`
			Path        string `json:"path"`
			Summary     string `json:"summary"`
			OperationID string `json:"operation_id"`
		}

		var endpoints []endpoint
		for path, methods := range spec.Paths {
			for method, op := range methods {
				if method == "parameters" {
					continue
				}
				e := endpoint{
					Method:      strings.ToUpper(method),
					Path:        path,
					Summary:     op.Summary,
					OperationID: op.OperationID,
				}
				if filter != "" {
					haystack := strings.ToLower(e.Path + " " + e.Summary + " " + e.OperationID + " " + op.Description)
					if !strings.Contains(haystack, filter) {
						continue
					}
				}
				endpoints = append(endpoints, e)
			}
		}

		sort.Slice(endpoints, func(i, j int) bool {
			if endpoints[i].Path != endpoints[j].Path {
				return endpoints[i].Path < endpoints[j].Path
			}
			return endpoints[i].Method < endpoints[j].Method
		})

		enc := json.NewEncoder(cmd.OutOrStdout())
		for _, e := range endpoints {
			_ = enc.Encode(e)
		}

		return nil
	},
}

var docsEndpointCmd = &cobra.Command{
	Use:   "endpoint <path>",
	Short: "Show full request/response schema for an API endpoint (no auth required)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := fetchOpenAPISpec(cmd)
		if err != nil {
			return err
		}

		target := args[0]

		var spec map[string]any
		if err := yaml.Unmarshal(data, &spec); err != nil {
			return fmt.Errorf("failed to parse OpenAPI spec: %w", err)
		}

		paths, ok := spec["paths"].(map[string]any)
		if !ok {
			return fmt.Errorf("no paths found in spec")
		}

		pathObj, ok := paths[target].(map[string]any)
		if !ok {
			return fmt.Errorf("endpoint %s not found", target)
		}

		resolved := resolveRefs(pathObj, spec, 0, nil)

		out, err := json.MarshalIndent(resolved, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

func init() {
	docsEndpointsCmd.Flags().String("filter", "", "Filter endpoints by pattern (matches path, summary, operation ID)")
	docsCmd.AddCommand(docsOpenapiCmd)
	docsCmd.AddCommand(docsEndpointsCmd)
	docsCmd.AddCommand(docsEndpointCmd)
}

func fetchOpenAPISpec(cmd *cobra.Command) ([]byte, error) {
	cachePath := openAPICachePath()

	if info, err := os.Stat(cachePath); err == nil {
		if time.Since(info.ModTime()) < cacheMaxAge {
			return os.ReadFile(cachePath)
		}
	}

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, openapiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Fall back to cache on network error.
		if data, readErr := os.ReadFile(cachePath); readErr == nil {
			return data, nil
		}
		return nil, fmt.Errorf("fetching OpenAPI spec: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if data, readErr := os.ReadFile(cachePath); readErr == nil {
			return data, nil
		}
		return nil, fmt.Errorf("fetching OpenAPI spec: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	_ = os.MkdirAll(filepath.Dir(cachePath), 0o700)
	_ = os.WriteFile(cachePath, data, 0o644)

	return data, nil
}

func openAPICachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, cacheDirName, "cache", cacheFileName)
}

const maxRefDepth = 10

// resolveRefs recursively resolves $ref pointers in the spec with cycle detection.
func resolveRefs(node any, root map[string]any, depth int, seen map[string]bool) any {
	if depth > maxRefDepth {
		return node
	}
	if seen == nil {
		seen = make(map[string]bool)
	}
	switch v := node.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok {
			if seen[ref] {
				return map[string]string{"$ref": ref}
			}
			seen[ref] = true
			resolved := followRef(ref, root)
			if resolved != nil {
				return resolveRefs(resolved, root, depth+1, seen)
			}
		}
		out := make(map[string]any, len(v))
		for key, val := range v {
			out[key] = resolveRefs(val, root, depth, seen)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = resolveRefs(val, root, depth, seen)
		}
		return out
	default:
		return node
	}
}

func followRef(ref string, root map[string]any) any {
	ref = strings.TrimPrefix(ref, "#/")
	parts := strings.Split(ref, "/")
	var current any = root
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}
	return current
}
