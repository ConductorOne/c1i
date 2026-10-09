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
	"go.yaml.in/yaml/v3"
)

const (
	openapiURL    = "https://www.c1.ai/api/openapi.yaml"
	cacheMaxAge   = 24 * time.Hour
	cacheDirName  = ".c1i"
	cacheFileName = "api-openapi.yaml"
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
	Long: `Search the public C1 OpenAPI spec for endpoints. Unlike semantic "docs
search", no output from --filter is a real no-match in that spec. It does not
prove no C1 operation exists. Use first-class commands to inspect tenant
resources. Pass a returned path to "docs endpoint" for its full schema.`,
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

		enc := newEmitter(cmd)
		for _, e := range endpoints {
			_ = enc.Encode(e)
		}

		if len(endpoints) == 0 && filter != "" {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"No endpoints matched %q in the public OpenAPI spec. Try 'c1i docs search %q' for related documentation, or a first-class command to inspect tenant resources.\n",
				filter, filter)
		}

		return nil
	},
}

var docsEndpointCmd = &cobra.Command{
	Use:   "endpoint <path>",
	Short: "Show full request/response schema for an API endpoint (no auth required)",
	Long: `Show the full request and response schema for a specific C1 API
endpoint. The path is one of the values returned by 'c1i docs endpoints'
(no auth required for either command).

Examples:
  c1i docs endpoint /api/v1/users/{id}
  c1i docs endpoint /api/v1/search/tasks
  c1i docs endpoint /api/v1/auth/introspect`,
	Args: cobra.ExactArgs(1),
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
			return &usageError{fmt.Errorf("endpoint %s not found", target)}
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
	docsEndpointsCmd.Flags().String("filter", "", "Filter endpoints by pattern (matches path, summary, operation ID, description)")
	docsCmd.AddCommand(docsOpenapiCmd)
	docsCmd.AddCommand(docsEndpointsCmd)
	docsCmd.AddCommand(docsEndpointCmd)
}

func fetchOpenAPISpec(cmd *cobra.Command) ([]byte, error) {
	cachePath := openAPICachePath()

	if cachePath != "" {
		if info, err := os.Stat(cachePath); err == nil && time.Since(info.ModTime()) < cacheMaxAge {
			return os.ReadFile(cachePath) // #nosec G304 -- cachePath is a fixed internal path (openAPICachePath), not caller input
		}
	}

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, openapiURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return staleOpenAPISpec(cachePath, fmt.Errorf("fetching OpenAPI spec: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return staleOpenAPISpec(cachePath, fmt.Errorf("fetching OpenAPI spec: HTTP %d", resp.StatusCode))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return staleOpenAPISpec(cachePath, fmt.Errorf("reading OpenAPI spec: %w", err))
	}
	// A captive portal answers 200 with HTML, and caching that would serve it
	// until cacheMaxAge.
	var doc map[string]any
	err = yaml.Unmarshal(data, &doc)
	if _, ok := doc["paths"]; err != nil || !ok {
		return staleOpenAPISpec(cachePath, fmt.Errorf("fetching OpenAPI spec: %s did not return an OpenAPI document", openapiURL))
	}

	if cachePath != "" {
		writeOpenAPICache(cachePath, data)
	}
	return data, nil
}

// staleOpenAPISpec returns the cached spec, however old, or err if there is none.
func staleOpenAPISpec(cachePath string, err error) ([]byte, error) {
	if cachePath != "" {
		if data, readErr := os.ReadFile(cachePath); readErr == nil { // #nosec G304 -- cachePath is a fixed internal path (openAPICachePath), not caller input
			return data, nil
		}
	}
	return nil, err
}

// writeOpenAPICache is best-effort. The rename keeps a reader from seeing a
// partial file. The temp name isn't in cacheFiles, so the prune after the
// rename sweeps one a crash left behind; a concurrent refresh's prune can also
// remove ours, which only skips this write.
func writeOpenAPICache(cachePath string, data []byte) {
	dir := filepath.Dir(cachePath)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "."+cacheFileName+".tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	_, writeErr := tmp.Write(data)
	chmodErr := tmp.Chmod(0o644) // #nosec G302 -- cached OpenAPI spec is public C1 API documentation, not sensitive
	if closeErr := tmp.Close(); writeErr != nil || chmodErr != nil || closeErr != nil {
		return
	}
	if os.Rename(tmpName, cachePath) == nil {
		pruneCacheDir(dir)
	}
}

// openAPICachePath returns "" when there is no usable home dir, which disables
// the cache: a relative path would put it in the working directory.
func openAPICachePath() string {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return ""
	}
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
		// seen is shared across siblings, so only the first to reach a ref
		// expands it; sorted keys make that the same sibling on every run.
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(v))
		for _, key := range keys {
			out[key] = resolveRefs(v[key], root, depth, seen)
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
