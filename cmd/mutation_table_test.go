package cmd

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// The §6.1 table: every mutating command, the response it is shown, the
// document it prints and the id paths that must resolve with jq -e.

type mutationRow struct {
	cmd    string   // command path after "c1i"
	args   []string // after the path; "@<json>" becomes a temp file holding <json>
	body   string   // the write's response
	want   string   // compact document; "" means body unchanged
	ids    []string // §6.1 id paths
	method string   // the write, when pinned
	path   string
	reads  int    // GETs sent before the write
	stderr string // text-mode stderr
}

const (
	tblCatalog    = `{"requestCatalogView":{"requestCatalog":{"id":"cat-1","displayName":"zz <x>"},"accessEntitlementsPath":"a","createdByUserPath":"c","memberCount":"2"},"expanded":[]}`
	tblCatalogDoc = `{"id":"cat-1","displayName":"zz <x>","expanded":[],"accessEntitlementsPath":"a","createdByUserPath":"c","memberCount":"2"}`
	tblBundle     = `{"requestCatalogId":"cat-1","enabled":true}`
	tblReview     = `{"accessReview":{"accessReview":{"id":"ar1"},"createdByUserPath":"c","objectPermissions":{},"policyPath":""},"expanded":[{"id":"p1"}]}`
	tblReviewDoc  = `{"id":"ar1","expanded":[{"id":"p1"}],"createdByUserPath":"c","objectPermissions":{},"policyPath":""}`
	tblApp        = `{"app":{"id":"app-1","displayName":"zz <x>","appOwners":[]}}`
	tblAppDoc     = `{"id":"app-1","displayName":"zz <x>","appOwners":[]}`
	tblEnt        = `{"appEntitlementView":{"appEntitlement":{"id":"e-1","appResourceTypeId":"rt-1","appResourceId":"r-1"},"appPath":"p","appResourcePath":"r","appResourceTypePath":"t","objectPermissions":{}},"expanded":[]}`
	tblEntDoc     = `{"id":"e-1","appResourceTypeId":"rt-1","appResourceId":"r-1","expanded":[],"appPath":"p","appResourcePath":"r","appResourceTypePath":"t","objectPermissions":{}}`
	tblProxy      = `{"appProxyEntitlementView":{"appProxyEntitlement":{"srcAppId":"sa","srcAppEntitlementId":"se","dstAppId":"da","dstAppEntitlementId":"de"},"dstAppEntitlementPath":"a","dstAppPath":"b","srcAppEntitlementPath":"c","srcAppPath":"d"},"expanded":[]}`
	tblProxyDoc   = `{"srcAppId":"sa","srcAppEntitlementId":"se","dstAppId":"da","dstAppEntitlementId":"de","expanded":[],"dstAppEntitlementPath":"a","dstAppPath":"b","srcAppEntitlementPath":"c","srcAppPath":"d"}`
	tblClassifier = `{"classifier":{"id":"cl1","displayName":"zz"}}`
	tblClassDoc   = `{"id":"cl1","displayName":"zz"}`
	tblGate       = `{"toolGate":{"id":"g1","displayName":"zz"}}`
	tblGateDoc    = `{"id":"g1","displayName":"zz"}`
	tblToolset    = `{"profile":{"id":"ts1","displayName":"zz"}}`
	tblToolsetDoc = `{"id":"ts1","displayName":"zz"}`
	tblServer     = `{"mcpServer":{"connectorId":"cn1","displayName":"zz"}}`
	tblServerDoc  = `{"connectorId":"cn1","displayName":"zz"}`
	tblPolicy     = `{"policy":{"id":"pol1","displayName":"zz"}}`
	tblPolicyDoc  = `{"id":"pol1","displayName":"zz"}`
	tblSP         = `{"servicePrincipal":{"id":"sp-1","displayName":"zz"}}`
	tblSPDoc      = `{"id":"sp-1","displayName":"zz"}`
	tblWebhook    = `{"webhook":{"id":"wh-1","displayName":"zz"}}`
	tblWebhookDoc = `{"id":"wh-1","displayName":"zz"}`
	tblStep       = "Resolved policy step: step-9\n"
)

