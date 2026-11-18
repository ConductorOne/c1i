package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// bodyFileListCommands are the list/search commands that take a request body
// from --body-file and paginate. TestBodyFileListCommandsAreAllCovered keeps
// the table equal to the command tree.
var bodyFileListCommands = []struct {
	name string
	args []string
	path string
}{
	{"c1i findings search", []string{"findings", "search"}, "/api/v1/findings/search"},
	{"c1i findings audits search", []string{"findings", "audits", "search"}, "/api/v1/search/finding_audits"},
	{"c1i findings shadow-mcp-occurrences search", []string{"findings", "shadow-mcp-occurrences", "search"}, "/api/v1/search/shadow_mcp_occurrences"},
	{"c1i role-mining suggestions search", []string{"role-mining", "suggestions", "search"}, "/api/v1/search/role-mining/suggestions"},
	{"c1i role-mining suggestions users", []string{"role-mining", "suggestions", "users", "sug1"}, "/api/v1/role-mining/suggestions/sug1/users"},
}

func TestBodyFileListCommandsAreAllCovered(t *testing.T) {
	var inTree, inTable []string
	walkCommandTree(func(c *cobra.Command) {
		// api's --paginate splices its own cursor into a raw body by design.
		if c.Name() == "api" || c.Flags().Lookup("page-token") == nil {
			return
		}
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "body-file" || strings.HasSuffix(f.Name, "-file") {
				inTree = append(inTree, c.CommandPath()+" --"+f.Name)
			}
		})
	})
	for _, c := range bodyFileListCommands {
		inTable = append(inTable, c.name+" --body-file")
	}
	sort.Strings(inTree)
	sort.Strings(inTable)
	if strings.Join(inTree, ",") != strings.Join(inTable, ",") {
		t.Errorf("commands with a --*-file flag and --page-token:\n tree  %v\n table %v", inTree, inTable)
	}
}

// pagingStub serves two pages (tokens "" then "tok1") and one last page for
// any other token, recording each request body.
func pagingStub(t *testing.T) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		seen = append(seen, body)
		n := len(seen)
		mu.Unlock()
		if n > 6 {
			t.Errorf("request %d; pagination did not terminate", n)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tok, _ := body["pageToken"].(string)
		next := ""
		if tok == "" {
			next = "tok1"
		}
		id := "row-" + tok
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":[{"id":%q,"findingId":%q,"eventId":%q}],"nextPageToken":%q}`, id, id, id, next)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), seen...)
	}
}

