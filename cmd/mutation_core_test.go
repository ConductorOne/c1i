package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// coreMutation is one migrated command with a canned response and what the
// helper must print for it.
type coreMutation struct {
	name   string
	args   []string
	body   string // the API response
	want   string // compact JSON the command prints
	method string // the first request
	path   string
	idPath string // top-level key that carries the id; "" when the document has none
}

var coreMutations = []coreMutation{
	{"service-principals create", []string{"service-principals", "create", "--display-name", "zz"},
		`{"servicePrincipal":{"id":"sp-1","displayName":"zz"}}`, `{"id":"sp-1","displayName":"zz"}`, "POST", "/api/v1/service_principals", "id"},
	{"service-principals update", []string{"service-principals", "update", "sp-1", "--display-name", "zz"},
		`{"servicePrincipal":{"id":"sp-1","displayName":"zz"}}`, `{"id":"sp-1","displayName":"zz"}`, "PATCH", "/api/v1/service_principals/sp-1", "id"},
	{"service-principals delete", []string{"service-principals", "delete", "sp-1"},
		`{}`, `{}`, "DELETE", "/api/v1/service_principals/sp-1", ""},
	{"service-principals credentials create", []string{"service-principals", "credentials", "create", "sp-1", "--display-name", "ci"},
		`{"credential":{"id":"c-1","clientId":"cid"},"clientSecret":"s3"}`, `{"id":"c-1","clientId":"cid","clientSecret":"s3"}`, "POST", "/api/v1/service_principals/sp-1/credentials", "id"},
	{"service-principals credentials update", []string{"service-principals", "credentials", "update", "c-1", "--service-principal-id", "sp-1", "--display-name", "n"},
		`{"credential":{"id":"c-1","displayName":"n"}}`, `{"id":"c-1","displayName":"n"}`, "PATCH", "/api/v1/service_principals/sp-1/credentials/c-1", "id"},
	{"service-principals credentials revoke", []string{"service-principals", "credentials", "revoke", "c-1", "--service-principal-id", "sp-1"},
		`{}`, `{}`, "DELETE", "/api/v1/service_principals/sp-1/credentials/c-1", ""},
	{"service-principals bindings add", []string{"service-principals", "bindings", "add", "--service-principal-id", "sp-1", "--function-id", "fn-1"},
		``, `{}`, "POST", spBindingsPath, ""},
	{"service-principals bindings delete", []string{"service-principals", "bindings", "delete", "--service-principal-id", "sp-1", "--function-id", "fn-1"},
		`{}`, `{}`, "POST", spBindingsDeletePath, ""},
	{"entitlements create", []string{"entitlements", "create", "--app-id", "app-1", "--display-name", "e", "--resource-type-id", "rt-1", "--resource-id", "r-1"},
		`{"appEntitlementView":{"appEntitlement":{"id":"e-1","appResourceTypeId":"rt-1","appResourceId":"r-1"},"appPath":"p"}}`,
		`{"id":"e-1","appResourceTypeId":"rt-1","appResourceId":"r-1","appPath":"p"}`, "POST", "/api/v1/apps/app-1/entitlements", "id"},
	{"entitlements update", []string{"entitlements", "update", "e-1", "--app-id", "app-1", "--description", "d"},
		`{"appEntitlementView":{"appEntitlement":{"id":"e-1"},"appPath":"p"}}`, `{"id":"e-1","appPath":"p"}`, "POST", "/api/v1/apps/app-1/entitlements/e-1", "id"},
	{"entitlements delete", []string{"entitlements", "delete", "e-1", "--app-id", "app-1"},
		`{}`, `{}`, "DELETE", "/api/v1/apps/app-1/entitlements/e-1", ""},
	{"entitlements add-owner", []string{"entitlements", "add-owner", "u1", "--app-id", "app-1", "--entitlement-id", "e-1"},
		`{}`, `{}`, "POST", "/api/v1/apps/app-1/entitlements/e-1/owners", ""},
	{"entitlements remove-owner", []string{"entitlements", "remove-owner", "u1", "--app-id", "app-1", "--entitlement-id", "e-1"},
		`{}`, `{}`, "DELETE", "/api/v1/apps/app-1/entitlements/e-1/owners/u1", ""},
	{"entitlements proxy-bindings create", []string{"entitlements", "proxy-bindings", "create", "--source-app-id", "sa", "--source-entitlement-id", "se", "--destination-app-id", "da", "--destination-entitlement-id", "de"},
		`{"appProxyEntitlementView":{"appProxyEntitlement":{"srcAppId":"sa","srcAppEntitlementId":"se","dstAppId":"da","dstAppEntitlementId":"de"},"srcAppPath":"x"}}`,
		`{"srcAppId":"sa","srcAppEntitlementId":"se","dstAppId":"da","dstAppEntitlementId":"de","srcAppPath":"x"}`, "POST", "/api/v1/apps/sa/se/bindings/da/de", "srcAppId"},
	{"entitlements proxy-bindings delete", []string{"entitlements", "proxy-bindings", "delete", "--source-app-id", "sa", "--source-entitlement-id", "se", "--destination-app-id", "da", "--destination-entitlement-id", "de"},
		`{}`, `{}`, "DELETE", "/api/v1/apps/sa/se/bindings/da/de", ""},
	{"webhooks create", []string{"webhooks", "create", "--display-name", "zz", "--destination-url", "https://hooks.example.com/x"},
		`{"webhook":{"id":"wh-1","displayName":"zz"}}`, `{"id":"wh-1","displayName":"zz"}`, "POST", "/api/v1/webhooks", "id"},
	{"webhooks update", []string{"webhooks", "update", "wh-1", "--description", "d"},
		`{"webhook":{"id":"wh-1","description":"d"}}`, `{"id":"wh-1","description":"d"}`, "POST", "/api/v1/webhooks/wh-1", "id"},
	{"webhooks test", []string{"webhooks", "test", "wh-1"},
		`{"webhook":{"id":"inst-1","webhookId":"wh-1"}}`, `{"id":"inst-1","webhookId":"wh-1"}`, "POST", "/api/v1/webhooks/wh-1/test", "id"},
	{"webhooks delete", []string{"webhooks", "delete", "wh-1"},
		`{}`, `{}`, "DELETE", "/api/v1/webhooks/wh-1", ""},
	{"automations update", []string{"automations", "update", "a-1", "--enabled=false"},
		`{"automation":{"id":"a-1","enabled":false},"webhookCapabilityUrl":"","webhookHmacSecret":""}`,
		`{"id":"a-1","enabled":false,"webhookCapabilityUrl":"","webhookHmacSecret":""}`, "POST", "/api/v1/automations/a-1", "id"},
	{"automations delete", []string{"automations", "delete", "a-1"},
		`{}`, `{}`, "DELETE", "/api/v1/automations/a-1", ""},
	{"accounts set-owner", []string{"accounts", "set-owner", "au-1", "--app-id", "app-1", "--user-id", "u1"},
		`{"appUserView":{"appUser":{"id":"au-1","identityUserId":"u1"},"appPath":"p"}}`, `{"id":"au-1","identityUserId":"u1","appPath":"p"}`, "POST", "/api/v1/apps/app-1/app_users/au-1", "id"},
}

