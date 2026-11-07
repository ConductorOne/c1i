package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestEnumFlagParse(t *testing.T) {
	values := []enumValue{
		{name: "pending", api: "X_STATE_PENDING_REVIEW", aliases: []string{"queued"}},
		{name: "done", api: "X_STATE_DONE"},
	}
	reject := enumFlag{flag: "state", prefix: "X_STATE_", values: values, unknown: enumReject}
	prefixed := reject
	prefixed.unknown = enumPassPrefixed
	passAll := enumFlag{flag: "status", values: []enumValue{{name: "enabled", api: "ENABLED"}}, unknown: enumPassAny}

	cases := []struct {
		name string
		e    enumFlag
		in   string
		want string // "" with wantErr false means an empty result
		err  bool
	}{
		{"short", reject, "pending", "X_STATE_PENDING_REVIEW", false},
		{"case", reject, "PeNdInG", "X_STATE_PENDING_REVIEW", false},
		{"whitespace", reject, "  done\t", "X_STATE_DONE", false},
		{"alias", reject, "queued", "X_STATE_PENDING_REVIEW", false},
		{"suffix underscore", reject, "pending_review", "X_STATE_PENDING_REVIEW", false},
		{"suffix dash", reject, "Pending-Review", "X_STATE_PENDING_REVIEW", false},
		{"full upper", reject, "X_STATE_DONE", "X_STATE_DONE", false},
		{"full lower dashed", reject, " x-state-done ", "X_STATE_DONE", false},
		{"reject unknown", reject, "bogus", "", true},
		{"reject unknown prefixed", reject, "X_STATE_NEW", "", true},
		{"reject empty", reject, "", "", true},
		{"reject blank", reject, "  ", "", true},
		{"prefixed passes newer", prefixed, "x_state_new", "X_STATE_NEW", false},
		// prefix+short name is a possible future server value, not an alias.
		{"prefixed short name not remapped", prefixed, "X_STATE_PENDING", "X_STATE_PENDING", false},
		{"reject prefixed short name", reject, "X_STATE_PENDING", "", true},
		{"prefixed bare prefix refused", prefixed, "X_STATE_", "", true},
		{"prefixed refuses short unknown", prefixed, "bogus", "", true},
		{"prefixed empty is unset", prefixed, "", "", false},
		{"prefixed blank refused", prefixed, " ", "", true},
		{"any passes trimmed", passAll, " Suspended ", "Suspended", false},
		{"any maps known", passAll, "Enabled", "ENABLED", false},
		{"any empty is unset", passAll, "", "", false},
	}
	for _, c := range cases {
		got, err := c.e.parse(c.in)
		if c.err {
			var ue *usageError
			if !errors.As(err, &ue) {
				t.Errorf("%s: parse(%q) = %q, %v; want a usageError", c.name, c.in, got, err)
			} else if !strings.Contains(err.Error(), "invalid --"+c.e.flag+" ") {
				t.Errorf("%s: error %q does not name --%s", c.name, err, c.e.flag)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: parse(%q) = %q, %v; want %q", c.name, c.in, got, err, c.want)
		}
	}

	if got := reject.usage(); got != "pending, done, or the full X_STATE_* name of one" {
		t.Errorf("reject usage = %q", got)
	}
	if got := prefixed.usage(); got != "pending, done, or any X_STATE_* value" {
		t.Errorf("prefixed usage = %q", got)
	}
	if _, err := reject.as("other").parse("bogus"); err == nil || !strings.Contains(err.Error(), "--other") {
		t.Errorf("as() did not rename the flag: %v", err)
	}
}

// allEnumFlags is every table, so the invariant test below covers a new one
// only if it is added here.
var allEnumFlags = map[string]enumFlag{
	"users --status":                 userStatusFlag,
	"users --origin":                 userOriginFlag,
	"accounts --status":              appUserStatusFlag,
	"accounts --type":                appUserTypeFlag,
	"policies --policy-type":         policyTypeFlag,
	"apps --identity-matching":       identityMatchingFlag,
	"mcp tools --state":              toolStateFlag,
	"mcp tools --classification":     toolClassificationFlag,
	"mcp servers --type":             serverTypeFlag,
	"mcp servers --data-sens":        dataSensitivityFlag,
	"mcp servers --transport":        transportTypeFlag,
	"mcp servers --token-sharing":    tokenSharingFlag,
	"access-profiles --enroll":       catalogEnrollmentFlag,
	"access-profiles --unenroll":     catalogUnenrollmentFlag,
	"access-profiles --unenroll-e":   catalogUnenrollmentEntitlementFlag,
	"automations --state":            executionStateFlag,
	"tasks --state":                  taskStateFlag,
	"export --sort":                  sortDirectionFlag,
	"access-reviews --format":        reportFormatFlag,
	"mcp servers --auth":             authFlag,
	"--print-config-template --auth": authTemplateFlag,
}

// TestEnumFlagTablesAreConsistent: every API name carries the prefix, and no
// two inputs collide once case and separators are normalised.
func TestEnumFlagTablesAreConsistent(t *testing.T) {
	for name, e := range allEnumFlags {
		seen := map[string]string{}
		add := func(in, api string) {
			k := enumKey(in)
			if prev, ok := seen[k]; ok && prev != api {
				t.Errorf("%s: %q maps to both %s and %s", name, in, prev, api)
			}
			seen[k] = api
		}
		for _, kw := range e.keywords {
			add(kw, "keyword "+kw)
		}
		for _, v := range e.values {
			if !strings.HasPrefix(v.api, e.prefix) {
				t.Errorf("%s: %s lacks prefix %q", name, v.api, e.prefix)
			}
			if strings.Contains(v.name, "_") {
				t.Errorf("%s: short name %q: use '-' as the separator", name, v.name)
			}
			for _, k := range e.inputs(v) {
				add(k, v.api)
			}
		}
		if e.unknown == enumPassAny && e.prefix != "" {
			t.Errorf("%s: enumPassAny is for unprefixed enums; use enumPassPrefixed", name)
		}
	}
}

// TestEnumFlagValues pins every flag's short names (all accepted before the
// shared table) and the forms the table added: the other separator, the full
// API name in any case, and surrounding whitespace.
func TestEnumFlagValues(t *testing.T) {
	const bad = "\x00error"
	cases := []struct {
		e    enumFlag
		rows map[string]string
	}{
		// The users enum is bare (ENABLED, not STATUS_ENABLED like app_users);
		// the prefixed form 400s. Unknowns still pass: there is no prefix to
		// tell a newer value from a typo.
		{userStatusFlag, map[string]string{
			"enabled": "ENABLED", "disabled": "DISABLED", "deleted": "DELETED",
			"ENABLED": "ENABLED", "Disabled": "DISABLED", "": "",
			" enabled ": "ENABLED", "unknown": "unknown",
			"enabıed": bad,
		}},
		{userOriginFlag, map[string]string{
			"directory": "USER_ORIGIN_DIRECTORY", "LOCAL": "USER_ORIGIN_LOCAL", "system": "USER_ORIGIN_SYSTEM",
			"all": "all", "user_origin_local": "USER_ORIGIN_LOCAL", "user-origin-future": "USER_ORIGIN_FUTURE",
			"bogus":     bad,
			"dırectory": bad,
		}},
		{appUserStatusFlag, map[string]string{
			"enabled": "STATUS_ENABLED", "disabled": "STATUS_DISABLED", "deleted": "STATUS_DELETED",
			"ENABLED": "STATUS_ENABLED", "Disabled": "STATUS_DISABLED", "": "",
			"status_enabled": "STATUS_ENABLED", " deleted ": "STATUS_DELETED", "unknown": bad,
		}},
		{appUserTypeFlag, map[string]string{
			"user": "APP_USER_TYPE_USER", "service_account": "APP_USER_TYPE_SERVICE_ACCOUNT",
			"system_account": "APP_USER_TYPE_SYSTEM_ACCOUNT", "USER": "APP_USER_TYPE_USER",
			"Service_Account": "APP_USER_TYPE_SERVICE_ACCOUNT", "": "",
			"service-account": "APP_USER_TYPE_SERVICE_ACCOUNT", "app_user_type_user": "APP_USER_TYPE_USER", "unknown": bad,
		}},
		{policyTypeFlag, map[string]string{
			"grant": "POLICY_TYPE_GRANT", "GRANT": "POLICY_TYPE_GRANT", "revoke": "POLICY_TYPE_REVOKE",
			"certify": "POLICY_TYPE_CERTIFY", "access_request": "POLICY_TYPE_ACCESS_REQUEST",
			"access-request": "POLICY_TYPE_ACCESS_REQUEST", "accessrequest": "POLICY_TYPE_ACCESS_REQUEST",
			"provision": "POLICY_TYPE_PROVISION", "POLICY_TYPE_FOO": "POLICY_TYPE_FOO", "": "",
			" grant ": "POLICY_TYPE_GRANT", "policy_type_revoke": "POLICY_TYPE_REVOKE", "bogus": bad,
		}},
		{identityMatchingFlag, map[string]string{
			"strict": "APP_USER_IDENTITY_MATCHING_STRICT", "display-name": "APP_USER_IDENTITY_MATCHING_DISPLAY_NAME",
			"display_name": "APP_USER_IDENTITY_MATCHING_DISPLAY_NAME", "CUSTOM": "APP_USER_IDENTITY_MATCHING_CUSTOM",
			"app_user_identity_matching_strict": "APP_USER_IDENTITY_MATCHING_STRICT",
			"unspecified":                       bad, "APP_USER_IDENTITY_MATCHING_UNSPECIFIED": bad, "fuzzy": bad, "": bad,
			"ſtrict": bad, "cuſtom": bad,
		}},
		{toolStateFlag, map[string]string{
			"pending": "MCP_TOOL_STATE_PENDING_REVIEW", "pending_review": "MCP_TOOL_STATE_PENDING_REVIEW",
			"PENDING": "MCP_TOOL_STATE_PENDING_REVIEW", "approved": "MCP_TOOL_STATE_APPROVED",
			"Approved": "MCP_TOOL_STATE_APPROVED", "disabled": "MCP_TOOL_STATE_DISABLED",
			"removed": "MCP_TOOL_STATE_REMOVED", "": "",
			"pending-review": "MCP_TOOL_STATE_PENDING_REVIEW", "mcp_tool_state_approved": "MCP_TOOL_STATE_APPROVED",
			"MCP_TOOL_STATE_FUTURE": "MCP_TOOL_STATE_FUTURE", "unknown": bad,
			"pendıng": bad, "approvedſ": bad, "MCP_TOOL_STATE_PENDING": "MCP_TOOL_STATE_PENDING_REVIEW", "mcp_tool_state_approved_v2": "MCP_TOOL_STATE_APPROVED_V2", "MCP_TOOL_STATE_DISABLED_SOFT": "MCP_TOOL_STATE_DISABLED_SOFT",
		}},
		{toolClassificationFlag, map[string]string{
			"read": "TOOL_CLASSIFICATION_READ", "READ": "TOOL_CLASSIFICATION_READ", "write": "TOOL_CLASSIFICATION_WRITE",
			"destructive": "TOOL_CLASSIFICATION_DESTRUCTIVE", "sensitive": "TOOL_CLASSIFICATION_SENSITIVE",
			"dangerous": "TOOL_CLASSIFICATION_DANGEROUS", "": "",
			" read": "TOOL_CLASSIFICATION_READ", "tool_classification_write": "TOOL_CLASSIFICATION_WRITE", "unknown": bad,
		}},
		{serverTypeFlag, map[string]string{
			"hosted": "MCP_SERVER_TYPE_HOSTED", "EXTERNAL": "MCP_SERVER_TYPE_EXTERNAL",
			"mcp_server_type_hosted": "MCP_SERVER_TYPE_HOSTED", "passthrough": bad,
			"MCP_SERVER_TYPE_FUTURE": bad, "": bad,
		}},
		{dataSensitivityFlag, map[string]string{
			"confidential": "MCP_SERVER_DATA_SENSITIVITY_CONFIDENTIAL", "Public": "MCP_SERVER_DATA_SENSITIVITY_PUBLIC",
			"internal": "MCP_SERVER_DATA_SENSITIVITY_INTERNAL", "restricted": "MCP_SERVER_DATA_SENSITIVITY_RESTRICTED",
			"public ": "MCP_SERVER_DATA_SENSITIVITY_PUBLIC", "": "", "secret": bad,
		}},
		{transportTypeFlag, map[string]string{
			"sse": "MCP_SERVER_TRANSPORT_TYPE_SSE", "streamable-http": "MCP_SERVER_TRANSPORT_TYPE_STREAMABLE_HTTP",
			"streamable_http": "MCP_SERVER_TRANSPORT_TYPE_STREAMABLE_HTTP", "http": "MCP_SERVER_TRANSPORT_TYPE_STREAMABLE_HTTP",
			"mcp_server_transport_type_sse": "MCP_SERVER_TRANSPORT_TYPE_SSE", "stdio": bad,
		}},
		{tokenSharingFlag, map[string]string{
			"per-user": "MCP_SERVER_TOKEN_SHARING_PER_USER", "per_user": "MCP_SERVER_TOKEN_SHARING_PER_USER",
			"peruser": "MCP_SERVER_TOKEN_SHARING_PER_USER", "shared": "MCP_SERVER_TOKEN_SHARING_SHARED",
			" shared": "MCP_SERVER_TOKEN_SHARING_SHARED", "global": bad,
		}},
		{catalogEnrollmentFlag, map[string]string{
			"unspecified":                        "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_UNSPECIFIED",
			"bypass":                             "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS_ENTITLEMENT_REQUEST_POLICY",
			"bypass-entitlement-request-policy":  "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS_ENTITLEMENT_REQUEST_POLICY",
			"bypass_entitlement_request_policy":  "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS_ENTITLEMENT_REQUEST_POLICY",
			"enforce":                            "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_ENFORCE_ENTITLEMENT_REQUEST_POLICY",
			"enforce-entitlement-request-policy": "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_ENFORCE_ENTITLEMENT_REQUEST_POLICY",
			"request_catalog_enrollment_behavior_bypass_entitlement_request_policy": "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS_ENTITLEMENT_REQUEST_POLICY",
			"": "", "maybe": bad,
			"REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS": "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS_ENTITLEMENT_REQUEST_POLICY",
		}},
		{catalogUnenrollmentFlag, map[string]string{
			"unspecified":        "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_UNSPECIFIED",
			"leave-access-as-is": "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_LEAVE_ACCESS_AS_IS",
			"leave_access_as_is": "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_LEAVE_ACCESS_AS_IS",
			"revoke-all":         "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_ALL",
			"revoke_all":         "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_ALL",
			"revoke-unjustified": "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_UNJUSTIFIED",
			"revoke_unjustified": "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_UNJUSTIFIED",
			" revoke-all ":       "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_ALL",
			"keep":               bad,
		}},
		{catalogUnenrollmentEntitlementFlag, map[string]string{
			"unspecified": "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_UNSPECIFIED",
			"bypass":      "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_BYPASS",
			"enforce":     "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_ENFORCE",
			"request_catalog_unenrollment_entitlement_behavior_bypass": "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_BYPASS",
			"maybe": bad,
		}},
		// Filtered client-side: an unknown short name used to match nothing and
		// exit 0.
		{executionStateFlag, map[string]string{
			"": "", "done": "AUTOMATION_EXECUTION_STATE_DONE", "DONE": "AUTOMATION_EXECUTION_STATE_DONE",
			"success": "AUTOMATION_EXECUTION_STATE_DONE", "error": "AUTOMATION_EXECUTION_STATE_ERROR",
			"Error": "AUTOMATION_EXECUTION_STATE_ERROR", "failed": "AUTOMATION_EXECUTION_STATE_ERROR",
			"failure": "AUTOMATION_EXECUTION_STATE_ERROR", "pending": "AUTOMATION_EXECUTION_STATE_PENDING",
			"creating": "AUTOMATION_EXECUTION_STATE_CREATING", "waiting": "AUTOMATION_EXECUTION_STATE_WAITING",
			"terminate": "AUTOMATION_EXECUTION_STATE_TERMINATE", "terminated": "AUTOMATION_EXECUTION_STATE_TERMINATE",
			"AUTOMATION_EXECUTION_STATE_PROCESS_STEP": "AUTOMATION_EXECUTION_STATE_PROCESS_STEP",
			"automation_execution_state_done":         "AUTOMATION_EXECUTION_STATE_DONE",
			"unknown":                                 bad,
		}},
		{taskStateFlag, map[string]string{
			"open": "TASK_STATE_OPEN", "closed": "TASK_STATE_CLOSED", "OPEN": "TASK_STATE_OPEN",
			" Closed ": "TASK_STATE_CLOSED", "TASK_STATE_OPEN": "TASK_STATE_OPEN",
			"bogus": bad, "pending": bad, "open,closed": bad, "TASK_STATE_UNSPECIFIED": bad,
		}},
		{sortDirectionFlag, map[string]string{
			"asc": "SORT_DIRECTION_ASC", "ASC": "SORT_DIRECTION_ASC", "desc": "SORT_DIRECTION_DESC",
			" Desc ": "SORT_DIRECTION_DESC", "sort_direction_desc": "SORT_DIRECTION_DESC", "newest": bad,
			"aſc": bad, "deſc": bad,
		}},
		{reportFormatFlag, map[string]string{
			"json": "ACCESS_REVIEW_REPORT_FORMAT_JSON", "CSV": "ACCESS_REVIEW_REPORT_FORMAT_CSV",
			" xlsx ":                          "ACCESS_REVIEW_REPORT_FORMAT_XLSX",
			"access_review_report_format_csv": "ACCESS_REVIEW_REPORT_FORMAT_CSV", "pdf": bad,
		}},
		{authFlag, map[string]string{
			"none": "none", "bearer-token": "bearerToken", "Bearer": "bearerToken", "bearer_token": "bearerToken",
			"bearerToken": "bearerToken", " custom-header ": "customHeader", "basic": "basicAuth", "basic-auth": "basicAuth",
			"oauth2": bad, "baſic": bad, "": bad,
		}},
		{authTemplateFlag, map[string]string{
			"bearer": "bearerToken", "oauth2": "oauth2", "aws": "awsSigv4", "aws-sigv4": "awsSigv4",
			"google": "googleServiceAccount", "google-service-account": "googleServiceAccount", "kerberos": bad,
		}},
	}
	for _, c := range cases {
		for in, want := range c.rows {
			got, err := c.e.parse(in)
			if want == bad {
				if err == nil {
					t.Errorf("--%s %q = %q, want a usage error", c.e.flag, in, got)
				}
				continue
			}
			if err != nil || got != want {
				t.Errorf("--%s %q = %q, %v; want %q", c.e.flag, in, got, err, want)
			}
		}
	}
}

// Blank --sort and --state keep meaning the default / no filter.
func TestEnumFlagBlankDefaults(t *testing.T) {
	for _, in := range []string{"", "  "} {
		if got, err := parseSortDirection(in); err != nil || got != "SORT_DIRECTION_ASC" {
			t.Errorf("parseSortDirection(%q) = %q, %v", in, got, err)
		}
	}
}
