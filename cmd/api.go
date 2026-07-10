package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Make a raw C1 API request and pretty-print the JSON response",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "path"); err != nil {
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

		path, _ := cmd.Flags().GetString("path")
		method, _ := cmd.Flags().GetString("method")
		body, _ := cmd.Flags().GetString("body")
		paginate, _ := cmd.Flags().GetBool("paginate")
		listKey, _ := cmd.Flags().GetString("list-key")
		limit := getIntFlag(cmd, "limit")

		if limit > 0 && !paginate {
			return fmt.Errorf("--limit only applies with --paginate (without it, c1i api emits a single response and there's nothing to cap)")
		}
		if listKey != "" && !paginate {
			return fmt.Errorf("--list-key only applies with --paginate")
		}

		method = strings.ToUpper(method)
		if method == "" {
			if body != "" {
				method = "POST"
			} else {
				method = "GET"
			}
		}

		out := cmd.OutOrStdout()
		enc := newEmitter(out)
		pageToken := ""
		prevToken := ""
		emitted := 0

		for !limitReached(emitted, limit) {
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
			case "PUT":
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
				data, err = c.Put(cmd.Context(), path, bodyObj)
			case "DELETE":
				data, err = c.Delete(cmd.Context(), path)
			default:
				return fmt.Errorf("unsupported method: %s (use GET, POST, PUT, or DELETE)", method)
			}
			if err != nil {
				if method == "GET" && looksLikePOSTOnly(err.Error(), path) {
					return fmt.Errorf("API error: %w\n\nHint: this looks like a POST-only endpoint (e.g. /search/* paths). Try '--body \"{}\"' (which switches to POST) or '--method=POST'", err)
				}
				return fmt.Errorf("API error: %w", err)
			}

			if !paginate {
				return writeObject(cmd, data)
			}

			items, nextToken, err := extractListAndToken(data, listKey)
			if err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range items {
				// Encode the raw item, not a decoded any: decoding to any turns
				// JSON numbers into float64 and corrupts large integer IDs. The
				// emitter handles json.RawMessage directly (and projects it with
				// UseNumber when --fields is set), preserving precision.
				if err := enc.Encode(item); err != nil {
					return fmt.Errorf("failed to write output: %w", err)
				}
				emitted++
				if limitReached(emitted, limit) {
					return nil
				}
			}

			if nextToken == "" {
				break
			}
			// Guard against servers that return the same token forever.
			// Without this, c1i would loop indefinitely emitting no progress.
			if nextToken == prevToken {
				return fmt.Errorf("API returned the same nextPageToken twice in a row for %s; the endpoint may not support pagination via the cursor c1i is sending. Drop --paginate and call the endpoint directly, or report this with the path", path)
			}
			prevToken = nextToken
			pageToken = nextToken
		}

		return nil
	},
}

func init() {
	apiCmd.Flags().String("path", "", "API path (e.g. /api/v1/search/app_users)")
	apiCmd.Flags().String("method", "", "HTTP method: GET, POST, PUT, or DELETE (default: GET, or POST if --body is set)")
	apiCmd.Flags().String("body", "", "JSON request body (implies POST)")
	apiCmd.Flags().Bool("paginate", false, "Automatically follow pagination to fetch all pages")
	apiCmd.Flags().String("list-key", "", "Force the response field name to drain as the list (default: auto-detect the first array-valued field, e.g. 'list', 'automationExecutions', 'automations')")
	markRequired(apiCmd, "path")
	addLimitFlag(apiCmd)
	rootCmd.AddCommand(apiCmd)
}

// looksLikePOSTOnly returns true when a GET error and path together suggest
// the endpoint requires POST. C1 returns 405 in some cases and 404 in others
// (the gateway treats unknown method+path combos as 404), so we hint on both
// when the path matches the search/* convention.
func looksLikePOSTOnly(errMsg, path string) bool {
	if !strings.Contains(errMsg, "returned 405") && !strings.Contains(errMsg, "returned 404") {
		return false
	}
	return strings.Contains(path, "/search/") || strings.Contains(errMsg, "returned 405")
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

// extractListAndToken finds the array-valued field in a paginated response and
// the nextPageToken. Most C1 list endpoints wrap items under "list", but some
// use typed keys ("automationExecutions", "automationVersions", etc.) — for
// those, returning an empty list would make --paginate loop forever, so we
// detect the array generically.
//
// If listKey is non-empty, that field is used verbatim. Otherwise the first
// array-valued field at the top level (other than "nextPageToken") wins.
func extractListAndToken(data []byte, listKey string) ([]json.RawMessage, string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, "", err
	}

	var nextPageToken string
	if v, ok := raw["nextPageToken"]; ok {
		// Ignore unmarshal error: a missing/null token is legitimately empty.
		_ = json.Unmarshal(v, &nextPageToken)
	}

	if listKey != "" {
		v, ok := raw[listKey]
		if !ok {
			return nil, nextPageToken, nil
		}
		var items []json.RawMessage
		if err := json.Unmarshal(v, &items); err != nil {
			return nil, nextPageToken, fmt.Errorf("field %q is not an array: %w", listKey, err)
		}
		return items, nextPageToken, nil
	}

	// Prefer "list" when present — it's the canonical name and skipping the
	// map walk keeps behavior stable for endpoints that have always worked.
	if v, ok := raw["list"]; ok {
		var items []json.RawMessage
		if err := json.Unmarshal(v, &items); err == nil {
			return items, nextPageToken, nil
		}
	}

	// Iterate keys in sorted order so the chosen array is deterministic when a
	// response carries more than one array-valued field (Go map iteration is
	// randomized). --list-key is the escape hatch when the first sorted match
	// isn't the one you want.
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "nextPageToken" || k == "list" {
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw[k], &items); err == nil {
			return items, nextPageToken, nil
		}
	}

	return nil, nextPageToken, nil
}