func indentJSON(t *testing.T, compact string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Indent(&b, []byte(compact), "", "  "); err != nil {
		t.Fatal(err)
	}
	return b.String() + "\n"
}

func TestCoreMutationStdout(t *testing.T) {
	for _, tc := range coreMutations {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, tc.body))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			if want := indentJSON(t, tc.want); r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
			if r.stderr != "" {
				t.Errorf("stderr = %q, want nothing", r.stderr)
			}
			got := s.requests()
			if len(got) != 1 || got[0].method != tc.method || got[0].rawPath != tc.path {
				t.Errorf("requests = %+v, want one %s %s", got, tc.method, tc.path)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
				t.Fatalf("stdout is not one JSON object: %v", err)
			}
			if tc.idPath != "" && (doc[tc.idPath] == nil || doc[tc.idPath] == "") {
				t.Errorf("no .%s in %s", tc.idPath, r.stdout)
			}
		})
	}
}

func TestCoreMutationPreview(t *testing.T) {
	for _, tc := range coreMutations {
		t.Run(tc.name, func(t *testing.T) {
			withDryRun(t)
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, nil, 0, nil, tc.args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || len(doc) != 2 || doc["dryRun"] != true || doc["requests"] == nil {
				t.Fatalf("stdout = %q, want exactly dryRun and requests", r.stdout)
			}
			reqs := doc["requests"].([]any)
			first := reqs[0].(map[string]any)
			if first["method"] != tc.method || first["url"] != mutTestURL+tc.path {
				t.Errorf("first request = %v %v, want %s %s", first["method"], first["url"], tc.method, mutTestURL+tc.path)
			}
			if r.stderr != "dry run: nothing was sent\n" {
				t.Errorf("stderr = %q, want the dry-run notice", r.stderr)
			}
		})
	}
}