func TestBodyFilePagingFieldsFollowThePaginationContract(t *testing.T) {
	type want struct {
		exit int
		// tokens is the pageToken each request carried, in order ("" = none).
		tokens []string
		// size is the pageSize every request carried (0 = not checked).
		size int
		rows int
		// msg is the exact error text, when set.
		msg string
	}
	refused := want{exit: exitUsage}
	cases := []struct {
		name  string
		body  string
		flags []string
		want  want
	}{
		{"no paging fields, auto-paginates", `{"query":"q"}`, nil, want{tokens: []string{"", "tok1"}, size: defaultPageSize, rows: 2}},
		{"--page-token alone fetches one page", `{"query":"q"}`, []string{"--page-token", "t9"}, want{tokens: []string{"t9"}, rows: 1}},
		{"--page-size alone", `{"query":"q"}`, []string{"--page-size", "7"}, want{tokens: []string{"", "tok1"}, size: 7, rows: 2}},
		{"pageToken in file", `{"pageToken":"x"}`, nil, refused},
		{"page_token in file", `{"page_token":"x"}`, nil, refused},
		{"null pageToken in file", `{"pageToken":null}`, nil, refused},
		{"pageToken in file with --page-token", `{"pageToken":"x"}`, []string{"--page-token", "t9"}, refused},
		{"pageSize in file", `{"pageSize":5}`, nil, refused},
		{"page_size in file", `{"page_size":5}`, nil, refused},
		{"negative pageSize in file", `{"pageSize":-1}`, nil, refused},
		{"pageSize in file with --page-size", `{"pageSize":5}`, []string{"--page-size", "7"}, refused},
		{"both keys in file are all listed", `{"pageSize":5,"pageToken":"x"}`, nil, want{exit: exitUsage, msg: "--body-file must not contain pageSize, pageToken; remove them and use --page-size and --page-token"}},
		{"both in file with both flags", `{"pageSize":5,"pageToken":"x"}`, []string{"--page-size", "7", "--page-token", "t9"}, refused},
	}
	for _, cmdCase := range bodyFileListCommands {
		for _, tc := range cases {
			t.Run(cmdCase.name+"/"+tc.name, func(t *testing.T) {
				srv, requests := pagingStub(t)
				file := filepath.Join(t.TempDir(), "body.json")
				if err := os.WriteFile(file, []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("C1I_URL", mutTestURL)
				args := append(append([]string{}, cmdCase.args...), "--body-file", file)
				args = append(args, tc.flags...)
				res := runCLI(t, func() {
					orig := newListClient
					newListClient = func(*cobra.Command, string) (*client.Client, error) {
						return client.NewForTesting(srv.URL, srv.Client()), nil
					}
					t.Cleanup(func() { newListClient = orig })
				}, nil, args...)

				if res.exit != tc.want.exit {
					t.Fatalf("exit = %d, want %d; stderr=%q", res.exit, tc.want.exit, res.stderr)
				}
				got := requests()
				if tc.want.exit != 0 {
					if len(got) != 0 {
						t.Errorf("a refused body sent %d request(s): %v", len(got), got)
					}
					if !strings.Contains(res.stderr, "--body-file") {
						t.Errorf("stderr %q does not name --body-file", res.stderr)
					}
					if tc.want.msg != "" && !strings.Contains(res.stderr, tc.want.msg) {
						t.Errorf("stderr %q does not contain %q", res.stderr, tc.want.msg)
					}
					return
				}
				if len(got) != len(tc.want.tokens) {
					t.Fatalf("%d requests, want %d: %v", len(got), len(tc.want.tokens), got)
				}
				for i, body := range got {
					tok, _ := body["pageToken"].(string)
					if tok != tc.want.tokens[i] {
						t.Errorf("request %d pageToken = %q, want %q", i, tok, tc.want.tokens[i])
					}
					if tc.want.size != 0 {
						if size, _ := body["pageSize"].(float64); int(size) != tc.want.size {
							t.Errorf("request %d pageSize = %v, want %d", i, body["pageSize"], tc.want.size)
						}
					}
				}
				if rows := len(decodeNDJSONRows(t, res.stdout)); rows != tc.want.rows {
					t.Errorf("rows = %d, want %d", rows, tc.want.rows)
				}
			})
		}
	}
}

// ownedBodyKeyCommands pins, per command, every --body-file key it sets itself
// (and so refuses) and the args that reach its file read.
var ownedBodyKeyCommands = []struct {
	name string
	args []string
	keys []string
	// positionals are the command's positional args by index, which a
	// positional-owned key may equal; extra is JSON object members the file
	// needs besides the key for the request to be valid.
	positionals []string
	extra       string
}{

	{name: "c1i findings search", args: []string{"findings", "search"}, keys: pagingKeys},
	{name: "c1i findings audits search", args: []string{"findings", "audits", "search"}, keys: pagingKeys},
	{name: "c1i findings shadow-mcp-occurrences search", args: []string{"findings", "shadow-mcp-occurrences", "search"}, keys: pagingKeys},
	{name: "c1i role-mining suggestions search", args: []string{"role-mining", "suggestions", "search"}, keys: pagingKeys},
	{name: "c1i role-mining suggestions users", args: []string{"role-mining", "suggestions", "users", "s1"}, keys: append(append([]string(nil), pagingKeys...), "suggestionId", "suggestion_id"), positionals: []string{"s1"}},
	{name: "c1i findings state", args: []string{"findings", "state", "f1"}, keys: []string{"findingId", "finding_id"}, positionals: []string{"f1"}},
	{name: "c1i findings assignee", args: []string{"findings", "assignee", "f1"}, keys: []string{"findingId", "finding_id"}, positionals: []string{"f1"}},
	{name: "c1i findings routing-rules update", args: []string{"findings", "routing-rules", "update", "r1"}, keys: []string{"id"}, positionals: []string{"r1"}},
	{name: "c1i findings transformation-rules update", args: []string{"findings", "transformation-rules", "update", "r1"}, keys: []string{"id"}, positionals: []string{"r1"}},
	{name: "c1i functions update", args: []string{"functions", "update", "f1", "--update-mask", "description"}, keys: []string{"id"}, positionals: []string{"f1"}, extra: `"description":"d","displayName":"n"`},
	{name: "c1i apps resources update", args: []string{"apps", "resources", "update", "r1", "--app-id", "a1", "--resource-type-id", "t1", "--update-mask", "displayName"}, keys: []string{"id"}, positionals: []string{"r1"}, extra: `"displayName":"d"`},
	{name: "c1i automations update", args: []string{"automations", "update", "a1", "--update-mask", "enabled"}, keys: []string{"id"}, positionals: []string{"a1"}, extra: `"enabled":true`},
	{name: "c1i access-reviews templates update", args: []string{"access-reviews", "templates", "update", "t1", "--update-mask", "description"}, keys: []string{"id"}, positionals: []string{"t1"}, extra: `"description":"d","displayName":"n","policyId":"p"`},
	{name: "c1i access-reviews update", args: []string{"access-reviews", "update", "c1", "--update-mask", "displayName"}, keys: []string{"id"}, positionals: []string{"c1"}, extra: `"displayName":"d"`},
	{name: "c1i policies update", args: []string{"policies", "update", "p1", "--update-mask", "description"}, keys: []string{"id"}, positionals: []string{"p1"}, extra: `"description":"d"`},
	{name: "c1i mcp classifiers update", args: []string{"mcp", "classifiers", "update", "k1", "--update-mask", "displayName"}, keys: []string{"id"}, positionals: []string{"k1"}, extra: `"displayName":"d"`},
	{name: "c1i mcp classifiers tool-gates update", args: []string{"mcp", "classifiers", "tool-gates", "update", "g1", "--update-mask", "displayName"}, keys: []string{"id"}, positionals: []string{"g1"}, extra: `"displayName":"d"`},
	{name: "c1i mcp classifiers templates add-rule", args: []string{"mcp", "classifiers", "templates", "add-rule", "k1", "t1"}, keys: []string{"templateId", "template_id"}, positionals: []string{"k1", "t1"}},
	{name: "c1i connectors update", args: []string{"connectors", "update", "cn1", "--app-id", "a1", "--update-mask", "config"}, keys: []string{"id"}, positionals: []string{"cn1"}, extra: `"config":{}`},
	{name: "c1i mcp classifiers templates instantiate", args: []string{"mcp", "classifiers", "templates", "instantiate", "t1"}, keys: []string{"templateId", "template_id"}, positionals: []string{"t1"}},
	{name: "c1i hooks update", args: []string{"hooks", "update", "h1", "--update-mask", "description"}, keys: []string{"id"}, positionals: []string{"h1"}, extra: `"description":"d"`},
	{name: "c1i xaa resource-servers create", args: []string{"xaa", "resource-servers", "create", "--app-id", "a1"}, keys: []string{"appId", "app_id"}},
	{name: "c1i xaa resource-servers update", args: []string{"xaa", "resource-servers", "update", "rs1", "--app-id", "a1", "--update-mask", "description"}, keys: []string{"appId", "app_id", "id"}, positionals: []string{"rs1"}, extra: `"description":"d","displayName":"n","normalizedAudience":"https://a.example.com"`},
}

var pagingKeys = []string{"pageSize", "page_size", "pageToken", "page_token"}

// Every command that declares owned body keys is pinned above, with the exact
// keys, and each key (alone, with any value, and all together) exits 2 before
// any request, naming every offender.
func TestOwnedBodyKeysAreRefusedBeforeAnyRequest(t *testing.T) {
	declared := map[string][]string{}
	walkCommandTree(func(c *cobra.Command) {
		f := c.Flags().Lookup("body-file")
		if f == nil || len(f.Annotations[ownedBodyFieldsAnnotation]) == 0 {
			return
		}
		for _, e := range f.Annotations[ownedBodyFieldsAnnotation] {
			key, _, _ := strings.Cut(e, "\x1f")
			declared[c.CommandPath()] = append(declared[c.CommandPath()], key)
		}
		if !strings.Contains(f.Usage, "must not contain "+strings.Join(declared[c.CommandPath()], ", ")) {
			t.Errorf("%s: --body-file usage %q does not state the refused keys", c.CommandPath(), f.Usage)
		}
	})
	pinned := map[string][]string{}
	for _, c := range ownedBodyKeyCommands {
		pinned[c.name] = c.keys
		if got := append([]string(nil), declared[c.name]...); !sameKeys(got, c.keys) {
			t.Errorf("%s declares %v, pinned %v", c.name, got, c.keys)
		}
	}
	for name := range declared {
		if _, ok := pinned[name]; !ok {
			t.Errorf("%s declares owned body keys but is not pinned in ownedBodyKeyCommands", name)
		}
	}

	for _, c := range ownedBodyKeyCommands {
		flagKeys, argKeys := splitOwnedKeys(t, c.name, c.positionals)
		run := func(t *testing.T, body string) (mutResult, []map[string]any) {
			srv, requests := onePageStub(t)
			file := filepath.Join(t.TempDir(), "body.json")
			if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("C1I_URL", mutTestURL)
			args := append(append([]string{}, c.args...), "--body-file", file)
			res := runCLI(t, func() {
				stubMutationClient(t, srv, 0)
				orig := newListClient
				newListClient = func(*cobra.Command, string) (*client.Client, error) {
					return client.NewForTesting(srv.URL, srv.Client()), nil
				}
				t.Cleanup(func() { newListClient = orig })
			}, nil, args...)
			return res, requests()
		}
		object := func(members ...string) string {
			if c.extra != "" {
				members = append(members, c.extra)
			}
			return "{" + strings.Join(members, ",") + "}"
		}

		// A flag-owned key is refused with any value, null included.
		for _, k := range flagKeys {
			for _, v := range []string{`"x"`, `null`, `5`} {
				t.Run(c.name+"/"+k+"="+v, func(t *testing.T) {
					res, sent := run(t, object(fmt.Sprintf("%q:%s", k, v)))
					if res.exit != exitUsage || len(sent) != 0 {
						t.Fatalf("exit = %d (want %d), %d request(s) sent; stderr=%q", res.exit, exitUsage, len(sent), res.stderr)
					}
					if want := "--body-file must not contain " + k + ";"; !strings.Contains(res.stderr, want) {
						t.Errorf("stderr %q does not contain %q", res.stderr, want)
					}
				})
			}
		}
		// A positional-owned key passes when equal to the positional (one
		// request, the field sent once under its camelCase spelling: the
		// server 400s on both spellings) and is refused, naming key, value and
		// positional, when it is anything else.
		for _, k := range argKeys {
			t.Run(c.name+"/"+k.key+"=equal", func(t *testing.T) {
				res, sent := run(t, object(fmt.Sprintf("%q:%q", k.key, k.value)))
				if res.exit == exitUsage || len(sent) != 1 {
					t.Fatalf("exit = %d, %d request(s) sent; want one request and no usage error; stderr=%q", res.exit, len(sent), res.stderr)
				}
				camel := snakeToCamel(k.key)
				if got := findKey(sent[0], camel); got != k.value {
					t.Errorf("request carried %s = %v, want %q", camel, got, k.value)
				}
				if camel != k.key {
					if got := findKey(sent[0], k.key); got != nil {
						t.Errorf("request carried both %s and %s; the server rejects a field sent twice", camel, k.key)
					}
				}
			})
			for _, v := range []string{`"other"`, `null`, `5`, `["` + k.value + `"]`} {
				t.Run(c.name+"/"+k.key+"="+v, func(t *testing.T) {
					res, sent := run(t, object(fmt.Sprintf("%q:%s", k.key, v)))
					if res.exit != exitUsage || len(sent) != 0 {
						t.Fatalf("exit = %d (want %d), %d request(s) sent; stderr=%q", res.exit, exitUsage, len(sent), res.stderr)
					}
					want := fmt.Sprintf("--body-file %s is %s but %s is %q;", k.key, v, k.owner, k.value)
					if !strings.Contains(res.stderr, want) {
						t.Errorf("stderr %q does not contain %q", res.stderr, want)
					}
				})
			}
		}
		// Every offender is listed in one message.
		t.Run(c.name+"/all", func(t *testing.T) {
			var members []string
			for _, k := range flagKeys {
				members = append(members, fmt.Sprintf("%q:1", k))
			}
			for _, k := range argKeys {
				members = append(members, fmt.Sprintf("%q:1", k.key))
			}
			res, sent := run(t, object(members...))
			if res.exit != exitUsage || len(sent) != 0 {
				t.Fatalf("exit = %d (want %d), %d request(s) sent; stderr=%q", res.exit, exitUsage, len(sent), res.stderr)
			}
			if len(flagKeys) > 0 {
				if want := "--body-file must not contain " + strings.Join(flagKeys, ", ") + ";"; !strings.Contains(res.stderr, want) {
					t.Errorf("stderr %q does not contain %q", res.stderr, want)
				}
			}
			for _, k := range argKeys {
				if want := "--body-file " + k.key + " is 1 but"; !strings.Contains(res.stderr, want) {
					t.Errorf("stderr %q does not contain %q", res.stderr, want)
				}
			}
		})
	}
}

type argOwnedKey struct{ key, owner, value string }

// splitOwnedKeys reads the declared keys of the named command from its flag
// annotation: flag-owned keys, and positional-owned keys with the positional
// value they may equal.
func splitOwnedKeys(t *testing.T, name string, positionals []string) (flagKeys []string, argKeys []argOwnedKey) {
	t.Helper()
	walkCommandTree(func(c *cobra.Command) {
		if c.CommandPath() != name {
			return
		}
		for _, e := range c.Flags().Lookup("body-file").Annotations[ownedBodyFieldsAnnotation] {
			p := strings.Split(e, "\x1f")
			if p[2] == "" {
				flagKeys = append(flagKeys, p[0])
				continue
			}
			pos, _ := strconv.Atoi(p[3])
			if pos >= len(positionals) {
				t.Fatalf("%s: key %s is owned by positional %d, but the table lists %d", name, p[0], pos, len(positionals))
			}
			argKeys = append(argKeys, argOwnedKey{p[0], p[1], positionals[pos]})
		}
	})
	return flagKeys, argKeys
}

// findKey returns the first value of key anywhere in the request body.
func findKey(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		if got, ok := x[key]; ok {
			return got
		}
		for _, child := range x {
			if got := findKey(child, key); got != nil {
				return got
			}
		}
	}
	return nil
}

// onePageStub answers every request with one list page that has no next
// cursor, recording each request body.
func onePageStub(t *testing.T) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		seen = append(seen, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"list":[{"id":"r1","findingId":"f1","eventId":"e1"}],"nextPageToken":""}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), seen...)
	}
}

func sameKeys(a, b []string) bool {
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, ",") == strings.Join(b, ",")
}

// snakeToCamel turns finding_id into findingId.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}
