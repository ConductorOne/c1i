package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// apiRequester is the minimal surface `api` needs from an authenticated
// client: send one request, get the response bytes. Defining it locally
// (instead of depending on *client.Client's full method set) lets tests
// substitute a fake that talks straight to an httptest.Server, so the
// DELETE-body opt-in can be proven on the wire without driving newClient's
// real OAuth token mint.
type apiRequester interface {
	Request(ctx context.Context, method, path string, body []byte, headers map[string]string) ([]byte, error)
}

// newAPIClient builds the client `api` sends requests through. It's a var,
// not a plain function, purely so api_test.go can swap in a fake for the
// duration of a test (restoring the original after) — production always
// takes this branch, unchanged.
var newAPIClient = func(cmd *cobra.Command, baseURL string) (apiRequester, error) {
	return newClient(cmd, baseURL)
}

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

		path, _ := cmd.Flags().GetString("path")
		// Without a leading slash, path gets concatenated straight onto
		// baseURL (e.g. "https://host" + "api/v1/users" -> the host becomes
		// "hostapi", not "host") and fails as a DNS lookup error that never
		// names the real problem: the path itself.
		if !strings.HasPrefix(path, "/") {
			return &usageError{fmt.Errorf("--path %q must start with a leading slash (e.g. \"/api/v1/users\")", path)}
		}
		method, _ := cmd.Flags().GetString("method")
		body, _ := cmd.Flags().GetString("body")
		bodyFile, _ := cmd.Flags().GetString("body-file")
		paginate, _ := cmd.Flags().GetBool("paginate")
		listKey, _ := cmd.Flags().GetString("list-key")
		queryPairs, _ := cmd.Flags().GetStringArray("query")
		headerPairs, _ := cmd.Flags().GetStringArray("header")
		allowDeleteBody, _ := cmd.Flags().GetBool("allow-delete-body")
		limit := getIntFlag(cmd, "limit")

		if limit > 0 && !paginate {
			return &usageError{fmt.Errorf("--limit only applies with --paginate (without it, c1i api emits a single response and there's nothing to cap)")}
		}
		if listKey != "" && !paginate {
			return &usageError{fmt.Errorf("--list-key only applies with --paginate")}
		}

		// --body-file (or "-" for stdin) is an alternative to inline --body for
		// payloads too large or awkward to pass on the command line.
		if bodyFile != "" {
			if body != "" {
				return &usageError{fmt.Errorf("--body and --body-file are mutually exclusive")}
			}
			var raw []byte
			if bodyFile == "-" {
				raw, err = io.ReadAll(cmd.InOrStdin())
			} else {
				raw, err = os.ReadFile(bodyFile)
			}
			if err != nil {
				return fmt.Errorf("reading --body-file: %w", err)
			}
			body = string(raw)
		}

		headers, err := parseKeyValueFlag(headerPairs, "header")
		if err != nil {
			return err
		}

		method = strings.ToUpper(method)
		if method == "" {
			if body != "" {
				method = "POST"
			} else {
				method = "GET"
			}
		}
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return &usageError{fmt.Errorf("unsupported method: %s (use GET, POST, PUT, PATCH, or DELETE)", method)}
		}

		// GET carries no request body, full stop. DELETE normally doesn't
		// either, but a handful of C1 endpoints (e.g. remove-membership) are
		// body-taking DELETEs, so --allow-delete-body is an explicit opt-in
		// escape hatch. Without it, a body on DELETE is far more likely to be
		// a mistake than intent, so we fail fast rather than silently drop it
		// (which would send a different request than the caller expects).
		if body != "" && method == "GET" {
			return &usageError{fmt.Errorf("--method GET does not take a request body; drop --body/--body-file or use POST, PUT, or PATCH")}
		}
		if body != "" && method == "DELETE" && !allowDeleteBody {
			return &usageError{fmt.Errorf("--method DELETE does not take a request body; drop --body/--body-file, use POST, PUT, or PATCH, or pass --allow-delete-body if the endpoint requires a body on DELETE")}
		}

		// Apply --query params to the path once; pagination adds page_token per
		// iteration below for GET/DELETE.
		for _, qp := range queryPairs {
			k, v, ok := strings.Cut(qp, "=")
			if !ok || k == "" {
				return &usageError{fmt.Errorf("invalid --query %q: expected key=value", qp)}
			}
			path = setQueryParam(path, k, v)
		}

		// Preview mutating requests without sending when --dry-run is set. GET is
		// a read, so it still executes below. This mirrors the request the loop
		// would build for the first page (before any page token is added), and
		// runs before newClient so a preview needs no credentials.
		if dryRunActive() && method != "GET" {
			var previewBody any
			// A DELETE only reaches here with body != "" when --allow-delete-body
			// was set (the guard above rejects the combination otherwise), so
			// this still previews "no body" for a plain DELETE.
			if method != "DELETE" || body != "" {
				bodyObj := map[string]any{}
				if body != "" {
					if err := json.Unmarshal([]byte(body), &bodyObj); err != nil {
						return fmt.Errorf("invalid JSON body: %w", err)
					}
				}
				previewBody = bodyObj
			}
			return printDryRun(cmd, method, path, previewBody)
		}

		c, err := newAPIClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		enc := newEmitter(cmd)
		pageToken := ""
		prevToken := ""
		emitted := 0

		for !limitReached(emitted, limit) {
			reqPath := path
			var bodyBytes []byte
			switch method {
			case "GET":
				if paginate && pageToken != "" {
					reqPath = setQueryParam(reqPath, "page_token", pageToken)
				}
			case "DELETE":
				if paginate && pageToken != "" {
					reqPath = setQueryParam(reqPath, "page_token", pageToken)
				}
				// Reaching here with body != "" means --allow-delete-body was
				// set; the guard above already rejected the combination
				// otherwise. Send the caller's exact bytes (validated as JSON,
				// but not re-marshaled) rather than routing through the
				// map[string]any round-trip POST/PUT/PATCH use below, which
				// exists to splice in pageToken for pagination — not needed
				// here since DELETE pagination (like GET's) rides the query
				// string, not the body.
				if body != "" {
					var discard any
					if err := json.Unmarshal([]byte(body), &discard); err != nil {
						return fmt.Errorf("invalid JSON body: %w", err)
					}
					bodyBytes = []byte(body)
				}
			case "POST", "PUT", "PATCH":
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
				b, mErr := json.Marshal(bodyObj)
				if mErr != nil {
					return fmt.Errorf("encoding request body: %w", mErr)
				}
				bodyBytes = b
			default:
				return fmt.Errorf("unsupported method: %s (use GET, POST, PUT, PATCH, or DELETE)", method)
			}

			data, reqErr := c.Request(cmd.Context(), method, reqPath, bodyBytes, headers)
			if reqErr != nil {
				if method == "GET" && looksLikePOSTOnly(reqErr.Error(), reqPath) {
					return fmt.Errorf("API error: %w\n\nHint: this looks like a POST-only endpoint (e.g. /search/* paths). Try '--body \"{}\"' (which switches to POST) or '--method=POST'", reqErr)
				}
				return fmt.Errorf("API error: %w", reqErr)
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

// parseKeyValueFlag parses repeated "key=value" flag values into a map. flagName
// is only used to make error messages point at the offending flag.
func parseKeyValueFlag(pairs []string, flagName string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, &usageError{fmt.Errorf("invalid --%s %q: expected key=value", flagName, p)}
		}
		m[k] = v
	}
	return m, nil
}

func init() {
	apiCmd.Flags().String("path", "", "API path (e.g. /api/v1/search/app_users)")
	apiCmd.Flags().String("method", "", "HTTP method: GET, POST, PUT, PATCH, or DELETE (default: GET, or POST if a body is set)")
	apiCmd.Flags().String("body", "", "JSON request body (implies POST)")
	apiCmd.Flags().String("body-file", "", "Read the JSON request body from a file (\"-\" for stdin); mutually exclusive with --body")
	apiCmd.Flags().Bool("allow-delete-body", false, "Allow --body/--body-file with --method DELETE (some C1 endpoints, e.g. remove-membership, require a body on DELETE; without this flag such a request is refused)")
	apiCmd.Flags().StringArray("query", nil, "Query parameter as key=value (repeatable)")
	apiCmd.Flags().StringArray("header", nil, "Extra request header as key=value (repeatable)")
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
