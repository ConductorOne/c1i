package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/redact"
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
	Long: `Use this raw escape hatch only when no first-class command covers the endpoint.
First-class list commands auto-paginate; api returns one response unless
--paginate is set. A nextPageToken without --paginate is a partial-result
warning on stderr. With --paginate, a pageToken in the body (or page_token in
--query) is the first page's cursor, and later pages replace it; pageSize is
sent as given. You supply the request method and body; use --dry-run before an
intended non-GET REST write.`,
	RunE: func(cmd *cobra.Command, args []string) error {

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
			return &usageError{fmt.Errorf("--path %q must start with a leading slash (e.g. \"/api/v1/users\")", redact.URL(path))}
		}
		method, _ := cmd.Flags().GetString("method")
		body, _ := cmd.Flags().GetString("body")
		bodyFile, _ := cmd.Flags().GetString("body-file")
		paginate, _ := cmd.Flags().GetBool("paginate")
		listKey, _ := cmd.Flags().GetString("list-key")
		queryPairs, err := repeatableStringFlag(cmd, "query")
		if err != nil {
			return err
		}
		headerPairs, err := repeatableStringFlag(cmd, "header")
		if err != nil {
			return err
		}
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
				raw, err = os.ReadFile(bodyFile) // #nosec G304 -- user-supplied --body-file path is intentional
			}
			if err != nil {
				return &usageError{fmt.Errorf("reading --body-file: %w", err)}
			}
			body = string(raw)
		}

		headers, err := parseKeyValueFlag(headerPairs, "header")
		if err != nil {
			return err
		}

		switch {
		case method != "":
			if method, err = httpMethodFlag.parse(method); err != nil {
				return err
			}
		case body != "":
			method = "POST"
		default:
			method = "GET"
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
			k, v, err := cutKeyValue("query", qp)
			if err != nil {
				return err
			}
			path = setQueryParam(path, k, v)
		}

		// Preview mutating requests without sending when --dry-run is set. GET is
		// a read, so it still executes below. This mirrors the request the loop
		// would build for the first page (before any page token is added), and
		// runs before newClient so a preview needs no credentials.
		if dryRunActive() && method != "GET" {
			// A preview is never projected.
			if err := refuseMutationFields(cmd); err != nil {
				return err
			}
			// The exact bytes the first request would send: a DELETE only
			// has a body with --allow-delete-body (rejected above otherwise).
			var raw []byte
			if method != "DELETE" || body != "" {
				bodyObj := map[string]any{}
				if body != "" {
					if err := json.Unmarshal([]byte(body), &bodyObj); err != nil {
						return &usageError{fmt.Errorf("invalid JSON body: %w", err)}
					}
					if bodyObj == nil {
						return errNullBody
					}
				}
				raw = []byte(body)
				if method != "DELETE" {
					if raw, err = json.Marshal(bodyObj); err != nil {
						return fmt.Errorf("encoding request body: %w", err)
					}
				}
			}
			req, err := prepareMutation(baseURL, mutation{Method: method, Path: path, RawBody: raw})
			if err != nil {
				return err
			}
			return writeMutationPreview(cmd, baseURL, []previewRequest{req})
		}

		c, err := newAPIClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		var bodyObj map[string]any
		var rawBody []byte
		switch method {
		case "GET":
		case "DELETE":
			// Reaching here with body != "" means --allow-delete-body was set;
			// the guard above already rejected the combination otherwise. Send
			// the caller's exact bytes (validated as JSON, not re-marshaled):
			// DELETE pagination, like GET's, rides the query string.
			if body != "" {
				var discard any
				if err := json.Unmarshal([]byte(body), &discard); err != nil {
					return &usageError{fmt.Errorf("invalid JSON body: %w", err)}
				}
				rawBody = []byte(body)
			}
		case "POST", "PUT", "PATCH":
			bodyObj = map[string]any{}
			if body != "" {
				if err := json.Unmarshal([]byte(body), &bodyObj); err != nil {
					return &usageError{fmt.Errorf("invalid JSON body: %w", err)}
				}
				if bodyObj == nil {
					return errNullBody
				}
				if paginate {
					if _, _, err := bodyField(bodyObj, "pageToken"); err != nil {
						return err
					}
				}
			}
		default:
			return fmt.Errorf("unsupported method: %s (use GET, POST, PUT, PATCH, or DELETE)", method)
		}

		// fetch sends the request for the page at token (through c, which may be a test fake, not the engine's client); paging rides the query
		// string for GET/DELETE and the body for POST/PUT/PATCH.
		fetch := func(_ *client.Client, _ int, token string) ([]byte, error) {
			reqPath, bodyBytes := path, rawBody
			if paginate && token != "" && bodyObj == nil {
				reqPath = setQueryParam(reqPath, "page_token", token)
			}
			if bodyObj != nil {
				obj := bodyObj
				if paginate && token != "" {
					obj = cloneJSONObject(bodyObj)
					if err := setBodyField(obj, "pageToken", token); err != nil {
						return nil, err
					}
				}
				b, err := json.Marshal(obj)
				if err != nil {
					return nil, fmt.Errorf("encoding request body: %w", err)
				}
				bodyBytes = b
			}
			data, err := c.Request(cmd.Context(), method, reqPath, bodyBytes, headers)
			if err != nil {
				if method == "GET" && looksLikePOSTOnly(err.Error(), reqPath) {
					return nil, fmt.Errorf("API error: %w\n\nHint: this looks like a POST-only endpoint (e.g. /search/* paths). Try '--body \"{}\"' (which switches to POST) or '--method=POST'", err)
				}
				return nil, fmt.Errorf("API error: %w", err)
			}
			return data, nil
		}

		if !paginate {
			data, err := fetch(nil, 0, "")
			if err != nil {
				return err
			}
			warnPartialResult(cmd, data, path)
			return writeObject(cmd, data)
		}
		return runPages(cmd, nil, pageSpec[json.RawMessage]{
			Route: method + " " + redact.URL(path),
			Fetch: fetch,
			Parse: apiPageParser(listKey),
			// Every item has an identity, so a page is either kept whole or
			// stale; a stale page is held, not dropped (see HoldStale).
			ID:        itemIdentity,
			HoldStale: true,
			// Encode the raw item, not a decoded any: decoding to any turns
			// JSON numbers into float64 and corrupts large integer IDs. The
			// emitter handles json.RawMessage directly (and projects it with
			// UseNumber when --fields is set), preserving precision.
			Enc:   newEmitter(cmd),
			Limit: limit,
		})
	},
}

