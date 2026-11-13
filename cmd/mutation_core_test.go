package cmd

import (
	"net/http"
	"strings"
	"testing"
)

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

// The policy check is a pre-send read that runs on a dry run too, and a failure
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
	dry := newMutServer(t, func(*http.Request, int) mutReply { return mutReply{status: 404, body: `{"code":5}`} })
	r = runMutationCLI(t, dry.srv, 0, nil, args...)
	if r.exit != exitUsage || r.stdout != "" || len(dry.requests()) != 1 {
		t.Errorf("dry run: exit %d stdout %q, %d requests, want exit 2, no preview, one lookup", r.exit, r.stdout, len(dry.requests()))
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
	r := runMutationCLI(t, s.srv, 0, nil, "entitlements", "add-owner", "e-1", "--app-id", "app-1", "--user-id", "u1", "--wait", "--error-format", "json")
	objs := r.stderrObjects(t)
	if r.err != nil || r.stdout != "{}\n" || len(objs) != 1 || objs[0]["kind"] != noticeWaitDone {
		t.Errorf("exit %d stdout %q stderr %q; want {} and one waitDone notice", r.exit, r.stdout, r.stderr)
	}
}
