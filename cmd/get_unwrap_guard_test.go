package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Every typed `get` must expose the resource's identifier at the top level of
// its output, as list rows do. getUnwrapCases is the table; each body is the
// envelope that endpoint really answers with.

type getUnwrapCase struct {
	name string
	cmd  *cobra.Command
	args []string
	// flags are the parent-scope ids the command requires (--app-id, ...).
	flags map[string]string
	// idKey is the unique resource identity field, normally "id". A singleton
	// configuration has no id, so its stable field can opt out of string checks.
	idKey             string
	allowNonStringKey bool
	body              string
	// wantKeys is the EXACT top-level key set the output must have: the
	// payload's own keys plus every envelope key beside it. Asserting the whole
	// set, not just that the id survived, is what catches a hoist that also
	// leaks or loses a key.
	wantKeys []string
	// payloadPath is where the resource sits inside body. Declared, not
	// derived, so the value assertions don't re-implement the search they check.
	payloadPath []string
}

func getUnwrapCases() []getUnwrapCase {
	return []getUnwrapCase{
		{
			name:        "apps get",
			cmd:         appsGetCmd,
			args:        []string{"app-1"},
			idKey:       "id",
			body:        `{"app":{"id":"app-1","displayName":"Okta"}}`,
			payloadPath: []string{"app"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "apps managed-state get",
			cmd:         appsManagedStateGetCmd,
			args:        []string{"res-1"},
			flags:       map[string]string{"app-id": "app-1", "resource-type-id": "rt-1"},
			idKey:       "resourceId",
			body:        `{"appManagementState":{"appManagementStateBinding":{"resourceId":"res-1","displayName":"Demo","state":{"unmanaged":{}}},"appPath":"","resourcePath":""},"expanded":[]}`,
			payloadPath: []string{"appManagementState", "appManagementStateBinding"},
			wantKeys:    []string{"resourceId", "displayName", "state", "appPath", "resourcePath", "expanded"},
		},
		{
			name:        "apps resource-types get",
			cmd:         appsResourceTypesGetCmd,
			args:        []string{"rt-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"appResourceTypeView":{"appResourceType":{"id":"rt-1","appId":"app-1","displayName":"group","traitIds":["tr-1"]},"appPath":""},"expanded":[]}`,
			payloadPath: []string{"appResourceTypeView", "appResourceType"},
			wantKeys:    []string{"id", "appId", "displayName", "traitIds", "appPath", "expanded"},
		},
		{
			name:        "apps routing-rules get",
			cmd:         appsRoutingRulesGetCmd,
			args:        []string{"rr-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"routingRule":{"id":"rr-1","appId":"app-1","displayName":"zz","priority":1,"enabled":false,"settings":{"requestPolicyId":""}}}`,
			payloadPath: []string{"routingRule"},
			wantKeys:    []string{"id", "appId", "displayName", "priority", "enabled", "settings"},
		},
		{
			name:  "apps usage-controls get",
			cmd:   appsUsageControlsGetCmd,
			args:  []string{"app-1"},
			idKey: "appId",
			// One body answers both GETs; "app" is there for the app read.
			body:        `{"appUsageControls":{"appId":"app-1","notify":true,"notifyAfterDays":30,"revoke":false,"revokeAfterDays":0},"hasUsageData":true,"app":{"id":"app-1"}}`,
			payloadPath: []string{"appUsageControls"},
			wantKeys:    []string{"appId", "notify", "notifyAfterDays", "revoke", "revokeAfterDays", "hasUsageData", "app"},
		},
		{
			name:        "apps resources get",
			cmd:         appsResourcesGetCmd,
			args:        []string{"r-1"},
			flags:       map[string]string{"app-id": "app-1", "resource-type-id": "rt-1"},
			idKey:       "id",
			body:        `{"appResourceView":{"appResource":{"id":"r-1","appId":"app-1","appResourceTypeId":"rt-1","displayName":"zz"},"appPath":"","resourceTypePath":"","parentResourceTypePath":"","parentResourcePath":"","objectPermissions":{"read":true}},"expanded":[]}`,
			payloadPath: []string{"appResourceView", "appResource"},
			wantKeys:    []string{"id", "appId", "appResourceTypeId", "displayName", "appPath", "resourceTypePath", "parentResourceTypePath", "parentResourcePath", "objectPermissions", "expanded"},
		},
		{
			name:        "policies get",
			cmd:         policiesGetCmd,
			args:        []string{"pol-1"},
			idKey:       "id",
			body:        `{"policy":{"id":"pol-1","displayName":"Nightly review"}}`,
			payloadPath: []string{"policy"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "automations executions get",
			cmd:         automationsExecutionsGetCmd,
			args:        []string{"7"},
			idKey:       "id",
			body:        `{"automationExecution":{"id":"7","automationTemplateId":"aut-1","state":"AUTOMATION_EXECUTION_STATE_DONE","context":{"context":{}}},"expanded":[],"view":null}`,
			payloadPath: []string{"automationExecution"},
			wantKeys:    []string{"id", "automationTemplateId", "state", "context", "expanded", "view"},
		},
		{
			name:        "automations get",
			cmd:         automationsGetCmd,
			args:        []string{"aut-1"},
			idKey:       "id",
			body:        `{"automation":{"id":"aut-1","displayName":"Nightly"}}`,
			payloadPath: []string{"automation"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "functions get",
			cmd:         functionsGetCmd,
			args:        []string{"fn-1"},
			idKey:       "id",
			body:        `{"function":{"id":"fn-1","displayName":"scorer"}}`,
			payloadPath: []string{"function"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "functions invocations get",
			cmd:         functionsInvocationsGetCmd,
			args:        []string{"inv-1"},
			flags:       map[string]string{"function-id": "fn-1"},
			idKey:       "id",
			body:        `{"invocation":{"id":"inv-1","status":"FUNCTION_INVOCATION_STATUS_SUCCESS"}}`,
			payloadPath: []string{"invocation"},
			wantKeys:    []string{"id", "status"},
		},
		{
			name:        "users get",
			cmd:         usersGetCmd,
			args:        []string{"usr-1"},
			idKey:       "id",
			body:        `{"userView":{"user":{"id":"usr-1","email":"a@b.example"},"userId":"usr-1","rolesPath":"","objectPermissions":{"read":true}},"expanded":[{"id":"role-1"}]}`,
			wantKeys:    []string{"id", "email", "userId", "rolesPath", "objectPermissions", "expanded"},
			payloadPath: []string{"userView", "user"},
		},
		{
			name:        "requests get",
			cmd:         requestsGetCmd,
			args:        []string{"req-1"},
			idKey:       "id",
			body:        `{"taskView":{"task":{"id":"req-1","state":"TASK_STATE_OPEN"},"userPath":"","appPath":"","objectPermissions":null},"expanded":[{"id":"ent-1"}]}`,
			wantKeys:    []string{"id", "state", "userPath", "appPath", "objectPermissions", "expanded"},
			payloadPath: []string{"taskView", "task"},
		},
		{
			name:        "tasks get",
			cmd:         tasksGetCmd,
			args:        []string{"task-1"},
			idKey:       "id",
			body:        `{"taskView":{"task":{"id":"task-1","state":"TASK_STATE_OPEN"},"userPath":"","appPath":"","objectPermissions":null},"expanded":[{"id":"ent-1"}]}`,
			wantKeys:    []string{"id", "state", "userPath", "appPath", "objectPermissions", "expanded"},
			payloadPath: []string{"taskView", "task"},
		},
		{
			name:        "entitlements get",
			cmd:         entitlementsGetCmd,
			args:        []string{"ent-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"appEntitlementView":{"appEntitlement":{"id":"ent-1","displayName":"Admin"},"appPath":"","objectPermissions":{"read":true}},"expanded":[{"id":"app-1"}]}`,
			wantKeys:    []string{"id", "displayName", "appPath", "objectPermissions", "expanded"},
			payloadPath: []string{"appEntitlementView", "appEntitlement"},
		},
		{
			name: "entitlements proxy-bindings get",
			cmd:  entitlementsProxyBindingsGetCmd,
			flags: map[string]string{
				"source-app-id":              "src-app",
				"source-entitlement-id":      "src-ent",
				"destination-app-id":         "dst-app",
				"destination-entitlement-id": "dst-ent",
			},
			idKey:       "srcAppId",
			body:        `{"appProxyEntitlementView":{"appProxyEntitlement":{"srcAppId":"src-app","srcAppEntitlementId":"src-ent","dstAppId":"dst-app","dstAppEntitlementId":"dst-ent"},"srcAppPath":"src-path","dstAppPath":"dst-path"},"expanded":[{"id":"exp"}]}`,
			payloadPath: []string{"appProxyEntitlementView", "appProxyEntitlement"},
			wantKeys:    []string{"srcAppId", "srcAppEntitlementId", "dstAppId", "dstAppEntitlementId", "srcAppPath", "dstAppPath", "expanded"},
		},
		{
			name:        "connectors get",
			cmd:         connectorsGetCmd,
			args:        []string{"cn-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"connectorView":{"connector":{"id":"cn-1","appId":"app-1","displayName":"Okta"},"appPath":"","capabilitiesPath":"","usersPath":""},"expanded":[]}`,
			payloadPath: []string{"connectorView", "connector"},
			wantKeys:    []string{"id", "appId", "displayName", "appPath", "capabilitiesPath", "usersPath", "expanded"},
		},
		{
			name:        "connectors credentials get",
			cmd:         connectorCredentialsGetCmd,
			args:        []string{"foo-bar-12345"},
			flags:       map[string]string{"app-id": "app-1", "connector-id": "cn-1"},
			idKey:       "id",
			body:        `{"credential":{"id":"foo-bar-12345","clientId":"foo@acme.c1/ccc","clientSecret":""}}`,
			payloadPath: []string{"credential"},
			wantKeys:    []string{"id", "clientId", "clientSecret"},
		},
		{
			name:        "mcp servers get",
			cmd:         mcpServersGetCmd,
			args:        []string{"conn-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "connectorId",
			body:        `{"mcpServer":{"connectorId":"conn-1","appId":"app-1","displayName":"Datadog"}}`,
			payloadPath: []string{"mcpServer"},
			wantKeys:    []string{"connectorId", "appId", "displayName"},
		},
		{
			name:        "mcp tools get",
			cmd:         mcpToolsGetCmd,
			args:        []string{"tool-1"},
			flags:       map[string]string{"app-id": "app-1", "connector-id": "conn-1"},
			idKey:       "id",
			body:        `{"tool":{"id":"tool-1","name":"dd_search"}}`,
			payloadPath: []string{"tool"},
			wantKeys:    []string{"id", "name"},
		},
		{
			name:        "mcp prompts get",
			cmd:         mcpPromptsGetCmd,
			args:        []string{"p-1"},
			flags:       map[string]string{"app-id": "app-1", "connector-id": "conn-1"},
			idKey:       "id",
			body:        `{"prompt":{"id":"p-1","name":"summarize","arguments":[{"name":"topic"}]}}`,
			payloadPath: []string{"prompt"},
			wantKeys:    []string{"id", "name", "arguments"},
		},
		{
			name:        "mcp resources get",
			cmd:         mcpResourcesGetCmd,
			args:        []string{"r-1"},
			flags:       map[string]string{"app-id": "app-1", "connector-id": "conn-1"},
			idKey:       "id",
			body:        `{"resource":{"id":"r-1","name":"readme","uri":"file:///readme.md"}}`,
			payloadPath: []string{"resource"},
			wantKeys:    []string{"id", "name", "uri"},
		},
		{
			name:        "mcp toolsets get",
			cmd:         mcpToolsetsGetCmd,
			args:        []string{"ts-1"},
			flags:       map[string]string{"app-id": "app-1", "connector-id": "conn-1"},
			idKey:       "id",
			body:        `{"profile":{"id":"ts-1","displayName":"read-only"}}`,
			payloadPath: []string{"profile"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "mcp classifiers get",
			cmd:         mcpClassifiersGetCmd,
			args:        []string{"classifier-1"},
			idKey:       "id",
			body:        `{"classifier":{"id":"classifier-1","displayName":"Observe"}}`,
			payloadPath: []string{"classifier"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "mcp classifiers templates get",
			cmd:         mcpClassifierTemplatesGetCmd,
			args:        []string{"template-1"},
			idKey:       "id",
			body:        `{"template":{"id":"template-1","displayName":"Balanced"}}`,
			payloadPath: []string{"template"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "mcp classifiers tool-gates get",
			cmd:         mcpClassifierToolGatesGetCmd,
			args:        []string{"gate-1"},
			idKey:       "id",
			body:        `{"toolGate":{"id":"gate-1","displayName":"Destructive"}}`,
			payloadPath: []string{"toolGate"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "mcp classifiers policy show",
			cmd:         mcpClassifierPolicyShowCmd,
			idKey:       "agentId",
			body:        `{"policy":{"agentId":"agent-1","defaultOutcome":"AGENT_CLASSIFIER_RULE_OUTCOME_ALLOWED"}}`,
			payloadPath: []string{"policy"},
			wantKeys:    []string{"agentId", "defaultOutcome"},
		},
		{
			name:        "settings ai-governance show",
			cmd:         settingsAIGovernanceShowCmd,
			idKey:       "auditVerbosity",
			body:        `{"aiGovernanceSettings":{"auditVerbosity":"AUDIT_VERBOSITY_STANDARD","requireToolApproval":true}}`,
			payloadPath: []string{"aiGovernanceSettings"},
			wantKeys:    []string{"auditVerbosity", "requireToolApproval"},
		},
		{
			name:        "settings session show",
			cmd:         settingsSessionShowCmd,
			idKey:       "maxSessionLength",
			body:        `{"sessionSettings":{"maxSessionLength":"72000s","externalClientsEnabled":true}}`,
			payloadPath: []string{"sessionSettings"},
			wantKeys:    []string{"maxSessionLength", "externalClientsEnabled"},
		},
		{
			name:        "settings sso show",
			cmd:         settingsSSOShowCmd,
			idKey:       "defaultSubjectType",
			body:        `{"settings":{"enabled":false,"defaultSubjectType":"SSO_SUBJECT_TYPE_UNSPECIFIED"}}`,
			payloadPath: []string{"settings"},
			wantKeys:    []string{"enabled", "defaultSubjectType"},
		},
		{
			name:        "settings cross-app-access show",
			cmd:         settingsXAAShowCmd,
			idKey:       "defaultSigningAlgorithm",
			body:        `{"settings":{"enabled":false,"defaultSigningAlgorithm":"XAA_SIGNING_ALGORITHM_UNSPECIFIED"}}`,
			payloadPath: []string{"settings"},
			wantKeys:    []string{"enabled", "defaultSigningAlgorithm"},
		},
		{
			name:        "settings contacts show",
			cmd:         settingsContactsShowCmd,
			idKey:       "createdAt",
			body:        `{"contacts":{"securityEmails":[],"createdAt":"2026-01-01T00:00:00Z"}}`,
			payloadPath: []string{"contacts"},
			wantKeys:    []string{"securityEmails", "createdAt"},
		},
		{
			name:              "settings request show",
			cmd:               settingsRequestShowCmd,
			idKey:             "maxBulkEntitlementSelection",
			allowNonStringKey: true,
			body:              `{"requestSettings":{"skipJustification":false,"maxBulkEntitlementSelection":10}}`,
			payloadPath:       []string{"requestSettings"},
			wantKeys:          []string{"skipJustification", "maxBulkEntitlementSelection"},
		},
		{
			name:        "settings aws-external-id show",
			cmd:         settingsAWSExternalIDShowCmd,
			idKey:       "externalId",
			body:        `{"awsExternalId":{"externalId":"ext-1"}}`,
			payloadPath: []string{"awsExternalId"},
			wantKeys:    []string{"externalId"},
		},
		{
			name:              "settings notifications org show",
			cmd:               settingsNotificationsOrgShowCmd,
			idKey:             "channelSettings",
			allowNonStringKey: true,
			body:              `{"orgNotificationSettings":{"channelSettings":{"email":{"enabled":true}}}}`,
			payloadPath:       []string{"orgNotificationSettings"},
			wantKeys:          []string{"channelSettings"},
		},
		{
			name:              "settings notifications user show",
			cmd:               settingsNotificationsUserShowCmd,
			idKey:             "channelSettings",
			allowNonStringKey: true,
			body:              `{"userNotificationSettings":{"channelSettings":{"email":{"enabled":true}}}}`,
			payloadPath:       []string{"userNotificationSettings"},
			wantKeys:          []string{"channelSettings"},
		},
		{
			name:        "mcp toolsets get-by-entitlement",
			cmd:         mcpToolsetsGetByEntitlementCmd,
			args:        []string{"ent-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"profile":{"id":"ts-1","appEntitlementId":"ent-1"}}`,
			payloadPath: []string{"profile"},
			wantKeys:    []string{"id", "appEntitlementId"},
		},
		{
			// The catalog sits two levels down and memberCount rides beside
			// it on the view, so a hoist that took only requestCatalogView
			// would strip the id and one that took only requestCatalog would
			// drop the count.
			name:        "access-profiles get",
			cmd:         accessProfilesGetCmd,
			args:        []string{"cat-1"},
			idKey:       "id",
			body:        `{"requestCatalogView":{"requestCatalog":{"id":"cat-1","displayName":"Engineering","published":true},"memberCount":"7","createdByUserPath":"","accessEntitlementsPath":""},"expanded":[{"id":"ent-1"}]}`,
			payloadPath: []string{"requestCatalogView", "requestCatalog"},
			wantKeys:    []string{"id", "displayName", "published", "memberCount", "createdByUserPath", "accessEntitlementsPath", "expanded"},
		},
		{
			name:        "access-reviews get",
			cmd:         accessReviewsGetCmd,
			args:        []string{"review-1"},
			idKey:       "id",
			body:        `{"accessReview":{"accessReview":{"id":"review-1","displayName":"Q4 certification"},"policyPath":"","objectPermissions":{"read":true}},"expanded":[{"id":"policy-1"}]}`,
			payloadPath: []string{"accessReview", "accessReview"},
			wantKeys:    []string{"id", "displayName", "policyPath", "objectPermissions", "expanded"},
		},
		{
			name:        "export exporters get",
			cmd:         exportExportersGetCmd,
			args:        []string{"ex-1"},
			idKey:       "exportId",
			body:        `{"exporter":{"exportId":"ex-1","displayName":"zz"}}`,
			payloadPath: []string{"exporter"},
			wantKeys:    []string{"exportId", "displayName"},
		},
		{
			name:        "reports get",
			cmd:         reportsGetCmd,
			args:        []string{"rp-1"},
			idKey:       "id",
			body:        `{"report":{"id":"rp-1","displayName":"zz"},"latestRun":{"id":"run-1"},"latestSuccessfulRun":null,"promptDrifted":false}`,
			payloadPath: []string{"report"},
			wantKeys:    []string{"id", "displayName", "latestRun", "latestSuccessfulRun", "promptDrifted"},
		},
		{
			name:        "mcp servers catalog get",
			cmd:         mcpServersCatalogGetCmd,
			args:        []string{"cat-1"},
			idKey:       "id",
			body:        `{"catalogEntry":{"id":"cat-1","serviceName":"datadog"}}`,
			payloadPath: []string{"catalogEntry"},
			wantKeys:    []string{"id", "serviceName"},
		},
		{
			name:        "roles get",
			cmd:         rolesGetCmd,
			args:        []string{"role-1"},
			idKey:       "id",
			body:        `{"role":{"id":"role-1","displayName":"Read-Only Administrator"}}`,
			payloadPath: []string{"role"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "service-principals get",
			cmd:         servicePrincipalsGetCmd,
			args:        []string{"sp-1"},
			idKey:       "id",
			body:        `{"servicePrincipal":{"id":"sp-1","displayName":"payments-reconciler"}}`,
			payloadPath: []string{"servicePrincipal"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "webhooks get",
			cmd:         webhooksGetCmd,
			args:        []string{"wh-1"},
			idKey:       "id",
			body:        `{"webhook":{"id":"wh-1","displayName":"zz-webhook-a","deletedAt":null}}`,
			payloadPath: []string{"webhook"},
			wantKeys:    []string{"id", "displayName", "deletedAt"},
		},
		{
			name:        "hooks get",
			cmd:         hooksGetCmd,
			args:        []string{"h-1"},
			idKey:       "id",
			body:        `{"hook":{"id":"h-1","displayName":"zz-hook-a","filter":null}}`,
			payloadPath: []string{"hook"},
			wantKeys:    []string{"id", "displayName", "filter"},
		},
		{
			name:        "attributes get",
			cmd:         attributesGetCmd,
			args:        []string{"av-1"},
			idKey:       "id",
			body:        `{"value":{"id":"av-1","attributeTypeId":"ty-1","value":"zz","deletedAt":null}}`,
			payloadPath: []string{"value"},
			wantKeys:    []string{"id", "attributeTypeId", "value", "deletedAt"},
		},
		{
			name:        "attribute-mappings get",
			cmd:         attributeMappingsGetCmd,
			args:        []string{"map-1"},
			idKey:       "id",
			body:        `{"attribute":{"userAttributeMapping":{"id":"map-1","appId":"app-1","attributeType":{"id":"ty-1","displayName":"Department"}},"appPath":"","fallbacksPath":""},"expanded":[]}`,
			payloadPath: []string{"attribute", "userAttributeMapping"},
			wantKeys:    []string{"id", "appId", "attributeType", "expanded", "appPath", "fallbacksPath"},
		},
		{
			name:        "entitlements automation get",
			cmd:         entitlementsAutomationGetCmd,
			args:        []string{"e-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "appEntitlementId",
			body:        `{"AppEntitlementAutomation":{"appId":"app-1","appEntitlementId":"e-1","displayName":"zz","cel":{"expression":"true"}}}`,
			payloadPath: []string{"AppEntitlementAutomation"},
			wantKeys:    []string{"appId", "appEntitlementId", "displayName", "cel"},
		},
		{
			name:        "access-profiles bundle-automation get",
			cmd:         accessProfilesBundleAutomationGetCmd,
			args:        []string{"cat-1"},
			idKey:       "requestCatalogId",
			body:        `{"requestCatalogId":"cat-1","enabled":true}`,
			payloadPath: nil,
			wantKeys:    []string{"requestCatalogId", "enabled"},
		},
		{
			name:        "service-principals credentials get",
			cmd:         spCredentialsGetCmd,
			args:        []string{"foo-bar-12345"},
			flags:       map[string]string{"service-principal-id": "sp-1"},
			idKey:       "id",
			body:        `{"credential":{"id":"foo-bar-12345","displayName":"ci","clientId":"foo@acme.c1/spc"}}`,
			payloadPath: []string{"credential"},
			wantKeys:    []string{"id", "displayName", "clientId"},
		},
		{
			name:        "findings get",
			cmd:         findingsGetCmd,
			args:        []string{"finding-1"},
			idKey:       "id",
			body:        `{"finding":{"id":"finding-1","state":"FINDING_STATE_OPEN"}}`,
			payloadPath: []string{"finding"},
			wantKeys:    []string{"id", "state"},
		},
		{
			name:        "findings routing-rules get",
			cmd:         findingRuleSubcommand(findingsRoutingRulesCmd, "get"),
			args:        []string{"routing-rule-1"},
			idKey:       "id",
			body:        `{"routingRule":{"id":"routing-rule-1","displayName":"Route"}}`,
			payloadPath: []string{"routingRule"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "findings transformation-rules get",
			cmd:         findingRuleSubcommand(findingsTransformationRulesCmd, "get"),
			args:        []string{"transformation-rule-1"},
			idKey:       "id",
			body:        `{"transformationRule":{"id":"transformation-rule-1","displayName":"Transform"}}`,
			payloadPath: []string{"transformationRule"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "access-reviews templates get",
			cmd:         accessReviewTemplatesGetCmd,
			args:        []string{"t1"},
			idKey:       "id",
			body:        `{"accessReviewTemplate":{"id":"t1","displayName":"zz","isCampaignScheduleEnabled":false}}`,
			payloadPath: []string{"accessReviewTemplate"},
			wantKeys:    []string{"id", "displayName", "isCampaignScheduleEnabled"},
		},
		{
			name:              "access-reviews templates scope get",
			cmd:               accessReviewTemplateScopeGetCmd,
			args:              []string{"t1"},
			idKey:             "scope",
			allowNonStringKey: true,
			body:              `{"list":[],"expanded":[],"scope":{"appAccess":{}},"excludedEntitlements":[],"excludedResourceTypes":[]}`,
			wantKeys:          []string{"list", "expanded", "scope", "excludedEntitlements", "excludedResourceTypes"},
		},
		{
			name:              "access-reviews scope get",
			cmd:               accessReviewScopeGetCmd,
			args:              []string{"ar1"},
			idKey:             "scopeV2",
			allowNonStringKey: true,
			body:              `{"list":[],"expanded":[],"scopeV2":{"appAccess":{}},"excludedEntitlements":[],"excludedResourceTypes":[]}`,
			wantKeys:          []string{"list", "expanded", "scopeV2", "excludedEntitlements", "excludedResourceTypes"},
		},
		{
			name:              "role mining config show",
			cmd:               roleMiningConfigShowCmd,
			idKey:             "minCohortSize",
			allowNonStringKey: true,
			body:              `{"config":{"minCohortSize":10,"maxSuggestions":25}}`,
			payloadPath:       []string{"config"},
			wantKeys:          []string{"minCohortSize", "maxSuggestions"},
		},
		{
			name:        "role mining runs latest",
			cmd:         roleMiningRunsLatestCmd,
			idKey:       "id",
			body:        `{"run":{"id":"run-1","status":"RUN_STATUS_COMPLETED"}}`,
			payloadPath: []string{"run"},
			wantKeys:    []string{"id", "status"},
		},
		{
			name:        "role mining suggestion get",
			cmd:         roleMiningSuggestionsGetCmd,
			args:        []string{"suggestion-1"},
			idKey:       "id",
			body:        `{"suggestion":{"id":"suggestion-1","suggestedName":"Finance"}}`,
			payloadPath: []string{"suggestion"},
			wantKeys:    []string{"id", "suggestedName"},
		},
		{
			name:        "role mining custom analysis get",
			cmd:         roleMiningCustomAnalysisGetCmd,
			args:        []string{"analysis-1"},
			idKey:       "id",
			body:        `{"id":"analysis-1","status":"COMPLETED"}`,
			payloadPath: nil,
			wantKeys:    []string{"id", "status"},
		},
		{
			name:        "role mining custom analysis latest",
			cmd:         roleMiningCustomAnalysisLatestCmd,
			idKey:       "id",
			body:        `{"result":{"id":"analysis-1","status":"COMPLETED"}}`,
			payloadPath: []string{"result"},
			wantKeys:    []string{"id", "status"},
		},

		// workload federation
		{
			name:        "service-principals trusts get",
			cmd:         spTrustsGetCmd,
			args:        []string{"t-1"},
			flags:       map[string]string{"service-principal-id": "sp-1"},
			idKey:       "clientId",
			body:        `{"trust":{"clientId":"t-1@tenant.example.com/wfe","displayName":"zz","c1EdgeService":null}}`,
			payloadPath: []string{"trust"},
			wantKeys:    []string{"clientId", "displayName", "c1EdgeService"},
		},
		{
			name:        "workload-federation providers get",
			cmd:         wfProvidersGetCmd,
			args:        []string{"pr-1"},
			idKey:       "id",
			body:        `{"provider":{"id":"pr-1","displayName":"zz","oidc":{}}}`,
			payloadPath: []string{"provider"},
			wantKeys:    []string{"id", "displayName", "oidc"},
		},
		{
			name:        "xaa resource-servers get",
			cmd:         xaaResourceServersGetCmd,
			args:        []string{"rs-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"resourceServer":{"id":"rs-1","appId":"app-1","displayName":"zz"},"scopeDiscoveryStatus":{"state":"XAA_SCOPE_DISCOVERY_STATE_FAILED"}}`,
			payloadPath: []string{"resourceServer"},
			wantKeys:    []string{"id", "appId", "displayName", "scopeDiscoveryStatus"},
		},
		{
			name:        "xaa scopes get",
			cmd:         xaaScopesGetCmd,
			args:        []string{"sc-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"scope":{"id":"sc-1","appId":"app-1","scopeValue":"zz.read"}}`,
			payloadPath: []string{"scope"},
			wantKeys:    []string{"id", "appId", "scopeValue"},
		},
		{
			name:        "xaa access-profiles get",
			cmd:         xaaAccessProfilesGetCmd,
			args:        []string{"ap-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"accessProfile":{"id":"ap-1","appId":"app-1","displayName":"zz"}}`,
			payloadPath: []string{"accessProfile"},
			wantKeys:    []string{"id", "appId", "displayName"},
		},
	}
}

func findingRuleSubcommand(group *cobra.Command, name string) *cobra.Command {
	for _, command := range group.Commands() {
		if command.Name() == name {
			return command
		}
	}
	panic("missing " + name + " subcommand")
}

// stubGetClient is stubSearchClient (cmd/mcp_servers_test.go) for the
// single-object read path; policies get builds its client through its own var.
func stubGetClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	stub := func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	orig, origPolicies, origSP := newClient, newPoliciesClient, newServicePrincipalsClient
	newClient, newPoliciesClient, newServicePrincipalsClient = stub, stub, stub
	t.Cleanup(func() { newClient, newPoliciesClient, newServicePrincipalsClient = orig, origPolicies, origSP })
}

// runGetCase drives one case's RunE against a stub API answering with its
// envelope, and returns the command's stdout.
func runGetCase(t *testing.T, c getUnwrapCase) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, c.body)
	}))
	defer srv.Close()

	resetCmdFlags(t, c.cmd)
	stubGetClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")
	for name, val := range c.flags {
		if err := c.cmd.Flags().Set(name, val); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}

	var out bytes.Buffer
	restoreCmdWriters(t)
	c.cmd.SetOut(&out)
	t.Cleanup(func() { c.cmd.SetOut(nil) })
	c.cmd.SetContext(context.Background())
	if err := c.cmd.RunE(c.cmd, c.args); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	return out.String()
}

// TestTypedGetExposesTopLevelID is the guard: `jq -r .id` (or .connectorId for
// an MCP server) must resolve on every typed get's output, and no envelope key
// may be lost on the way.
func TestTypedGetExposesTopLevelID(t *testing.T) {
	for _, c := range getUnwrapCases() {
		t.Run(c.name, func(t *testing.T) {
			var got map[string]any
			raw := runGetCase(t, c)
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("output is not a JSON object: %v\noutput: %s", err, raw)
			}
			id, ok := got[c.idKey]
			if !ok {
				t.Fatalf("output has no top-level %q (keys: %v)\noutput: %s", c.idKey, sortedKeys(got), raw)
			}
			if !c.allowNonStringKey {
				if s, isStr := id.(string); !isStr || s == "" {
					t.Errorf("top-level %q = %v, want a non-empty string", c.idKey, id)
				}
			}
			want := append([]string{}, c.wantKeys...)
			sort.Strings(want)
			if gotKeys := sortedKeys(got); !reflect.DeepEqual(gotKeys, want) {
				t.Errorf("top-level keys = %v, want exactly %v", gotKeys, want)
			}
			// Values, not just names: the payload's own keys and each sibling
			// passed on the way down must arrive carrying what they held.
			for key, val := range flattenEnvelope(t, c) {
				if !reflect.DeepEqual(got[key], val) {
					t.Errorf("%q = %#v, want %#v", key, got[key], val)
				}
			}
		})
	}
}

// TestTypedGetFieldsProjectsUnwrapped pins the composition order: --fields
// applies after unwrapping, so `--fields id` yields {"id":"…"} rather than
// rebuilding the wrapper around it.
func TestTypedGetFieldsProjectsUnwrapped(t *testing.T) {
	for _, c := range getUnwrapCases() {
		t.Run(c.name, func(t *testing.T) {
			setFieldsFlag(t, c.idKey)
			raw := runGetCase(t, c)
			var got map[string]any
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("output is not a JSON object: %v\noutput: %s", err, raw)
			}
			if len(got) != 1 {
				t.Fatalf("--fields %s produced %v, want just that one top-level key", c.idKey, sortedKeys(got))
			}
			if _, ok := got[c.idKey]; !ok {
				t.Errorf("--fields %s produced %v", c.idKey, sortedKeys(got))
			}
		})
	}
}

// TestTypedGetZeroMatchFieldsStillExitsUsage: unwrapping must not soften the
// zero-match rule — a --fields naming nothing in the response is still exit 2,
// not an empty object at exit 0.
func TestTypedGetZeroMatchFieldsStillExitsUsage(t *testing.T) {
	c := getUnwrapCases()[0]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, c.body)
	}))
	defer srv.Close()

	resetCmdFlags(t, c.cmd)
	stubGetClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")
	setFieldsFlag(t, "totally_bogus_field")

	var out bytes.Buffer
	restoreCmdWriters(t)
	c.cmd.SetOut(&out)
	t.Cleanup(func() { c.cmd.SetOut(nil) })
	c.cmd.SetContext(context.Background())
	err := c.cmd.RunE(c.cmd, c.args)
	if err == nil {
		t.Fatalf("expected a usage error, got nil and output %q", out.String())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d (err: %v)", got, want, err)
	}
}

// TestEveryTypedGetIsCovered fails when a `get`-shaped command anywhere in the
// tree is missing from getUnwrapCases, so a twelfth one can't ship unwrapped
// and untested.
func TestEveryTypedGetIsCovered(t *testing.T) {
	covered := map[string]bool{}
	for _, c := range getUnwrapCases() {
		covered[c.cmd.CommandPath()] = true
	}

	var missing []string
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			name := sub.Name()
			if (name == "get" || strings.HasPrefix(name, "get-")) && !covered[sub.CommandPath()] {
				missing = append(missing, sub.CommandPath())
			}
			walk(sub)
		}
	}
	walk(rootCmd)

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("these get commands are not covered by getUnwrapCases: %v\n"+
			"add them (and route their output through writeResource) so top-level id stays guaranteed", missing)
	}
}

// setFieldsFlag sets the --fields projection through viper, the same override
// the rest of this package's tests use (a viper.Set outranks both the flag and
// C1I_FIELDS, so anything less loses to another test's leftover).
func setFieldsFlag(t *testing.T, value string) {
	t.Helper()
	clearViperOverrides(t, "fields")
	viper.Set("fields", value)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// flattenEnvelope returns what unwrapping c.body must produce: the payload's
// keys plus every key sitting beside a level of c.payloadPath.
func flattenEnvelope(t *testing.T, c getUnwrapCase) map[string]any {
	t.Helper()
	var level map[string]any
	if err := json.Unmarshal([]byte(c.body), &level); err != nil {
		t.Fatalf("fixture body is not JSON: %v", err)
	}
	want := map[string]any{}
	for i, seg := range c.payloadPath {
		for k, v := range level {
			if k != seg {
				want[k] = v
			}
		}
		next, ok := level[seg].(map[string]any)
		if !ok {
			t.Fatalf("fixture payloadPath %v does not resolve at segment %d", c.payloadPath, i)
		}
		level = next
	}
	for k, v := range level {
		want[k] = v
	}
	return want
}