func init() {
	apiCmd.Flags().String("path", "", "API path (e.g. /api/v1/search/app_users)")
	apiCmd.Flags().String("method", "", "HTTP method: "+httpMethodFlag.usage()+" (default: get, or post if a body is set)")
	apiCmd.Flags().String("body", "", "JSON request body (implies POST)")
	apiCmd.Flags().String("body-file", "", "Read the JSON request body from a file (\"-\" for stdin); mutually exclusive with --body")
	apiCmd.Flags().Bool("allow-delete-body", false, "Allow --body/--body-file with --method DELETE (some C1 endpoints, e.g. remove-membership, require a body on DELETE; without this flag such a request is refused)")
	addRepeatableStringFlag(apiCmd, "query", "Query parameter as key=value (repeatable)")
	addRepeatableStringFlag(apiCmd, "header", "Extra request header as key=value (repeatable)")
	apiCmd.Flags().Bool("paginate", false, "Automatically follow pagination to fetch all pages")
	apiCmd.Flags().String("list-key", "", "Force the response field name to drain as the list (default: auto-detect the first array-valued field, e.g. 'list', 'automationExecutions', 'automations')")
	allowEmpty(apiCmd, emptyIsOmitted, "method", "body", "body-file", "list-key")
	markRequired(apiCmd, "path")
	addLimitFlag(apiCmd)
	rootCmd.AddCommand(apiCmd)
}

// warnPartialResult warns when a one-page read discards a next-page cursor.
// A non-empty nextPageToken means more results exist; staying silent would
// leave one page at exit 0 indistinguishable from a complete result. It says
// "partial", not "the first page": --query page_token=X makes this page N.
// --paginate is offered, not promised: on an endpoint whose cursor never
// advances it aborts, still a clear failure rather than this silent one.
func warnPartialResult(cmd *cobra.Command, data []byte, path string) {
	if tok := nextPageTokenOf(data); tok != "" {
		notice(cmd, noticeInfo, fmt.Sprintf(
			"Warning: %s returned a partial result — more pages exist and c1i is discarding the cursor to them. Pass --paginate to fetch the rest (which emits NDJSON rows rather than a single JSON object).", redact.URL(path)), nil)
	}
}