// tblTask is a TaskView envelope with the given action-id sibling ("" for
// none), and tblTaskDoc what it unwraps to.
func tblTask(action string) string {
	a := ""
	if action != "" {
		a = `,"` + action + `":"act-1"`
	}
	return `{"taskView":{"task":{"id":"task-1","state":"TASK_STATE_OPEN"},"appPath":"p"},"objectPermissions":{}` + a + `,"expanded":[]}`
}

func tblTaskDoc(action string) string {
	a := ""
	if action != "" {
		a = `,"` + action + `":"act-1"`
	}
	return `{"id":"task-1","state":"TASK_STATE_OPEN","expanded":[],"objectPermissions":{}` + a + `,"appPath":"p"}`
}

func tblTaskIDs(action string) []string {
	if action == "" {
		return []string{".id"}
	}
	return []string{".id", "." + action}
}

var (
	idPath   = []string{".id"}
	ent      = []string{"--app-id", "app-1", "--entitlement-id", "ent-1"}
	mcpScope = []string{"--app-id=a1", "--connector-id=cn1"}
)

func cat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

var mutationTable = []mutationRow{
	// access-profiles
	{cmd: "access-profiles create", args: []string{"--display-name", "zz"}, body: tblCatalog, want: tblCatalogDoc, ids: idPath},
	{cmd: "access-profiles update", args: []string{"cat-1", "--description", "d"}, body: tblCatalog, want: tblCatalogDoc, ids: idPath},
	{cmd: "access-profiles delete", args: []string{"cat-1"}, body: `{}`},
	{cmd: "access-profiles bundle-automation create", args: []string{"cat-1", "--body-file", `@{"enabled":true}`}, body: tblBundle, ids: []string{".requestCatalogId"}},
	{cmd: "access-profiles bundle-automation set", args: []string{"cat-1", "--body-file", `@{"enabled":true}`}, body: tblBundle, ids: []string{".requestCatalogId"}},
	{cmd: "access-profiles bundle-automation delete", args: []string{"cat-1"}, body: `{}`},
	{cmd: "access-profiles bundle-automation resume", args: []string{"cat-1"}, body: `{}`},
	{cmd: "access-profiles bundle-automation run", args: []string{"cat-1"}, body: `{}`},
	{cmd: "access-profiles requestable-entitlements add", args: cat([]string{"cat-1"}, ent), body: `{}`},
	{cmd: "access-profiles requestable-entitlements remove", args: cat([]string{"cat-1"}, ent), body: `{}`},
	{cmd: "access-profiles requestable-entitlements set", args: []string{"cat-1", "--refs-file", `@[{"appId":"app-1","id":"ent-1"}]`}, body: `{}`},
	{cmd: "access-profiles visibility-entitlements add", args: cat([]string{"cat-1"}, ent), body: `{}`},
	{cmd: "access-profiles visibility-entitlements remove", args: cat([]string{"cat-1"}, ent), body: `{}`},
	// access-reviews
	{cmd: "access-reviews create", args: []string{"--body-file", `@{"displayName":"zz","ownerIds":["u1"]}`}, body: tblReview, want: tblReviewDoc, ids: idPath},
	{cmd: "access-reviews update", args: []string{"ar1", "--body-file", `@{}`, "--update-mask", "description"}, body: tblReview, want: tblReviewDoc, ids: idPath},
	{cmd: "access-reviews reports generate", args: []string{"ar1"}, body: `{}`},
	// accounts
	{cmd: "accounts set-owner", args: []string{"au-1", "--app-id", "app-1", "--user-id", "u1"},
		body: `{"appUserView":{"appUser":{"id":"au-1","identityUserId":"u1"},"appPath":"p","identityUserPath":"i","lastUsagePath":"l"},"expanded":[]}`,
		want: `{"id":"au-1","identityUserId":"u1","expanded":[],"appPath":"p","identityUserPath":"i","lastUsagePath":"l"}`,
		ids:  []string{".id", ".identityUserId"}, method: "POST", path: "/api/v1/apps/app-1/app_users/au-1"},
	// apps
	{cmd: "apps create", args: []string{"--display-name", "zz"}, body: tblApp, want: tblAppDoc, ids: idPath, method: "POST", path: "/api/v1/apps"},
	{cmd: "apps update", args: []string{"app-1", "--description", "d"}, body: tblApp, want: tblAppDoc, ids: idPath},
	{cmd: "apps delete", args: []string{"app-1"}, body: `{}`, method: "DELETE", path: "/api/v1/apps/app-1"},
	{cmd: "apps add-owner", args: []string{"app-1", "--user-id", "u1"}, body: `{}`},
	{cmd: "apps remove-owner", args: []string{"app-1", "--user-id", "u1"}, body: `{}`},
	{cmd: "apps set-owners", args: []string{"app-1", "--user-id", "u1"}, body: `{}`, method: "PUT", path: "/api/v1/apps/app-1/owners", reads: 1},
	// automations
	{cmd: "automations update", args: []string{"a-1", "--enabled=false"},
		body: `{"automation":{"id":"a-1","enabled":false},"webhookCapabilityUrl":"","webhookHmacSecret":""}`,
		want: `{"id":"a-1","enabled":false,"webhookCapabilityUrl":"","webhookHmacSecret":""}`, ids: idPath, method: "POST", path: "/api/v1/automations/a-1"},
	{cmd: "automations delete", args: []string{"a-1"}, body: `{}`, method: "DELETE", path: "/api/v1/automations/a-1"},
	// entitlements
	{cmd: "entitlements create", args: []string{"--app-id", "app-1", "--display-name", "e", "--resource-type-id", "rt-1", "--resource-id", "r-1"},
		body: tblEnt, want: tblEntDoc, ids: []string{".id", ".appResourceTypeId", ".appResourceId"}, method: "POST", path: "/api/v1/apps/app-1/entitlements"},
	{cmd: "entitlements update", args: []string{"e-1", "--app-id", "app-1", "--description", "d"}, body: tblEnt, want: tblEntDoc, ids: idPath, method: "POST", path: "/api/v1/apps/app-1/entitlements/e-1"},
	{cmd: "entitlements delete", args: []string{"e-1", "--app-id", "app-1"}, body: `{}`, method: "DELETE", path: "/api/v1/apps/app-1/entitlements/e-1"},
	{cmd: "entitlements add-owner", args: []string{"e-1", "--app-id", "app-1", "--user-id", "u1"}, body: `{}`, method: "POST", path: "/api/v1/apps/app-1/entitlements/e-1/owners"},
	{cmd: "entitlements remove-owner", args: []string{"e-1", "--app-id", "app-1", "--user-id", "u1"}, body: `{}`, method: "DELETE", path: "/api/v1/apps/app-1/entitlements/e-1/owners/u1"},
	{cmd: "entitlements set-owners", args: []string{"e-1", "--app-id", "app-1", "--user-id", "u1"}, body: `{}`, method: "PUT", path: "/api/v1/apps/app-1/entitlements/e-1/owners", reads: 1},
	{cmd: "entitlements proxy-bindings create", args: []string{"--source-app-id", "sa", "--source-entitlement-id", "se", "--destination-app-id", "da", "--destination-entitlement-id", "de"},
		body: tblProxy, want: tblProxyDoc, ids: []string{".srcAppId", ".srcAppEntitlementId", ".dstAppId", ".dstAppEntitlementId"}, method: "POST", path: "/api/v1/apps/sa/se/bindings/da/de"},
	{cmd: "entitlements proxy-bindings delete", args: []string{"--source-app-id", "sa", "--source-entitlement-id", "se", "--destination-app-id", "da", "--destination-entitlement-id", "de"},
		body: `{}`, method: "DELETE", path: "/api/v1/apps/sa/se/bindings/da/de"},
	// findings
	{cmd: "findings create", args: []string{"--body-file", "@{}"}, body: `{"finding":{"id":"f1","state":"OPEN"}}`, want: `{"id":"f1","state":"OPEN"}`, ids: idPath},
	{cmd: "findings state", args: []string{"f1", "--body-file", "@{}"}, body: `{"finding":{"id":"f1","state":"CLOSED"}}`, want: `{"id":"f1","state":"CLOSED"}`, ids: idPath},
	{cmd: "findings assignee", args: []string{"f1", "--body-file", "@{}"}, body: `{"finding":{"id":"f1","assigneeUserId":"u1"}}`, want: `{"id":"f1","assigneeUserId":"u1"}`, ids: idPath},
	{cmd: "findings create-task", args: []string{"f1"}, body: `{"finding":{"id":"f1","taskId":""},"taskId":"t1"}`, ids: []string{".taskId", ".finding.id"}},
	{cmd: "findings bulk-state", args: []string{"--body-file", "@{}"}, body: `{"bulkActionId":"b1"}`, ids: []string{".bulkActionId"}},
	{cmd: "findings bulk-create-tasks", args: []string{"--body-file", "@{}"}, body: `{"bulkActionId":"b1"}`, ids: []string{".bulkActionId"}},
	{cmd: "findings settings update", args: []string{"--body-file", "@{}"}, body: `{"list":[{"id":"s1"}]}`, ids: []string{".list[]"}},
	{cmd: "findings routing-rules create", args: []string{"--body-file", "@{}"}, body: `{"routingRule":{"id":"r1"}}`, want: `{"id":"r1"}`, ids: idPath},
	{cmd: "findings routing-rules update", args: []string{"r1", "--body-file", "@{}"}, body: `{"routingRule":{"id":"r1","displayName":"n"}}`, want: `{"id":"r1","displayName":"n"}`, ids: idPath},
	{cmd: "findings routing-rules delete", args: []string{"r1"}, body: `{}`},
	{cmd: "findings transformation-rules create", args: []string{"--body-file", "@{}"}, body: `{"transformationRule":{"id":"x1"}}`, want: `{"id":"x1"}`, ids: idPath},
	{cmd: "findings transformation-rules update", args: []string{"x1", "--body-file", "@{}"}, body: `{"transformationRule":{"id":"x1","displayName":"n"}}`, want: `{"id":"x1","displayName":"n"}`, ids: idPath},
	{cmd: "findings transformation-rules delete", args: []string{"x1"}, body: `{}`},
	// mcp
	{cmd: "mcp bindings create", args: cat(mcpScope, []string{"--toolset-id=ts1", "--tool-id=t1"}), body: `{"bindings":[{"mcpToolId":"t1"}]}`, ids: []string{".bindings[].mcpToolId"}},
	{cmd: "mcp bindings delete", args: cat(mcpScope, []string{"--toolset-id=ts1", "--tool-id=t1"}), body: `{}`},
	{cmd: "mcp classifiers create", args: []string{"--body-file", `@{"displayName":"zz"}`}, body: tblClassifier, want: tblClassDoc, ids: idPath},
	{cmd: "mcp classifiers update", args: []string{"cl1", "--body-file", `@{"displayName":"zz"}`, "--update-mask=description"}, body: tblClassifier, want: tblClassDoc, ids: idPath},
	{cmd: "mcp classifiers delete", args: []string{"cl1"}, body: `{}`},
	{cmd: "mcp classifiers templates add-rule", args: []string{"cl1", "tp1", "--body-file", "@{}"}, body: tblClassifier, want: tblClassDoc, ids: idPath},
	{cmd: "mcp classifiers templates instantiate", args: []string{"tp1"}, body: tblClassifier, want: tblClassDoc, ids: idPath},
	{cmd: "mcp classifiers bindings create", args: []string{"--body-file", `@{"surface":"S","classifierId":"cl1"}`}, body: `{"binding":{"id":"b1","surface":"S"}}`, want: `{"id":"b1","surface":"S"}`, ids: idPath},
	{cmd: "mcp classifiers bindings delete", args: []string{"b1"}, body: `{}`},
	{cmd: "mcp classifiers policy update", args: []string{"--body-file", `@{"defaultOutcome":"X"}`, "--update-mask=defaultOutcome"}, body: `{"policy":{"defaultOutcome":"X"}}`, want: `{"defaultOutcome":"X"}`},
	{cmd: "mcp classifiers tool-gates create", args: []string{"--body-file", `@{"displayName":"zz"}`}, body: tblGate, want: tblGateDoc, ids: idPath},
	{cmd: "mcp classifiers tool-gates update", args: []string{"g1", "--body-file", `@{"displayName":"zz"}`, "--update-mask=enabled"}, body: tblGate, want: tblGateDoc, ids: idPath},
	{cmd: "mcp classifiers tool-gates delete", args: []string{"g1"}, body: `{}`},
	{cmd: "mcp servers register", args: []string{"--app-id=a1", "--type=external", "--display-name=zz", "--server-url=https://mcp.example.invalid"},
		body: `{"mcpServer":{"connectorId":"cn1","displayName":"zz"},"accessProfilesAttached":2}`, want: `{"connectorId":"cn1","displayName":"zz","accessProfilesAttached":2}`, ids: []string{".connectorId"}},
	{cmd: "mcp servers update", args: []string{"cn1", "--app-id=a1", "--display-name=zz"}, body: tblServer, want: tblServerDoc, ids: []string{".connectorId"}},
	{cmd: "mcp servers update-credentials", args: []string{"cn1", "--app-id=a1", "--type=external", "--server-url=https://mcp.example.invalid"},
		body: `{"mcpServer":{"connectorId":"cn1"}}`, want: `{"connectorId":"cn1"}`, ids: []string{".connectorId"}},
	{cmd: "mcp servers delete", args: []string{"cn1", "--app-id=a1"}, body: `{}`},
	{cmd: "mcp servers resync-tools", args: []string{"cn1", "--app-id=a1"}, body: `{}`},
	{cmd: "mcp tools approve", args: cat([]string{"t1"}, mcpScope), body: `{"tool":{"id":"t1","state":"MCP_TOOL_STATE_APPROVED"}}`, want: `{"id":"t1","state":"MCP_TOOL_STATE_APPROVED"}`, ids: idPath},
	{cmd: "mcp tools delete", args: cat([]string{"t1"}, mcpScope), body: `{}`},
	{cmd: "mcp toolsets create", args: cat(mcpScope, []string{"--display-name=zz"}), body: tblToolset, want: tblToolsetDoc, ids: idPath},
	{cmd: "mcp toolsets update", args: cat([]string{"ts1"}, mcpScope, []string{"--display-name=zz"}), body: tblToolset, want: tblToolsetDoc, ids: idPath},
	{cmd: "mcp toolsets delete", args: cat([]string{"ts1"}, mcpScope), body: `{}`},
	// policies
	{cmd: "policies create", args: []string{"--display-name", "zz", "--policy-type", "grant", "--steps-file", `@[{"approval":{"users":{"userIds":["u1"]}}},{"accept":{}}]`}, body: tblPolicy, want: tblPolicyDoc, ids: idPath},
	{cmd: "policies update", args: []string{"pol1", "--display-name", "zz"}, body: tblPolicy, want: tblPolicyDoc, ids: idPath},
	{cmd: "policies delete", args: []string{"pol1"}, body: `{}`},
	// requests
	{cmd: "requests create grant", args: cat([]string{"--user-id", "u1"}, ent), body: tblTask(""), want: tblTaskDoc(""), ids: idPath},
	{cmd: "requests create revoke", args: cat([]string{"--user-id", "u1"}, ent), body: tblTask(""), want: tblTaskDoc(""), ids: idPath},
	// role-mining
	{cmd: "role-mining trigger", body: `{"runId":"run1"}`, ids: []string{".runId"}},
	{cmd: "role-mining suggestions state", args: []string{"s1", "--body-file", "@{}"}, body: `{"suggestion":{"id":"s1","suggestionState":"DISMISSED"}}`, want: `{"id":"s1","suggestionState":"DISMISSED"}`, ids: idPath},
	{cmd: "role-mining config update", args: []string{"--body-file", "@{}"}, body: `{"config":{"minCohortSize":10}}`, want: `{"minCohortSize":10}`},
	{cmd: "role-mining custom-analysis trigger", args: []string{"--body-file", "@{}"}, body: `{"id":"a1"}`, ids: idPath},
	{cmd: "role-mining custom-analysis evaluate", args: []string{"a1", "--body-file", "@{}"}, body: `{"coverage":3}`},
	{cmd: "role-mining access-profiles create", args: []string{"--body-file", "@{}"}, body: `{"accessProfileId":"ap1","celExpression":"true"}`, ids: []string{".accessProfileId"}},
	// service-principals
	{cmd: "service-principals create", args: []string{"--display-name", "zz"}, body: tblSP, want: tblSPDoc, ids: idPath, method: "POST", path: "/api/v1/service_principals"},
	{cmd: "service-principals update", args: []string{"sp-1", "--display-name", "zz"}, body: tblSP, want: tblSPDoc, ids: idPath, method: "PATCH", path: "/api/v1/service_principals/sp-1"},
	{cmd: "service-principals delete", args: []string{"sp-1"}, body: `{}`, method: "DELETE", path: "/api/v1/service_principals/sp-1"},
	{cmd: "service-principals credentials create", args: []string{"sp-1", "--display-name", "ci"},
		body: `{"credential":{"id":"c-1","clientId":"cid"},"clientSecret":"s3"}`, want: `{"id":"c-1","clientId":"cid","clientSecret":"s3"}`,
		ids: []string{".id", ".clientId", ".clientSecret"}, method: "POST", path: "/api/v1/service_principals/sp-1/credentials"},
	{cmd: "service-principals credentials update", args: []string{"c-1", "--service-principal-id", "sp-1", "--display-name", "n"},
		body: `{"credential":{"id":"c-1","displayName":"n"}}`, want: `{"id":"c-1","displayName":"n"}`, ids: idPath, method: "PATCH", path: "/api/v1/service_principals/sp-1/credentials/c-1"},
	{cmd: "service-principals credentials revoke", args: []string{"c-1", "--service-principal-id", "sp-1"}, body: `{}`, method: "DELETE", path: "/api/v1/service_principals/sp-1/credentials/c-1"},
	{cmd: "service-principals bindings add", args: []string{"--service-principal-id", "sp-1", "--function-id", "fn-1"}, body: `{}`, method: "POST", path: spBindingsPath},
	{cmd: "service-principals bindings delete", args: []string{"--service-principal-id", "sp-1", "--function-id", "fn-1"}, body: `{}`, method: "POST", path: spBindingsDeletePath},
	// tasks
	{cmd: "tasks approve", args: []string{"task-1", "--policy-step-id", "s"}, body: tblTask("ticketActionId"), want: tblTaskDoc("ticketActionId"), ids: tblTaskIDs("ticketActionId")},
	{cmd: "tasks deny", args: []string{"task-1"}, body: tblTask("ticketActionId"), want: tblTaskDoc("ticketActionId"), ids: tblTaskIDs("ticketActionId"), reads: 1, stderr: tblStep},
	{cmd: "tasks close", args: []string{"task-1"}, body: tblTask("taskActionId"), want: tblTaskDoc("taskActionId"), ids: tblTaskIDs("taskActionId")},
	{cmd: "tasks comment", args: []string{"task-1", "--comment", "hi"}, body: tblTask(""), want: tblTaskDoc(""), ids: tblTaskIDs("")},
	{cmd: "tasks process", args: []string{"task-1"}, body: tblTask(""), want: tblTaskDoc(""), ids: tblTaskIDs("")},
	{cmd: "tasks reassign", args: []string{"task-1", "--to-user-id", "u1", "--policy-step-id", "s"}, body: tblTask("ticketActionId"), want: tblTaskDoc("ticketActionId"), ids: tblTaskIDs("ticketActionId")},
	{cmd: "tasks reset", args: []string{"task-1"}, body: tblTask("ticketActionId"), want: tblTaskDoc("ticketActionId"), ids: tblTaskIDs("ticketActionId")},
	{cmd: "tasks restart", args: []string{"task-1"}, body: tblTask("ticketActionId"), want: tblTaskDoc("ticketActionId"), ids: tblTaskIDs("ticketActionId"), reads: 1, stderr: tblStep},
	{cmd: "tasks skip-step", args: []string{"task-1", "--policy-step-id", "s"}, body: tblTask("ticketActionId"), want: tblTaskDoc("ticketActionId"), ids: tblTaskIDs("ticketActionId")},
	{cmd: "tasks update-grant-duration", args: []string{"task-1", "--duration", "1h"}, body: tblTask("ticketActionId"), want: tblTaskDoc("ticketActionId"), ids: tblTaskIDs("ticketActionId")},
	// webhooks
	{cmd: "webhooks create", args: []string{"--display-name", "zz", "--destination-url", "https://hooks.example.com/x"}, body: tblWebhook, want: tblWebhookDoc, ids: idPath, method: "POST", path: "/api/v1/webhooks"},
	{cmd: "webhooks update", args: []string{"wh-1", "--description", "d"}, body: tblWebhook, want: tblWebhookDoc, ids: idPath, method: "POST", path: "/api/v1/webhooks/wh-1"},
	{cmd: "webhooks test", args: []string{"wh-1"}, body: `{"webhook":{"id":"inst-1","webhookId":"wh-1"}}`, want: `{"id":"inst-1","webhookId":"wh-1"}`, ids: []string{".id", ".webhookId"}, method: "POST", path: "/api/v1/webhooks/wh-1/test"},
	{cmd: "webhooks delete", args: []string{"wh-1"}, body: `{}`, method: "DELETE", path: "/api/v1/webhooks/wh-1"},
}