func TestCoreMutationRefusesFields(t *testing.T) {
	for _, tc := range coreMutations {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, tc.body))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, append(append([]string(nil), tc.args...), "--fields", "id", "--error-format", "json")...)
			if r.exit != exitUsage || r.stdout != "" || len(s.requests()) != 0 {
				t.Errorf("exit %d stdout %q, %d requests; want exit 2, nothing printed or sent", r.exit, r.stdout, len(s.requests()))
			}
			if got := r.errorObject(t)["applied"]; got != false {
				t.Errorf("applied = %v, want false", got)
			}
		})
	}
}

func TestCoreMutationNotFoundIsNotApplied(t *testing.T) {
	for _, tc := range coreMutations {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(404, `{"code":5}`))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, append(append([]string(nil), tc.args...), "--error-format", "json")...)
			if r.exit != exitNotFound || r.stdout != "" {
				t.Errorf("exit %d stdout %q, want 4 and nothing on stdout", r.exit, r.stdout)
			}
			if got := r.errorObject(t)["applied"]; got != false {
				t.Errorf("applied = %v, want false", got)
			}
		})
	}
}

func TestCoreMutationEmptyBodyPrintsEmptyObject(t *testing.T) {
	s := newMutServer(t, fixedReply(200, ``))
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, s.srv, 0, nil, "webhooks", "delete", "wh-1")
	if r.err != nil || r.stdout != "{}\n" {
		t.Errorf("exit %d stdout %q, want {}", r.exit, r.stdout)
	}
}

// R22: a failed stdout write names what can't be re-read, but only when a
// secret was in the response.
func TestCoreMutationSecretWriteFailure(t *testing.T) {
	cases := []struct {
		name string
		args []string
		body string
		want string // "" = no extra wording
	}{
		{"credential", []string{"service-principals", "credentials", "create", "sp-1", "--display-name", "ci"},
			`{"credential":{"id":"c-1"},"clientSecret":"s3"}`, "the clientSecret in this response cannot be retrieved again; revoke the credential and create a new one"},
		{"automation with a webhook secret", []string{"automations", "update", "a-1", "--enabled"},
			`{"automation":{"id":"a-1"},"webhookHmacSecret":"s3"}`, "webhookHmacSecret/webhookCapabilityUrl were not delivered"},
		{"automation without one", []string{"automations", "update", "a-1", "--enabled"},
			`{"automation":{"id":"a-1"},"webhookHmacSecret":"","webhookCapabilityUrl":""}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, tc.body))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, failingWriter{}, append(append([]string(nil), tc.args...), "--error-format", "json")...)
			if r.exit != exitError {
				t.Errorf("exit = %d, want 1", r.exit)
			}
			obj := r.errorObject(t)
			msg, _ := obj["error"].(string)
			if obj["applied"] != true || !strings.Contains(msg, "succeeded but writing its response failed") {
				t.Errorf("error object = %v, want applied:true and the write-failure wording", obj)
			}
			if has := strings.Contains(msg, "cannot be retrieved again") || strings.Contains(msg, "were not delivered"); has != (tc.want != "") ||
				(tc.want != "" && !strings.Contains(msg, tc.want)) {
				t.Errorf("error = %q, want extra wording %q", msg, tc.want)
			}
			if strings.Contains(r.stderr, "s3") {
				t.Errorf("stderr leaks the secret: %s", r.stderr)
			}
		})
	}
}

func TestCoreSetOwnerAcceptsBothEnvelopes(t *testing.T) {
	for name, body := range map[string]string{
		"appUserView.appUser": `{"appUserView":{"appUser":{"id":"au-1","identityUserId":"u1"}}}`,
		"appUser":             `{"appUser":{"id":"au-1","identityUserId":"u1"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, body))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, "accounts", "set-owner", "au-1", "--app-id", "app-1", "--user-id", "u1")
			if want := indentJSON(t, `{"id":"au-1","identityUserId":"u1"}`); r.err != nil || r.stdout != want {
				t.Errorf("exit %d stdout %q, want %q", r.exit, r.stdout, want)
			}
		})
	}
}