// apiPageParser decodes one --paginate page into its raw items and cursor.
func apiPageParser(listKey string) func([]byte) ([]json.RawMessage, string, error) {
	listKeySeen := listKey == ""
	return func(data []byte) ([]json.RawMessage, string, error) {
		items, next, err := extractListAndToken(data, listKey)
		if notArray := (*listKeyNotArrayError)(nil); errors.As(err, &notArray) {
			return nil, "", &usageError{fmt.Errorf("--list-key %q is not an array in the response: %w", listKey, notArray.err)}
		}
		if err != nil {
			return nil, "", &nonJSONResponseError{fmt.Errorf("failed to parse response: %w", err)}
		}
		if !listKeySeen {
			if listKeySeen, err = checkListKeyPresent(data, listKey); err != nil {
				return nil, "", err
			}
		}
		return items, next, nil
	}
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

// nextPageTokenOf returns the response's nextPageToken, or "" when the body is
// not a JSON object or carries no cursor. Shared with extractListAndToken so
// the two pagination paths cannot disagree about what a cursor is.
func nextPageTokenOf(data []byte) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	return nextPageTokenFromMap(raw)
}

func nextPageTokenFromMap(raw map[string]json.RawMessage) string {
	v, ok := raw["nextPageToken"]
	if !ok {
		return ""
	}
	var tok string
	// Ignore unmarshal error: a missing/null token is legitimately empty.
	_ = json.Unmarshal(v, &tok)
	return tok
}

// listKeyNotArrayError is a named list key holding a non-array. Only `api`
// makes it a usage error: other callers' keys are fixed, so there it is the
// server's shape.
type listKeyNotArrayError struct {
	key string
	err error
}

func (e *listKeyNotArrayError) Error() string {
	return fmt.Sprintf("field %q is not an array: %v", e.key, e.err)
}

func (e *listKeyNotArrayError) Unwrap() error { return e.err }

// errNullBody: a POST/PUT/PATCH body must be an object; a JSON null decodes
// to a nil map, which --paginate would write the page token into.
var errNullBody = &usageError{fmt.Errorf("--body/--body-file must be a JSON object, not null")}

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

	nextPageToken := nextPageTokenFromMap(raw)

	if listKey != "" {
		v, ok := raw[listKey]
		if !ok {
			return nil, nextPageToken, nil
		}
		var items []json.RawMessage
		if err := json.Unmarshal(v, &items); err != nil {
			return nil, nextPageToken, &listKeyNotArrayError{key: listKey, err: err}
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

// checkListKeyPresent reports whether the page carries listKey, and rejects
// one absent from a page that has other array fields: draining it would print
// 0 rows at exit 0. A page with no arrays at all is fine, since protojson
// omits an empty repeated field. Callers stop checking once the key is seen,
// so an empty first page beside an unrelated non-empty array still errors.
func checkListKeyPresent(data []byte, listKey string) (bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, nil // extractListAndToken already parsed it
	}
	if _, ok := raw[listKey]; ok {
		return true, nil
	}
	var arrays []string
	for k, v := range raw {
		if bytes.HasPrefix(bytes.TrimSpace(v), []byte("[")) {
			arrays = append(arrays, k)
		}
	}
	if len(arrays) == 0 {
		return false, nil
	}
	sort.Strings(arrays)
	return false, &usageError{fmt.Errorf("--list-key %q is not in the response; its array fields are: %s", listKey, strings.Join(arrays, ", "))}
}

// itemIdentity identifies a list item for the stuck-cursor guard: its string
// "id" when present, else its canonical JSON (re-marshaling sorts keys;
// UseNumber keeps large integers distinct).
func itemIdentity(item json.RawMessage) string {
	if id := rawItemID(item); id != "" {
		return id
	}
	dec := json.NewDecoder(bytes.NewReader(item))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return string(item)
	}
	canon, err := json.Marshal(decoded)
	if err != nil {
		return string(item)
	}
	return string(canon)
}

var httpMethodFlag = enumFlag{flag: "method", unknown: enumReject, values: []enumValue{
	{name: "get", api: "GET"},
	{name: "post", api: "POST"},
	{name: "put", api: "PUT"},
	{name: "patch", api: "PATCH"},
	{name: "delete", api: "DELETE"},
}}