// tblGet answers every pre-send read: the policy-step, user, catalog-id and
// self lookups.
const tblGet = `{"taskView":{"task":{"id":"task-1","policy":{"current":{"id":"step-9"}}}},"user":{"id":"u1"},"userId":"self-1"}`

func tableRoute(body string) func(*http.Request, int) mutReply {
	return func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{body: tblGet}
		}
		return mutReply{body: body}
	}
}

// argv expands the "@<json>" placeholders into temp files.
func (row mutationRow) argv(t *testing.T, extra ...string) []string {
	t.Helper()
	out := strings.Fields(row.cmd)
	for _, a := range row.args {
		if strings.HasPrefix(a, "@") {
			a = writeTempFile(t, a[1:])
		}
		out = append(out, a)
	}
	return append(out, extra...)
}

func (row mutationRow) batch() bool {
	_, ok := batchCommands["c1i "+row.cmd]
	return ok
}

// wantStdout is the exact bytes: an indented document, or a compact line for
// a batch.
func (row mutationRow) wantStdout(t *testing.T) string {
	t.Helper()
	want := row.want
	if want == "" {
		want = row.body
	}
	if row.batch() {
		return want + "\n"
	}
	return indentJSON(t, want)
}

// jqResolves reports whether `jq -e path` succeeds on doc with a non-empty
// value: the last output exists and is not null, false or "".
func jqResolves(doc any, path string) bool {
	vals := []any{doc}
	for _, seg := range strings.Split(strings.TrimPrefix(path, "."), ".") {
		key, iter := strings.CutSuffix(seg, "[]")
		var next []any
		for _, v := range vals {
			if key != "" {
				m, ok := v.(map[string]any)
				if !ok {
					return false
				}
				v = m[key]
			}
			if !iter {
				next = append(next, v)
				continue
			}
			arr, ok := v.([]any)
			if !ok {
				return false
			}
			next = append(next, arr...)
		}
		vals = next
	}
	if len(vals) == 0 {
		return false
	}
	last := vals[len(vals)-1]
	return last != nil && last != false && last != ""
}

