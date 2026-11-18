package cmd

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The server reads a field's camelCase and snake_case spellings as one field,
// so every --*-file guard must treat them alike. Each twin runs the same
// command with a camelCase file and its snake_case spelling: both must exit
// the same way, send the same requests and print the same preview, keys
// compared folded. A twin whose guard reads only the camelCase key exits 0
// on the snake twin, or sends a second spelling of a filled field.

// spellingRoute answers a lookup of an id ending in "9" with 404 and any
// other with a minimal live object.
func spellingRoute(r *http.Request, n int) mutReply {
	p := r.URL.EscapedPath()
	if p == "/api/v1/probe" {
		if n == 1 {
			return mutReply{body: `{"list":[{"id":"a"}],"nextPageToken":"t2"}`}
		}
		return mutReply{body: `{"list":[{"id":"b"}],"nextPageToken":""}`}
	}
	if r.Method != http.MethodGet {
		return mutReply{body: `{}`}
	}
	if strings.HasSuffix(p, "9") {
		return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
	}
	switch {
	case strings.HasPrefix(p, "/api/v1/policies/"):
		return mutReply{body: `{"policy":{"id":"pol1","policyType":"POLICY_TYPE_CERTIFY"}}`}
	case strings.HasPrefix(p, "/api/v1/functions/"):
		return mutReply{body: `{"function":{"id":"f1","displayName":"current"}}`}
	case strings.HasPrefix(p, "/api/v1/apps/a1/mcp_servers/"):
		return mutReply{body: `{"mcpServer":{"mcpServerCatalogId":"cat-from-server"}}`}
	}
	return mutReply{body: `{"user":{"id":"u1"}}`}
}

type spellingTwin struct {
	name  string
	path  string   // command path after "c1i"
	flag  string   // the file flag the twin exercises
	args  []string // after the path; "@BODY" is the twin's file
	camel string
	snake string
	exit  int  // both twins' exit code
	real  bool // run for real instead of --dry-run
}

// stepsWith wraps one approval arm in a grant policy body.
func stepsWith(arm string) string {
	return `{"displayName":"x","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"approval":{` + arm + `}}]}}}`
}

var spellingTwins = []spellingTwin{
	{name: "template create unknown policy", path: "access-reviews templates create", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"displayName":"x","policyId":"pol9","ownerIds":["u1"]}`, snake: `{"displayName":"x","policy_id":"pol9","ownerIds":["u1"]}`, exit: exitNotFound},
	{name: "template create blank policy", path: "access-reviews templates create", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"displayName":"x","policyId":" ","ownerIds":["u1"]}`, snake: `{"displayName":"x","policy_id":" ","ownerIds":["u1"]}`, exit: exitUsage},
	{name: "template create unknown owner", path: "access-reviews templates create", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"displayName":"x","policyId":"pol1","ownerIds":["u9"]}`, snake: `{"display_name":"x","policy_id":"pol1","owner_ids":["u9"]}`, exit: exitNotFound},
	{name: "template create valid", path: "access-reviews templates create", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"displayName":"x","policyId":"pol1","ownerIds":["u1"]}`, snake: `{"display_name":"x","policy_id":"pol1","owner_ids":["u1"]}`},
	{name: "template update unknown policy", path: "access-reviews templates update", flag: "body-file", args: []string{"t1", "--body-file", "@BODY", "--update-mask", "policyId"},
		camel: `{"displayName":"x","policyId":"pol9"}`, snake: `{"display_name":"x","policy_id":"pol9"}`, exit: exitNotFound},
	{name: "campaign create empty owner", path: "access-reviews create", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"displayName":"x","ownerIds":[""]}`, snake: `{"displayName":"x","owner_ids":[""]}`, exit: exitUsage},
	{name: "campaign create valid", path: "access-reviews create", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"displayName":"x","ownerIds":["u1"]}`, snake: `{"display_name":"x","owner_ids":["u1"]}`},
	{name: "connector create unknown user", path: "connectors create", flag: "body-file", args: []string{"--app-id", "a1", "--body-file", "@BODY"},
		camel: `{"catalogId":"c","userIds":["u9"]}`, snake: `{"catalog_id":"c","user_ids":["u9"]}`, exit: exitNotFound},
	{name: "connector create blank user", path: "connectors create", flag: "body-file", args: []string{"--app-id", "a1", "--body-file", "@BODY"},
		camel: `{"catalogId":"c","userIds":[" "]}`, snake: `{"catalogId":"c","user_ids":[" "]}`, exit: exitUsage},
	{name: "automation create unknown ref", path: "entitlements automation create", flag: "body-file", args: []string{"e1", "--app-id", "a1", "--body-file", "@BODY"},
		camel: `{"displayName":"x","entitlements":{"entitlementRefs":[{"appId":"a1","id":"e9"}]}}`, snake: `{"display_name":"x","entitlements":{"entitlement_refs":[{"app_id":"a1","id":"e9"}]}}`, exit: exitNotFound},
	{name: "automation create refs-file unknown ref", path: "entitlements automation create", flag: "refs-file", args: []string{"e1", "--app-id", "a1", "--display-name", "x", "--refs-file", "@BODY"},
		camel: `[{"appId":"a1","id":"e9"}]`, snake: `[{"app_id":"a1","id":"e9"}]`, exit: exitNotFound},
	{name: "automation update refs-file unknown ref", path: "entitlements automation update", flag: "refs-file", args: []string{"e1", "--app-id", "a1", "--display-name", "x", "--refs-file", "@BODY"},
		camel: `[{"appId":"a1","id":"e9"}]`, snake: `[{"app_id":"a1","id":"e9"}]`, exit: exitNotFound},
	{name: "requestable entitlements blank ref", path: "access-profiles requestable-entitlements set", flag: "refs-file", args: []string{"ap1", "--refs-file", "@BODY"},
		camel: `[{"appId":"","id":"e1"}]`, snake: `[{"app_id":"","id":"e1"}]`, exit: exitUsage},
	{name: "requestable entitlements valid", path: "access-profiles requestable-entitlements set", flag: "refs-file", args: []string{"ap1", "--refs-file", "@BODY"},
		camel: `[{"appId":"a1","id":"e1"}]`, snake: `[{"app_id":"a1","id":"e1"}]`},
	{name: "bundle run refs-file blank ref", path: "access-profiles bundle-automation run", flag: "refs-file", args: []string{"ap1", "--refs-file", "@BODY"},
		camel: `[{"appId":"a1","id":""}]`, snake: `[{"app_id":"a1","id":""}]`, exit: exitUsage},
	{name: "bundle create non-bool", path: "access-profiles bundle-automation create", flag: "body-file", args: []string{"ap1", "--body-file", "@BODY"},
		camel: `{"createTasks":"yes","entitlements":{"entitlementRefs":[]}}`, snake: `{"create_tasks":"yes","entitlements":{"entitlement_refs":[]}}`, exit: exitUsage},
	{name: "bundle set valid", path: "access-profiles bundle-automation set", flag: "body-file", args: []string{"ap1", "--body-file", "@BODY"},
		camel: `{"disableCircuitBreaker":true,"entitlements":{"entitlementRefs":[{"appId":"a1","id":"e1"}]}}`, snake: `{"disable_circuit_breaker":true,"entitlements":{"entitlement_refs":[{"app_id":"a1","id":"e1"}]}}`},
}