// R19, R34: entitlements create prints only the last response and reports
// each attempted step on failure.
func TestCoreEntitlementsCreateSequence(t *testing.T) {
	route := func(failAt int) func(*http.Request, int) mutReply {
		return func(r *http.Request, n int) mutReply {
			if n == failAt {
				return mutReply{status: 400, body: `{"code":3}`}
			}
			switch {
			case strings.HasSuffix(r.URL.Path, "/resource_types"):
				return mutReply{body: `{"appResourceType":{"id":"rt-new"}}`}
			case strings.HasSuffix(r.URL.Path, "/resources"):
				return mutReply{body: `{"appResource":{"id":"res-new"}}`}
			}
			return mutReply{body: `{"appEntitlementView":{"appEntitlement":{"id":"e-new","appResourceTypeId":"rt-new","appResourceId":"res-new"}}}`}
		}
	}
	args := []string{"entitlements", "create", "--app-id", "app-1", "--display-name", "e"}

	t.Run("success", func(t *testing.T) {
		s := newMutServer(t, route(0))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, args...)
		want := indentJSON(t, `{"id":"e-new","appResourceTypeId":"rt-new","appResourceId":"res-new"}`)
		if r.err != nil || r.stdout != want || len(s.requests()) != 3 {
			t.Errorf("exit %d stdout %q, %d requests; want only the entitlement after 3", r.exit, r.stdout, len(s.requests()))
		}
	})
	t.Run("step 3 fails", func(t *testing.T) {
		s := newMutServer(t, route(3))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, append(args, "--error-format", "json")...)
		obj := r.errorObject(t)
		steps, _ := obj["steps"].([]any)
		if r.exit != exitUsage || obj["applied"] != true || len(steps) != 3 {
			t.Errorf("exit %d error object %v, want 2, applied:true and 3 steps", r.exit, obj)
		}
		if msg, _ := obj["error"].(string); !strings.Contains(msg, "--resource-type-id rt-new --resource-id res-new") {
			t.Errorf("error %q does not name what was created", msg)
		}
	})
	t.Run("step 1 fails", func(t *testing.T) {
		s := newMutServer(t, route(1))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, append(args, "--error-format", "json")...)
		if obj := r.errorObject(t); r.exit != exitUsage || obj["applied"] != false {
			t.Errorf("exit %d error object %v, want 2 and applied:false", r.exit, obj)
		}
		if got := len(s.requests()); got != 1 {
			t.Errorf("%d requests, want 1", got)
		}
	})
	t.Run("preview carries the placeholders literally", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, nil, 0, nil, append(args, "--error-format", "json")...)
		reqs := previewDoc(t, r.stdout)
		if len(reqs) != 3 || reqs[1]["url"] != mutTestURL+"/api/v1/apps/app-1/resource_types/"+newResourceTypeIDPlaceholder+"/resources" {
			t.Errorf("preview = %s", r.stdout)
		}
		var kinds []string
		for _, o := range r.stderrObjects(t) {
			if k, ok := o["kind"].(string); ok {
				kinds = append(kinds, k)
			}
		}
		if !strings.Contains(strings.Join(kinds, ","), noticePlaceholders) || strings.Contains(r.stdout, "[dry-run]") {
			t.Errorf("stderr kinds %v, stdout %q; want a placeholders notice on stderr only", kinds, r.stdout)
		}
	})
}

// The policy check stays a pre-send read: skipped on a dry run, and a failure
// sends nothing.
func TestCoreEntitlementsUpdatePolicyCheck(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{status: 404, body: `{"code":5}`}
		}
		return mutReply{body: `{}`}
	})
	t.Setenv("C1I_URL", mutTestURL)
	args := []string{"entitlements", "update", "e-1", "--app-id", "app-1", "--grant-policy-id", "ghost"}
	r := runMutationCLI(t, s.srv, 0, nil, append(args, "--error-format", "json")...)
	if r.exit != exitUsage || r.errorObject(t)["applied"] != false {
		t.Errorf("exit %d stderr %s, want 2 and applied:false", r.exit, r.stderr)
	}
	for _, q := range s.requests() {
		if q.method != http.MethodGet {
			t.Errorf("sent %s %s after a failed policy check", q.method, q.rawPath)
		}
	}

	withDryRun(t)
	r = runMutationCLI(t, nil, 0, nil, args...)
	if r.err != nil || !strings.Contains(r.stdout, `"grantPolicyId": "ghost"`) {
		t.Errorf("dry run: exit %d stdout %q, want a preview with no lookup", r.exit, r.stdout)
	}
}

// R20: add-owner --wait polls inside the helper; its progress goes to stderr.
func TestCoreEntitlementsAddOwnerWait(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if strings.HasSuffix(r.URL.Path, "/ownerids") {
			return mutReply{body: `{"userIds":["u1"]}`}
		}
		return mutReply{body: `{}`}
	})
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, s.srv, 0, nil, "entitlements", "add-owner", "u1", "--app-id", "app-1", "--entitlement-id", "e-1", "--wait", "--error-format", "json")
	objs := r.stderrObjects(t)
	if r.err != nil || r.stdout != "{}\n" || len(objs) != 1 || objs[0]["kind"] != noticeWaitDone {
		t.Errorf("exit %d stdout %q stderr %q; want {} and one waitDone notice", r.exit, r.stdout, r.stderr)
	}
}