func TestJQResolves(t *testing.T) {
	var doc any
	if err := json.Unmarshal([]byte(`{"id":"x","e":"","n":null,"f":false,"o":{"id":"y"},"l":[{"id":"a"},{"id":"b"}],"z":[]}`), &doc); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		".id": true, ".o.id": true, ".l[].id": true, ".l[]": true,
		".e": false, ".n": false, ".f": false, ".missing": false, ".z[]": false, ".id.x": false, ".o[]": false,
	} {
		if got := jqResolves(doc, path); got != want {
			t.Errorf("jqResolves(%s) = %v, want %v", path, got, want)
		}
	}
}

// TestMutationTableCoversEveryMutation: the table has exactly one row per
// command Rule B classifies as mutating.
func TestMutationTableCoversEveryMutation(t *testing.T) {
	ix := loadSrcIndex(t, nil)
	want := map[string]bool{}
	for _, l := range leafCommands(t, ix) {
		if len(ix.closureOf(l.root).helpers) > 0 {
			want[strings.TrimPrefix(l.path, "c1i ")] = true
		}
	}
	have := map[string]bool{}
	for _, row := range mutationTable {
		if have[row.cmd] {
			t.Errorf("%s has two rows", row.cmd)
		}
		have[row.cmd] = true
		if !want[row.cmd] {
			t.Errorf("%s has a row but is not a mutating command", row.cmd)
		}
	}
	var missing []string
	for c := range want {
		if !have[c] {
			missing = append(missing, c)
		}
	}
	sort.Strings(missing)
	for _, c := range missing {
		t.Errorf("%s has no mutationTable row", c)
	}
}