// scopeTwins cover the six scope replacement commands, campaign and template.
func scopeTwins() []spellingTwin {
	var out []spellingTwin
	for _, owner := range []string{"access-reviews", "access-reviews templates"} {
		out = append(out,
			spellingTwin{name: owner + " scope set unknown ref", path: owner + " scope set", flag: "body-file", args: []string{"x1", "--body-file", "@BODY"},
				camel: `{"entitlements":[{"appId":"a1","appEntitlementId":"e9"}]}`, snake: `{"entitlements":[{"app_id":"a1","app_entitlement_id":"e9"}]}`, exit: exitNotFound},
			spellingTwin{name: owner + " scope set-excluded unknown ref", path: owner + " scope set-excluded", flag: "body-file", args: []string{"x1", "--body-file", "@BODY"},
				camel: `{"excludedEntitlements":[{"appId":"a1","appEntitlementId":"e9"}]}`, snake: `{"excluded_entitlements":[{"app_id":"a1","app_entitlement_id":"e9"}]}`, exit: exitNotFound},
			spellingTwin{name: owner + " scope set-by-resource-type unknown ref", path: owner + " scope set-by-resource-type", flag: "body-file", args: []string{"x1", "--body-file", "@BODY"},
				camel: `{"resourceTypeSelections":[{"appId":"a1","resourceTypeId":"r9"}]}`, snake: `{"resource_type_selections":[{"app_id":"a1","resource_type_id":"r9"}]}`, exit: exitNotFound},
		)
	}
	return out
}

// policyTwins cover every key the policy guards read, in create and update.
func policyTwins() []spellingTwin {
	arms := []struct{ name, camel, snake string }{
		{"no-fallback arm with fallback", `"appOwners":{"fallback":true,"fallbackUserIds":["u1"]}`, `"app_owners":{"fallback":true,"fallbackUserIds":["u1"]}`},
		{"fallbackUserIds on a no-fallback arm", `"appOwners":{"fallbackUserIds":["u1"]}`, `"appOwners":{"fallback_user_ids":["u1"]}`},
		{"entitlementOwners fallback with no users", `"entitlementOwners":{"fallback":true}`, `"entitlement_owners":{"fallback":true}`},
		{"resourceOwners fallback with no users", `"resourceOwners":{"fallback":true}`, `"resource_owners":{"fallback":true}`},
		{"group fallback with no groups", `"manager":{"fallback":true,"isGroupFallbackEnabled":true,"fallbackUserIds":["u1"]}`, `"manager":{"fallback":true,"is_group_fallback_enabled":true,"fallbackUserIds":["u1"]}`},
		{"manager assignedUserIds", `"manager":{"assignedUserIds":["u1"]}`, `"manager":{"assigned_user_ids":["u1"]}`},
	}
	var out []spellingTwin
	for _, a := range arms {
		out = append(out, spellingTwin{name: "policy create " + a.name, path: "policies create", flag: "body-file", args: []string{"--body-file", "@BODY"},
			camel: stepsWith(a.camel), snake: stepsWith(a.snake), exit: exitUsage})
	}
	// Each twin respells one field, so the rest of the arm stays valid.
	const mode, action = `"agentMode":"APPROVAL_AGENT_MODE_FULL_CONTROL"`, `"agentFailureAction":"APPROVAL_AGENT_FAILURE_ACTION_SKIP"`
	valid := []struct{ name, camel, snake string }{
		{"agentMode", mode + "," + action, `"agent_mode":"APPROVAL_AGENT_MODE_FULL_CONTROL",` + action},
		{"agentFailureAction", mode + "," + action, mode + `,"agent_failure_action":"APPROVAL_AGENT_FAILURE_ACTION_SKIP"`},
		{"policyIds", `"agentMode":"APPROVAL_AGENT_MODE_CHANGE_POLICY_ONLY",` + action + `,"policyIds":["pol1"]`,
			`"agentMode":"APPROVAL_AGENT_MODE_CHANGE_POLICY_ONLY",` + action + `,"policy_ids":["pol1"]`},
		{"reassignToUserIds", mode + `,"agentFailureAction":"APPROVAL_AGENT_FAILURE_ACTION_REASSIGN_TO_USERS","reassignToUserIds":["u1"]`,
			mode + `,"agentFailureAction":"APPROVAL_AGENT_FAILURE_ACTION_REASSIGN_TO_USERS","reassign_to_user_ids":["u1"]`},
	}
	for _, a := range valid {
		out = append(out, spellingTwin{name: "policy create agent " + a.name, path: "policies create", flag: "body-file", args: []string{"--body-file", "@BODY"},
			camel: stepsWith(`"agent":{` + a.camel + `}`), snake: stepsWith(`"agent":{` + a.snake + `}`)})
	}
	out = append(out,
		spellingTwin{name: "policy create agentUserId", path: "policies create", flag: "body-file", args: []string{"--body-file", "@BODY"},
			camel: stepsWith(`"agent":{` + mode + "," + action + `,"agentUserId":"u1"}`), snake: stepsWith(`"agent":{` + mode + "," + action + `,"agent_user_id":"u1"}`), exit: exitUsage},
		spellingTwin{name: "policy create fallbackGroupIds", path: "policies create", flag: "body-file", args: []string{"--body-file", "@BODY"},
			camel: stepsWith(`"manager":{"fallback":true,"isGroupFallbackEnabled":true,"fallbackGroupIds":["g1"]}`),
			snake: stepsWith(`"manager":{"fallback":true,"isGroupFallbackEnabled":true,"fallback_group_ids":["g1"]}`)},
	)
	out = append(out,
		spellingTwin{name: "policy create unspecified type", path: "policies create", flag: "body-file", args: []string{"--body-file", "@BODY"},
			camel: `{"displayName":"x","policyType":"POLICY_TYPE_UNSPECIFIED","policySteps":{"grant":{"steps":[{"accept":{}}]}}}`,
			snake: `{"display_name":"x","policy_type":"POLICY_TYPE_UNSPECIFIED","policy_steps":{"grant":{"steps":[{"accept":{}}]}}}`, exit: exitUsage},
		spellingTwin{name: "policy create valid", path: "policies create", flag: "body-file", args: []string{"--body-file", "@BODY"},
			camel: `{"displayName":"x","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"accept":{}}]}},"rules":[{"condition":"true","stepKey":"grant"}]}`,
			snake: `{"display_name":"x","policy_type":"POLICY_TYPE_GRANT","policy_steps":{"grant":{"steps":[{"accept":{}}]}},"rules":[{"condition":"true","step_key":"grant"}]}`},
		spellingTwin{name: "policy create rule two outcomes", path: "policies create", flag: "body-file", args: []string{"--body-file", "@BODY"},
			camel: `{"displayName":"x","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"accept":{}}]}},"rules":[{"condition":"true","policyId":"pol1","policyKey":"grant"}]}`,
			snake: `{"displayName":"x","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"accept":{}}]}},"rules":[{"condition":"true","policy_id":"pol1","policyKey":"grant"}]}`, exit: exitUsage},
		spellingTwin{name: "policy create steps-file no-fallback arm", path: "policies create", flag: "steps-file", args: []string{"--display-name", "x", "--policy-type", "grant", "--steps-file", "@BODY"},
			camel: `[{"approval":{"appOwners":{"fallback":true}}}]`, snake: `[{"approval":{"app_owners":{"fallback":true}}}]`, exit: exitUsage},
		spellingTwin{name: "policy create rules-file unknown step", path: "policies create", flag: "rules-file", args: []string{"--display-name", "x", "--policy-type", "grant", "--steps-file", `@[{"accept":{}}]`, "--rules-file", "@BODY"},
			camel: `[{"condition":"true","policyId":"pol1","stepKey":"nope"}]`, snake: `[{"condition":"true","policyId":"pol1","step_key":"nope"}]`, exit: exitUsage},
		spellingTwin{name: "policy update empty steps", path: "policies update", flag: "body-file", args: []string{"p1", "--body-file", "@BODY", "--update-mask", "policySteps"},
			camel: `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[]}}}`, snake: `{"policy_type":"POLICY_TYPE_GRANT","policy_steps":{"grant":{"steps":[]}}}`, exit: exitUsage},
		spellingTwin{name: "policy update unspecified type", path: "policies update", flag: "body-file", args: []string{"p1", "--body-file", "@BODY", "--update-mask", "policyType"},
			camel: `{"policyType":"POLICY_TYPE_UNSPECIFIED"}`, snake: `{"policy_type":"POLICY_TYPE_UNSPECIFIED"}`, exit: exitUsage},
		spellingTwin{name: "policy update steps-file no-fallback arm", path: "policies update", flag: "steps-file", args: []string{"p1", "--policy-type", "grant", "--steps-file", "@BODY"},
			camel: `[{"approval":{"appOwners":{"fallback":true}}}]`, snake: `[{"approval":{"app_owners":{"fallback":true}}}]`, exit: exitUsage},
		spellingTwin{name: "policy update rules-file two outcomes", path: "policies update", flag: "rules-file", args: []string{"p1", "--rules-file", "@BODY"},
			camel: `[{"condition":"true","stepKey":"grant","policyKey":"grant"}]`, snake: `[{"condition":"true","step_key":"grant","policyKey":"grant"}]`, exit: exitUsage},
	)
	return out
}