// TestMutationTable is the §6.1 table test: each command prints its exact
// document, its id paths resolve, and its preview names the request it sent.
func TestMutationTable(t *testing.T) {
	for _, row := range mutationTable {
		t.Run(row.cmd, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, tableRoute(row.body))
			r := runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			if want := row.wantStdout(t); r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
			if r.stderr != row.stderr {
				t.Errorf("stderr = %q, want %q", r.stderr, row.stderr)
			}
			var doc any
			if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
				t.Fatalf("stdout is not one JSON value: %v", err)
			}
			for _, p := range row.ids {
				if !jqResolves(doc, p) {
					t.Errorf("jq -e %s fails on %s", p, r.stdout)
				}
			}
			sent := s.requests()
			if len(sent) != row.reads+1 {
				t.Fatalf("requests = %+v, want %d reads and one write", sent, row.reads)
			}
			write := sent[len(sent)-1]
			if row.method != "" && (write.method != row.method || write.rawPath != row.path) {
				t.Errorf("write = %s %s, want %s %s", write.method, write.rawPath, row.method, row.path)
			}

			// The preview names that same request and body. A row with no
			// pre-send read must not build a client at all.
			withDryRun(t)
			p := newMutServer(t, tableRoute(row.body))
			psrv := p.srv
			if row.reads == 0 {
				psrv = nil
			}
			r = runMutationCLI(t, psrv, 0, nil, row.argv(t)...)
			if r.err != nil {
				t.Fatalf("preview: exit %d: %v", r.exit, r.err)
			}
			var preview map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &preview); err != nil || len(preview) != 2 || preview["dryRun"] != true {
				t.Fatalf("preview = %q, want exactly dryRun and requests", r.stdout)
			}
			reqs := previewDoc(t, r.stdout)
			last := reqs[len(reqs)-1]
			if last["method"] != write.method || last["url"] != mutTestURL+write.rawPath {
				t.Errorf("preview request = %v %v, want %s %s", last["method"], last["url"], write.method, mutTestURL+write.rawPath)
			}
			if got, want := compactJSON(t, last["body"]), compactRaw(t, write.body); got != want {
				t.Errorf("preview body = %s, sent %s", got, want)
			}
			if want := row.stderr + "dry run: nothing was sent\n"; r.stderr != want {
				t.Errorf("preview stderr = %q, want %q", r.stderr, want)
			}
			for _, q := range p.requests() {
				if q.method != http.MethodGet {
					t.Errorf("preview sent %s %s", q.method, q.rawPath)
				}
			}
		})
	}
}