var otherSpellingTwins = []spellingTwin{
	{name: "function update keeps the body's name", path: "functions update", flag: "body-file", args: []string{"f1", "--body-file", "@BODY", "--update-mask", "displayName"},
		camel: `{"displayName":"new"}`, snake: `{"display_name":"new"}`},
	{name: "credentials keep the file's catalog id", path: "mcp servers update-credentials", flag: "hosted-config-file", args: []string{"c1", "--app-id", "a1", "--type", "hosted", "--hosted-config-file", "@BODY", "--update-mask", "bearerToken"},
		camel: `{"mcpServerCatalogId":"cat-file","bearerToken":{"token":"t"}}`, snake: `{"mcp_server_catalog_id":"cat-file","bearer_token":{"token":"t"}}`},
	{name: "credentials header with a newline", path: "mcp servers update-credentials", flag: "external-config-file", args: []string{"c1", "--app-id", "a1", "--type", "external", "--external-config-file", "@BODY"},
		camel: `{"customHeader":{"headerName":"X-A","headerValue":"a\nb"}}`, snake: `{"custom_header":{"header_name":"X-A","header_value":"a\nb"}}`, exit: exitUsage},
	{name: "register hosted header with a newline", path: "mcp servers register", flag: "hosted-config-file", args: []string{"--app-id", "a1", "--type", "hosted", "--display-name", "x", "--hosted-config-file", "@BODY"},
		camel: `{"mcpServerCatalogId":"c","customHeader":{"headerName":"X-A","headerValue":"a\nb"}}`, snake: `{"mcp_server_catalog_id":"c","custom_header":{"header_name":"X-A","header_value":"a\nb"}}`, exit: exitUsage},
	{name: "register external header with a newline", path: "mcp servers register", flag: "external-config-file", args: []string{"--app-id", "a1", "--type", "external", "--display-name", "x", "--external-config-file", "@BODY"},
		camel: `{"url":"https://mcp.example","customHeader":{"headerName":"X-A","headerValue":"a\nb"}}`, snake: `{"url":"https://mcp.example","custom_header":{"header_name":"X-A","header_value":"a\nb"}}`, exit: exitUsage},
	{name: "test-connection header with a newline", path: "mcp servers test-connection", flag: "external-config-file", args: []string{"--external-config-file", "@BODY"},
		camel: `{"url":"https://mcp.example","customHeader":{"headerName":"X-A","headerValue":"a\nb"}}`, snake: `{"url":"https://mcp.example","custom_header":{"header_name":"X-A","header_value":"a\nb"}}`, exit: exitUsage},
	{name: "classifier update needs a name", path: "mcp classifiers update", flag: "body-file", args: []string{"cl1", "--body-file", "@BODY", "--update-mask", "description"},
		camel: `{"displayName":"x"}`, snake: `{"display_name":"x"}`},
	{name: "org notifications need settings", path: "settings notifications org update", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"channelSettings":{"email":{}}}`, snake: `{"channel_settings":{"email":{}}}`},
	{name: "user notifications empty settings", path: "settings notifications user update", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"channelSettings":{}}`, snake: `{"channel_settings":{}}`, exit: exitUsage},
	{name: "edge settings need a category", path: "findings settings edge update", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"subcategorySettings":[]}`, snake: `{"subcategory_settings":[]}`, exit: exitUsage},
	{name: "edge settings valid", path: "findings settings edge update", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"subcategorySettings":[{"subcategory":"x","enabled":false}]}`, snake: `{"subcategory_settings":[{"subcategory":"x","enabled":false}]}`},
	{name: "domains explicit empty", path: "settings domains set", flag: "body-file", args: []string{"--body-file", "@BODY"},
		camel: `{"newDomains":[]}`, snake: `{"new_domains":[]}`},
	{name: "app mapper cases", path: "apps update", flag: "app-user-mapper-file", args: []string{"a1", "--app-user-mapper-file", "@BODY"},
		camel: `{"mappingCases":[{"userKeyCel":"a","appUserKeyCel":"b"}]}`, snake: `{"mapping_cases":[{"user_key_cel":"a","app_user_key_cel":"b"}]}`},
	{name: "app mapper non-string cel", path: "apps update", flag: "app-user-mapper-file", args: []string{"a1", "--app-user-mapper-file", "@BODY"},
		camel: `{"mappingCases":[{"userKeyCel":5}]}`, snake: `{"mappingCases":[{"user_key_cel":5}]}`, exit: exitUsage},
	{name: "trust update unknown role", path: "service-principals trusts update", flag: "body-file", args: []string{"tr1", "--service-principal-id", "sp1", "--body-file", "@BODY", "--update-mask", "scopedRoleIds"},
		camel: `{"scopedRoleIds":["r9"]}`, snake: `{"scoped_role_ids":["r9"]}`, exit: exitNotFound},
	{name: "resource server unknown hook function", path: "xaa resource-servers create", flag: "body-file", args: []string{"--app-id", "a1", "--body-file", "@BODY"},
		camel: `{"displayName":"x","modifyClaimsHook":{"functionId":"f9"}}`, snake: `{"display_name":"x","modify_claims_hook":{"function_id":"f9"}}`, exit: exitNotFound},
	{name: "resource server update masked name", path: "xaa resource-servers update", flag: "body-file", args: []string{"rs1", "--app-id", "a1", "--body-file", "@BODY", "--update-mask", "displayName"},
		camel: `{"description":"d"}`, snake: `{"description":"d"}`, exit: exitUsage},
	{name: "finding state id mismatch", path: "findings state", flag: "body-file", args: []string{"fd1", "--body-file", "@BODY"},
		camel: `{"findingId":"other"}`, snake: `{"finding_id":"other"}`, exit: exitUsage},
	{name: "suggestion users paging key", path: "role-mining suggestions users", flag: "body-file", args: []string{"s1", "--body-file", "@BODY"},
		camel: `{"pageSize":5}`, snake: `{"page_size":5}`, exit: exitUsage},
	{name: "resource server create owned app id", path: "xaa resource-servers create", flag: "body-file", args: []string{"--app-id", "a1", "--body-file", "@BODY"},
		camel: `{"appId":"a1","displayName":"x"}`, snake: `{"app_id":"a1","display_name":"x"}`, exit: exitUsage},
	{name: "api paginate keeps the body's token key", path: "api", flag: "body-file", args: []string{"--path", "/api/v1/probe", "--paginate", "--body-file", "@BODY"},
		camel: `{"pageToken":"","pageSize":1}`, snake: `{"page_token":"","page_size":1}`, real: true},
}

func allSpellingTwins() []spellingTwin {
	out := append([]spellingTwin(nil), spellingTwins...)
	out = append(out, scopeTwins()...)
	out = append(out, policyTwins()...)
	return append(out, otherSpellingTwins...)
}

type twinRun struct {
	exit     int
	requests []string
	stdout   string // keys folded, for comparing twins
	raw      string // stdout as printed
}

func runSpellingTwin(t *testing.T, tc spellingTwin, file string) twinRun {
	t.Helper()
	restoreCmdWriters(t)
	clearViperOverrides(t, "dry_run")
	s := newMutServer(t, spellingRoute)
	var args []string
	for _, a := range tc.args {
		if a == "@BODY" {
			a = "@" + file
		}
		args = append(args, a)
	}
	argv := mutationRow{cmd: tc.path, args: args}.argv(t)
	if !tc.real {
		argv = append(argv, "--dry-run")
	}
	r := runMutationCLI(t, s.srv, 0, nil, argv...)
	run := twinRun{exit: r.exit, stdout: foldedJSONLines(r.stdout), raw: r.stdout}
	for _, q := range s.requests() {
		run.requests = append(run.requests, q.method+" "+q.rawPath+" "+foldedJSONLines(q.body))
	}
	return run
}

// foldedJSONLines re-encodes each JSON document in s with every object key
// folded (foldMaskName), so twins compare spelling-blind; a second key folding
// alike (in sorted key order) becomes a visible marker. Use it to compare
// twins only, never to assert which key was sent.
func foldedJSONLines(s string) string {
	dec := json.NewDecoder(strings.NewReader(s))
	var out []string
	for dec.More() {
		var v any
		if err := dec.Decode(&v); err != nil {
			return "UNPARSED " + s
		}
		b, _ := json.Marshal(foldKeys(v))
		out = append(out, string(b))
	}
	return strings.Join(out, "\n")
}

func foldKeys(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			e := x[k]
			f := foldMaskName(k)
			if _, dup := out[f]; dup {
				f = "DUPLICATE:" + f
			}
			out[f] = foldKeys(e)
		}
		return out
	case []any:
		for i := range x {
			x[i] = foldKeys(x[i])
		}
	}
	return v
}

// TestBodyFileGuardsAcceptSnakeCase: each twin's snake_case file behaves
// exactly like its camelCase one.
func TestBodyFileGuardsAcceptSnakeCase(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, tc := range allSpellingTwins() {
		t.Run(tc.name, func(t *testing.T) {
			camel := runSpellingTwin(t, tc, tc.camel)
			if camel.exit != tc.exit {
				t.Fatalf("camelCase exit %d, want %d; requests %v\n%s", camel.exit, tc.exit, camel.requests, camel.stdout)
			}
			snake := runSpellingTwin(t, tc, tc.snake)
			if snake.exit != camel.exit {
				t.Errorf("snake_case exit %d, camelCase %d; snake requests %v", snake.exit, camel.exit, snake.requests)
			}
			if strings.Join(snake.requests, "\n") != strings.Join(camel.requests, "\n") {
				t.Errorf("requests differ\nsnake %v\ncamel %v", snake.requests, camel.requests)
			}
			if snake.stdout != camel.stdout {
				t.Errorf("output differs\nsnake %s\ncamel %s", snake.stdout, camel.stdout)
			}
		})
	}
}