func compactJSON(t *testing.T, v any) string {
	t.Helper()
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func compactRaw(t *testing.T, s string) string {
	t.Helper()
	if s == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("sent body %q is not JSON: %v", s, err)
	}
	return compactJSON(t, v)
}

// An explicit --fields refuses before anything is sent, with applied:false.
func TestMutationTableRefusesFields(t *testing.T) {
	for _, row := range mutationTable {
		t.Run(row.cmd, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, tableRoute(row.body))
			r := runMutationCLI(t, s.srv, 0, nil, row.argv(t, "--fields", "id", "--error-format", "json")...)
			if r.exit != exitUsage || r.stdout != "" || len(s.requests()) != 0 {
				t.Errorf("exit %d stdout %q, %d requests; want exit 2, nothing printed or sent", r.exit, r.stdout, len(s.requests()))
			}
			if got := r.errorObject(t)["applied"]; got != false {
				t.Errorf("applied = %v, want false", got)
			}
		})
	}
}

// A write that 404s exits 4 with nothing on stdout and applied:false.
func TestMutationTableNotFound(t *testing.T) {
	for _, row := range mutationTable {
		t.Run(row.cmd, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, func(r *http.Request, n int) mutReply {
				if r.Method == http.MethodGet {
					return mutReply{body: tblGet}
				}
				return mutReply{status: 404, body: `{"code":5}`}
			})
			r := runMutationCLI(t, s.srv, 0, nil, row.argv(t, "--error-format", "json")...)
			if r.exit != exitNotFound || r.stdout != "" {
				t.Errorf("exit %d stdout %q, want 4 and nothing on stdout", r.exit, r.stdout)
			}
			if got := r.errorObject(t)["applied"]; got != false {
				t.Errorf("applied = %v, want false", got)
			}
		})
	}
}

// R9: an empty 2xx body prints {}.
func TestMutationEmptyBodyPrintsEmptyObject(t *testing.T) {
	for _, args := range [][]string{
		{"apps", "delete", "app-1"},
		{"access-profiles", "delete", "cat-1"},
		{"webhooks", "delete", "wh-1"},
		{"mcp", "servers", "delete", "cn1", "--app-id=a1"},
		{"service-principals", "bindings", "add", "--service-principal-id", "sp-1", "--function-id", "fn-1"},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, ``))
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.err != nil || r.stdout != "{}\n" {
				t.Errorf("exit %d stdout %q, want {}", r.exit, r.stdout)
			}
		})
	}
}