// TestBodyFileRefusesBothSpellings: a field set in both spellings exits 2
// before any request.
func TestBodyFileRefusesBothSpellings(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := []spellingTwin{
		{name: "template policy", path: "access-reviews templates create", args: []string{"--body-file", "@BODY"},
			camel: `{"displayName":"x","policyId":"pol1","policy_id":"pol2","ownerIds":["u1"]}`},
		{name: "campaign owners", path: "access-reviews create", args: []string{"--body-file", "@BODY"},
			camel: `{"displayName":"x","ownerIds":["u1"],"owner_ids":[""]}`},
		{name: "connector users", path: "connectors create", args: []string{"--app-id", "a1", "--body-file", "@BODY"},
			camel: `{"catalogId":"c","userIds":["u1"],"user_ids":["u2"]}`},
		{name: "automation refs", path: "entitlements automation create", args: []string{"e1", "--app-id", "a1", "--body-file", "@BODY"},
			camel: `{"displayName":"x","entitlements":{"entitlementRefs":[],"entitlement_refs":[]}}`},
		{name: "ref element", path: "access-profiles requestable-entitlements set", args: []string{"ap1", "--refs-file", "@BODY"},
			camel: `[{"appId":"a1","app_id":"a2"}]`},
		{name: "scope list", path: "access-reviews scope set-excluded", args: []string{"x1", "--body-file", "@BODY"},
			camel: `{"excludedEntitlements":[],"excluded_entitlements":[]}`},
		{name: "policy steps", path: "policies update", args: []string{"p1", "--body-file", "@BODY", "--update-mask", "policySteps"},
			camel: `{"policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"accept":{}}]}},"policy_steps":{}}`},
		{name: "approval arm", path: "policies create", args: []string{"--body-file", "@BODY"},
			camel: stepsWith(`"appOwners":{},"app_owners":{}`)},
		{name: "arm field", path: "policies create", args: []string{"--body-file", "@BODY"},
			camel: stepsWith(`"manager":{"fallbackUserIds":["u1"],"fallback_user_ids":[]}`)},
		{name: "rule outcome", path: "policies create", args: []string{"--body-file", "@BODY"},
			camel: `{"displayName":"x","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"accept":{}}]}},"rules":[{"condition":"true","stepKey":"grant","step_key":"grant"}]}`},
		{name: "function name", path: "functions update", args: []string{"f1", "--body-file", "@BODY", "--update-mask", "displayName"},
			camel: `{"displayName":"a","display_name":"b"}`},
		{name: "catalog id", path: "mcp servers update-credentials", args: []string{"c1", "--app-id", "a1", "--type", "hosted", "--hosted-config-file", "@BODY", "--update-mask", "bearerToken,mcpServerCatalogId"},
			camel: `{"mcpServerCatalogId":"a","mcp_server_catalog_id":"b","bearerToken":{"token":"t"}}`},
		{name: "classifier name", path: "mcp classifiers update", args: []string{"cl1", "--body-file", "@BODY", "--update-mask", "description"},
			camel: `{"displayName":"a","display_name":"b"}`},
		{name: "notification settings", path: "settings notifications org update", args: []string{"--body-file", "@BODY"},
			camel: `{"channelSettings":{"email":{}},"channel_settings":{}}`},
		{name: "domains", path: "settings domains set", args: []string{"--body-file", "@BODY"},
			camel: `{"newDomains":[],"new_domains":[]}`},
		{name: "bundle flag", path: "access-profiles bundle-automation create", args: []string{"ap1", "--body-file", "@BODY"},
			camel: `{"createTasks":true,"create_tasks":false,"entitlements":{"entitlementRefs":[]}}`},
		{name: "mapper cases", path: "apps update", args: []string{"a1", "--app-user-mapper-file", "@BODY"},
			camel: `{"mappingCases":[{"userKeyCel":"a"}],"mapping_cases":[]}`},
		{name: "xaa required field", path: "xaa resource-servers update", args: []string{"rs1", "--app-id", "a1", "--body-file", "@BODY", "--update-mask", "description"},
			camel: `{"displayName":"a","display_name":"b"}`},
		{name: "trust scoped roles", path: "service-principals trusts update", args: []string{"tr1", "--service-principal-id", "sp1", "--body-file", "@BODY", "--update-mask", "displayName"},
			camel: `{"scopedRoleIds":["r1"],"scoped_role_ids":["r2"]}`},
		{name: "trust cidrs", path: "service-principals trusts update", args: []string{"tr1", "--service-principal-id", "sp1", "--body-file", "@BODY", "--update-mask", "displayName"},
			camel: `{"allowSourceCidrs":[],"allow_source_cidrs":[]}`},
		{name: "edge settings later key", path: "findings settings edge update", args: []string{"--body-file", "@BODY"},
			camel: `{"settings":[{"category":"x"}],"subcategorySettings":[],"subcategory_settings":[]}`},
		{name: "config header arm", path: "mcp servers register", args: []string{"--app-id", "a1", "--type", "external", "--display-name", "x", "--external-config-file", "@BODY"},
			camel: `{"url":"https://mcp.example","customHeader":{"headerName":"X-A"},"custom_header":{"headerName":"X-B"}}`},
		{name: "config header field", path: "mcp servers test-connection", args: []string{"--external-config-file", "@BODY"},
			camel: `{"url":"https://mcp.example","customHeader":{"headerName":"X-A","header_name":"X-B"}}`},
		{name: "config with explicit mask", path: "mcp servers update-credentials", args: []string{"c1", "--app-id", "a1", "--type", "external", "--external-config-file", "@BODY", "--update-mask", "bearerToken"},
			camel: `{"bearerToken":{"token":"a"},"bearer_token":{"token":"b"}}`},
		{name: "config with derived mask", path: "mcp servers update-credentials", args: []string{"c1", "--app-id", "a1", "--type", "external", "--external-config-file", "@BODY"},
			camel: `{"url":"https://mcp.example","transportType":"x","transport_type":"y"}`},
		{name: "owned positional key", path: "findings state", args: []string{"fd1", "--body-file", "@BODY"},
			camel: `{"findingId":"fd1","finding_id":"fd1"}`},
		{name: "claims hook before the app lookup", path: "xaa resource-servers create", args: []string{"--app-id", "a1", "--body-file", "@BODY"},
			camel: `{"displayName":"x","modifyClaimsHook":{"functionId":"f1","function_id":"f2"}}`},
		{name: "api page token", path: "api", args: []string{"--path", "/api/v1/probe", "--paginate", "--body-file", "@BODY"},
			camel: `{"pageToken":"","page_token":""}`, real: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := runSpellingTwin(t, tc, tc.camel)
			if r.exit != exitUsage || len(r.requests) != 0 {
				t.Errorf("exit %d, requests %v; want 2 and none", r.exit, r.requests)
			}
		})
	}
}

// spellingExempt names each JSON file flag with no spelling twin, and why.
var spellingExempt = map[string]string{
	"access-reviews reports generate --body-file":               "sent verbatim, no guard reads it",
	"access-reviews update --body-file":                         "sent verbatim; the owned id key is covered by findings state",
	"apps resources create --body-file":                         "sent verbatim, no guard reads it",
	"apps resources update --body-file":                         "sent verbatim; the owned id key is covered by findings state",
	"attribute-mappings create --body-file":                     "sent verbatim, no guard reads it",
	"attribute-mappings create --fallbacks-file":                "sent verbatim, no guard reads it",
	"attribute-mappings update --fallbacks-file":                "sent verbatim, no guard reads it",
	"automations create --body-file":                            "sent verbatim, no guard reads it",
	"automations create --steps-file":                           "sent verbatim, no guard reads it",
	"automations create --triggers-file":                        "sent verbatim, no guard reads it",
	"automations execute --context-file":                        "free-form context map: its keys are data",
	"automations update --body-file":                            "sent verbatim; the owned id key is covered by findings state",
	"connectors sync-compare --body-file":                       "sent verbatim, no guard reads it",
	"connectors update --body-file":                             "sent verbatim; the owned id key is covered by findings state",
	"findings assignee --body-file":                             "owned findingId key, same declaration as findings state",
	"findings audits search --body-file":                        "owned paging keys, covered by role-mining suggestions users",
	"findings bulk-create-tasks --body-file":                    "sent verbatim, no guard reads it",
	"findings bulk-state --body-file":                           "sent verbatim, no guard reads it",
	"findings create --body-file":                               "sent verbatim, no guard reads it",
	"findings routing-rules create --body-file":                 "sent verbatim, no guard reads it",
	"findings routing-rules update --body-file":                 "sent verbatim; the owned id key is covered by findings state",
	"findings search --body-file":                               "owned paging keys, covered by role-mining suggestions users",
	"findings settings update --body-file":                      "sent verbatim, no guard reads it",
	"findings shadow-mcp-occurrences search --body-file":        "owned paging keys, covered by role-mining suggestions users",
	"findings transformation-rules create --body-file":          "sent verbatim, no guard reads it",
	"findings transformation-rules update --body-file":          "sent verbatim; the owned id key is covered by findings state",
	"functions create --file":                                   "function source code, not JSON",
	"functions invoke --input-file":                             "free-form function input",
	"functions update --file":                                   "function source code, not JSON",
	"hooks create --body-file":                                  "sent verbatim, no guard reads it",
	"hooks update --body-file":                                  "sent verbatim; the owned id key is covered by findings state",
	"mcp classifiers bindings create --body-file":               "sent verbatim, no guard reads it",
	"mcp classifiers create --body-file":                        "sent verbatim, no guard reads it",
	"mcp classifiers policy update --body-file":                 "sent verbatim, no guard reads it",
	"mcp classifiers templates add-rule --body-file":            "owned templateId key, same declaration as findings state",
	"mcp classifiers templates instantiate --body-file":         "owned templateId key, same declaration as findings state",
	"mcp classifiers tool-gates create --body-file":             "sent verbatim, no guard reads it",
	"mcp classifiers tool-gates update --body-file":             "sent verbatim; the owned id key is covered by findings state",
	"requests create action --form-values-file":                 "free-form form values: its keys are data",
	"requests create resource-action --form-values-file":        "free-form form values: its keys are data",
	"role-mining access-profiles create --body-file":            "sent verbatim, no guard reads it",
	"role-mining config update --body-file":                     "sent verbatim, no guard reads it",
	"role-mining custom-analysis evaluate --body-file":          "sent verbatim, no guard reads it",
	"role-mining custom-analysis trigger --body-file":           "sent verbatim, no guard reads it",
	"role-mining suggestions search --body-file":                "owned paging keys, covered by role-mining suggestions users",
	"role-mining suggestions state --body-file":                 "sent verbatim, no guard reads it",
	"service-principals trusts test-token --subject-token-file": "a JWT, not JSON",
	"settings ai-governance update --body-file":                 "sent verbatim with an explicit mask, no guard reads it",
	"settings contacts update --body-file":                      "sent verbatim with an explicit mask, no guard reads it",
	"settings cross-app-access update --body-file":              "sent verbatim with an explicit mask, no guard reads it",
	"settings request update --body-file":                       "sent verbatim with an explicit mask, no guard reads it",
	"settings session update --body-file":                       "sent verbatim with an explicit mask, no guard reads it",
	"settings sso update --body-file":                           "sent verbatim with an explicit mask, no guard reads it",
	"tasks update-request-data --data-file":                     "free-form form data: its keys are data",
	"workload-federation test-cel --claims-file":                "free-form JWT claims, sent as a string",
}

// TestEveryJSONFileFlagHasSpellingTwin: every file flag in the tree has a
// spelling twin or a reason in spellingExempt, so a new guard on a new file
// flag cannot skip the snake_case check unnoticed.
func TestEveryJSONFileFlagHasSpellingTwin(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range allSpellingTwins() {
		covered[tc.path+" --"+tc.flag] = true
	}
	var missing []string
	seen := map[string]bool{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			walk(sub)
		}
		if !c.Runnable() {
			return
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !strings.HasSuffix(f.Name, "-file") && f.Name != "file" {
				return
			}
			key := strings.TrimPrefix(c.CommandPath(), "c1i ") + " --" + f.Name
			seen[key] = true
			if !covered[key] && spellingExempt[key] == "" {
				missing = append(missing, key)
			}
			if covered[key] && spellingExempt[key] != "" {
				t.Errorf("%s has a twin and an exemption; drop the exemption", key)
			}
		})
	}
	walk(rootCmd)
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("%s: add a spelling twin to body_spelling_test.go, or an exemption with a reason", m)
	}
	for key := range spellingExempt {
		if !seen[key] {
			t.Errorf("spellingExempt names %s, which is not a file flag in the tree", key)
		}
	}
	for key := range covered {
		if !seen[key] {
			t.Errorf("a spelling twin names %s, which is not a file flag in the tree", key)
		}
	}
}

// TestOwnedBodyKeyMatchesEitherSpelling: one declared spelling covers both,
// so a declaration cannot leave the snake_case key unowned.
func TestOwnedBodyKeyMatchesEitherSpelling(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "x"}
		c.Flags().String("body-file", "", "Body")
		ownBodyKeys(c, "body-file", argBodyKeys(0, "<thing-id>", "thingId"), []ownedKey{{key: "appId", owner: "--app-id"}})
		if err := c.Flags().Parse([]string{"t1"}); err != nil {
			t.Fatal(err)
		}
		return c
	}
	for _, body := range []map[string]any{{"thing_id": "other"}, {"app_id": "a1"}} {
		if err := refuseCLIOwnedBodyKeys(newCmd(), "body-file", body); exitCode(err) != exitUsage {
			t.Errorf("%v: err %v, want a usage error", body, err)
		}
	}
	body := map[string]any{"thing_id": "t1"}
	if err := refuseCLIOwnedBodyKeys(newCmd(), "body-file", body); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sortedKeys(body), ","); got != "thingId" {
		t.Errorf("body keys %s, want the positional under the declared spelling only", got)
	}
}

// TestMisCasedKeysAreNotSpellings: only the camelCase and snake_case forms
// name a field (the server refuses any other casing as an unknown field), so
// c1i treats another casing as an unknown key.
func TestMisCasedKeysAreNotSpellings(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := []struct {
		name, path string
		args       []string
		file       string
		exit       int
		envelope   string         // the preview body's wrapper key, if any
		want       map[string]any // the exact keys of the sent object
	}{
		{"unknown bundle field", "access-profiles bundle-automation create", []string{"ap1", "--body-file", "@BODY"},
			`{"CreateTasks":true,"entitlements":{"entitlementRefs":[]}}`, exitUsage, "", nil},
		{"template policy not looked up", "access-reviews templates create", []string{"--body-file", "@BODY"},
			`{"displayName":"x","PolicyId":"pol9","ownerIds":["u1"]}`, 0, "",
			map[string]any{"displayName": "x", "PolicyId": "pol9", "ownerIds": []any{"u1"}}},
		{"owned id not rewritten", "findings routing-rules update", []string{"r1", "--body-file", "@BODY"},
			`{"Id":"other"}`, 0, "routingRule", map[string]any{"Id": "other", "id": "r1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := runSpellingTwin(t, spellingTwin{path: tc.path, args: tc.args}, tc.file)
			if r.exit != tc.exit {
				t.Fatalf("exit %d, want %d; requests %v", r.exit, tc.exit, r.requests)
			}
			for _, q := range r.requests {
				if strings.Contains(q, "pol9") {
					t.Errorf("looked up a mis-cased key: %s", q)
				}
			}
			if tc.want == nil {
				return
			}
			var preview struct {
				Requests []struct {
					Body map[string]any `json:"body"`
				} `json:"requests"`
			}
			if err := json.Unmarshal([]byte(r.raw), &preview); err != nil || len(preview.Requests) != 1 {
				t.Fatalf("preview %q: %v", r.raw, err)
			}
			got := preview.Requests[0].Body
			if tc.envelope != "" {
				got, _ = got[tc.envelope].(map[string]any)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("sent %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestFieldSpellings(t *testing.T) {
	for _, tc := range []struct{ in, camel, snake string }{
		{"pageSize", "pageSize", "page_size"},
		{"page_size", "pageSize", "page_size"},
		{"id", "id", "id"},
		{"awsSigv4", "awsSigv4", "aws_sigv4"},
		{"scopedRoleIds", "scopedRoleIds", "scoped_role_ids"},
		{"mcp_server_catalog_id", "mcpServerCatalogId", "mcp_server_catalog_id"},
	} {
		if c, s := fieldSpellings(tc.in); c != tc.camel || s != tc.snake {
			t.Errorf("fieldSpellings(%q) = %q, %q; want %q, %q", tc.in, c, s, tc.camel, tc.snake)
		}
	}
	obj := map[string]any{"PageSize": 1, "pagesize": 2, "PAGE_SIZE": 3, "Page_Size": 4}
	if v, key, err := bodyField(obj, "pageSize"); v != nil || key != "" || err != nil {
		t.Errorf("bodyField matched a mis-cased key: %v %q %v", v, key, err)
	}
	if bodySetsField(obj, "pageSize") {
		t.Error("bodySetsField matched a mis-cased key")
	}
	if err := setBodyField(obj, "pageSize", 5); err != nil || obj["pageSize"] != 5 {
		t.Errorf("setBodyField: %v, %v", err, obj)
	}
	both := map[string]any{"pageSize": 1, "page_size": 2}
	if _, _, err := bodyField(both, "page_size"); exitCode(err) != exitUsage {
		t.Errorf("both spellings: %v, want a usage error", err)
	}
}
