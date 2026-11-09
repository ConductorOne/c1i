package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestAppsMutationPreview(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			withDryRun(t)
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, nil, 0, nil, "apps", "set-owners", "app/1", "--user-id", "u1", "--wait", "--error-format", format)
			if r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
			want := wantPreview("PUT", "/api/v1/apps/app%2F1/owners", "{\n        \"userIds\": [\n          \"u1\"\n        ]\n      }")
			if r.stdout != want {
				t.Errorf("stdout =\n%s\nwant\n%s", r.stdout, want)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || len(doc) != 2 || doc["id"] != nil {
				t.Errorf("preview keys = %v, want exactly dryRun and requests", doc)
			}
			wantErr := "dry run: nothing was sent\n"
			if format == "json" {
				wantErr = `{"kind":"dryRun","notice":"dry run: nothing was sent"}` + "\n"
			}
			if r.stderr != wantErr {
				t.Errorf("stderr = %q, want %q", r.stderr, wantErr)
			}
		})
	}
	t.Run("no body on delete", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, nil, 0, nil, "apps", "delete", "app-1")
		if want := wantPreview("DELETE", "/api/v1/apps/app-1", ""); r.stdout != want {
			t.Errorf("stdout = %q, want %q", r.stdout, want)
		}
	})
}

func TestAppsMutationPreviewRefusals(t *testing.T) {
	cases := []struct {
		name string
		url  string
		args []string
	}{
		{"no URL", "", []string{"apps", "delete", "app-1"}},
		{"empty id", mutTestURL, []string{"apps", "delete", ""}},
		{"dot id", mutTestURL, []string{"apps", "delete", "."}},
		{"dot-dot id", mutTestURL, []string{"apps", "delete", ".."}},
		{"fields", mutTestURL, []string{"apps", "delete", "app-1", "--fields", "id"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDryRun(t)
			t.Setenv("C1I_URL", tc.url)
			if tc.url == "" {
				orig := viper.GetString("url")
				viper.Set("url", "")
				t.Cleanup(func() { viper.Set("url", orig) })
			}
			r := runMutationCLI(t, nil, 0, nil, append(tc.args, "--error-format", "json")...)
			if r.exit != exitUsage || r.stdout != "" {
				t.Errorf("exit %d stdout %q, want exit 2 and nothing on stdout", r.exit, r.stdout)
			}
			if got := r.errorObject(t)["applied"]; got != false {
				t.Errorf("applied = %v, want false", got)
			}
		})
	}
}

func TestAppsMutationRefusesFields(t *testing.T) {
	for _, fields := range []string{"id", ""} {
		t.Run("--fields="+fields, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, `{"app":{"id":"app-1"}}`))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, "apps", "create", "--display-name", "zz", "--fields", fields)
			if r.exit != exitUsage || len(s.requests()) != 0 {
				t.Errorf("exit %d, %d requests; want exit 2 and none sent", r.exit, len(s.requests()))
			}
		})
	}
	t.Run("C1I_FIELDS alone is ignored", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{"app":{"id":"app-1","displayName":"zz"}}`))
		t.Setenv("C1I_URL", mutTestURL)
		t.Setenv("C1I_FIELDS", "displayName")
		r := runMutationCLI(t, s.srv, 0, nil, "apps", "create", "--display-name", "zz")
		if r.err != nil || !strings.Contains(r.stdout, `"id": "app-1"`) {
			t.Errorf("exit %d stdout %q, want the full object", r.exit, r.stdout)
		}
	})
}

func TestAppsMutationApplied(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		route    func(*http.Request, int) mutReply
		retries  int
		noServer bool
		dead     bool // a closed port
		out      io.Writer
		exit     int
		applied  any
	}{
		{name: "404", args: []string{"apps", "delete", "app-1"}, route: fixedReply(404, `{"code":5}`), exit: exitNotFound, applied: false},
		{name: "401", args: []string{"apps", "delete", "app-1"}, route: fixedReply(401, ``), exit: exitAuth, applied: false},
		{name: "400", args: []string{"apps", "create", "--display-name", "zz"}, route: fixedReply(400, `{"code":3}`), exit: exitUsage, applied: false},
		{name: "429", args: []string{"apps", "create", "--display-name", "zz"}, route: fixedReply(429, ``), exit: exitRateLimited, applied: false},
		{name: "408", args: []string{"apps", "create", "--display-name", "zz"}, route: fixedReply(408, ``), exit: exitError, applied: "unknown"},
		{name: "499", args: []string{"apps", "create", "--display-name", "zz"}, route: fixedReply(499, ``), exit: exitError, applied: "unknown"},
		{name: "500 on POST", args: []string{"apps", "create", "--display-name", "zz"}, route: fixedReply(500, ``), exit: exitServer, applied: "unknown"},
		{name: "redirect", args: []string{"apps", "delete", "app-1"}, route: func(*http.Request, int) mutReply {
			return mutReply{status: http.StatusMovedPermanently}
		}, exit: exitUsage, applied: false},
		{name: "503 then 404 on DELETE", args: []string{"apps", "delete", "app-1"}, retries: 1, route: func(_ *http.Request, n int) mutReply {
			if n == 1 {
				return mutReply{status: 503}
			}
			return mutReply{status: 404}
		}, exit: exitNotFound, applied: "unknown"},
		{name: "dial error", args: []string{"apps", "delete", "app-1"}, dead: true, exit: exitError, applied: false},
		{name: "connection closed after headers", args: []string{"apps", "create", "--display-name", "zz"}, route: func(*http.Request, int) mutReply {
			return mutReply{hijack: true}
		}, exit: exitError, applied: "unknown"},
		{name: "non-JSON 2xx", args: []string{"apps", "create", "--display-name", "zz"}, route: fixedReply(200, `<html>`), exit: exitServer, applied: true},
		{name: "stdout write fails", args: []string{"apps", "delete", "app-1"}, route: fixedReply(200, `{}`), out: failingWriter{}, exit: exitError, applied: true},
		{name: "Args error", args: []string{"apps", "delete"}, noServer: true, exit: exitUsage, applied: false},
		{name: "requireNonEmpty", args: []string{"apps", "add-owner", "app-1", "--user-id", ""}, noServer: true, exit: exitUsage, applied: false},
		{name: "bad body file", args: []string{"apps", "update", "app-1", "--app-user-mapper-file", "/nonexistent/x.json"}, noServer: true, exit: exitUsage, applied: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			var srv *httptest.Server
			switch {
			case tc.dead:
				srv = closedPortServer(t)
			case !tc.noServer:
				srv = newMutServer(t, tc.route).srv
			}
			r := runMutationCLI(t, srv, tc.retries, tc.out, append(tc.args, "--error-format", "json")...)
			if r.exit != tc.exit {
				t.Errorf("exit = %d, want %d (%v)", r.exit, tc.exit, r.err)
			}
			if got := r.errorObject(t)["applied"]; got != tc.applied {
				t.Errorf("applied = %#v, want %#v; stderr %s", got, tc.applied, r.stderr)
			}
			if tc.out == nil && r.stdout != "" {
				t.Errorf("stdout = %q, want nothing", r.stdout)
			}
		})
	}
}

// R21: a failed stdout write after the change names the request.
func TestAppsMutationWriteFailureWording(t *testing.T) {
	s := newMutServer(t, fixedReply(200, `{}`))
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, s.srv, 0, failingWriter{}, "apps", "delete", "app-1")
	want := "DELETE /api/v1/apps/app-1 succeeded but writing its response failed: " + errStdoutFull.Error()
	if r.err == nil || r.err.Error() != want {
		t.Errorf("err = %v, want %q", r.err, want)
	}
}

func TestAppsSetOwnersWait(t *testing.T) {
	orig := ownerWaitInterval
	ownerWaitInterval = 5 * time.Millisecond
	t.Cleanup(func() { ownerWaitInterval = orig })
	t.Run("timeout", func(t *testing.T) {
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if strings.HasSuffix(r.URL.Path, "/ownerids") {
				return mutReply{body: `{"userIds":[]}`}
			}
			return mutReply{body: `{}`}
		})
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "apps", "set-owners", "app-1", "--user-id", "u1", "--wait", "--wait-timeout", "50ms", "--error-format", "json", "--debug")
		if r.exit != exitError || r.stdout != "{}\n" {
			t.Errorf("exit %d stdout %q, want 1 and the document", r.exit, r.stdout)
		}
		obj := r.errorObject(t)
		if obj["applied"] != true || !strings.HasPrefix(obj["error"].(string), "PUT /api/v1/apps/app-1/owners was applied; waiting for owners to provision on app app-1 failed: timed out") {
			t.Errorf("error object = %v", obj)
		}
		for _, o := range r.stderrObjects(t) {
			n := 0
			for _, k := range []string{"error", "notice", "progress", "debug"} {
				if _, ok := o[k]; ok {
					n++
				}
			}
			if n != 1 {
				t.Errorf("stderr object %v has %d of error/notice/progress/debug, want exactly 1", o, n)
			}
		}
	})
	t.Run("poll 404", func(t *testing.T) {
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if strings.HasSuffix(r.URL.Path, "/ownerids") {
				return mutReply{status: 404}
			}
			return mutReply{body: `{}`}
		})
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "apps", "set-owners", "app-1", "--user-id", "u1", "--wait", "--error-format", "json")
		if r.exit != exitNotFound || r.errorObject(t)["applied"] != true {
			t.Errorf("exit %d stderr %s, want 4 and applied:true", r.exit, r.stderr)
		}
	})
	t.Run("success lines on stderr", func(t *testing.T) {
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if strings.HasSuffix(r.URL.Path, "/ownerids") {
				return mutReply{body: `{"userIds":["u1"]}`}
			}
			return mutReply{body: `{}`}
		})
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "apps", "set-owners", "app-1", "--user-id", "u1", "--wait", "--error-format", "json")
		objs := r.stderrObjects(t)
		if r.err != nil || r.stdout != "{}\n" || len(objs) != 1 || objs[0]["kind"] != noticeWaitDone || objs[0]["subject"] != "owners to provision on app app-1" {
			t.Errorf("exit %d stdout %q stderr %q", r.exit, r.stdout, r.stderr)
		}
	})
}

// The pre-send reads (user and policy lookups) are skipped on a dry run and
// a failure is applied:false.
func TestAppsMutationPreReads(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{status: 404, body: `{"code":5}`}
		}
		return mutReply{body: `{}`}
	})
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, s.srv, 0, nil, "apps", "set-owners", "app-1", "--user-id", "ghost", "--error-format", "json")
	if r.exit != exitUsage || r.errorObject(t)["applied"] != false {
		t.Errorf("exit %d stderr %s, want 2 and applied:false", r.exit, r.stderr)
	}
	if got := s.requests(); len(got) != 1 || got[0].method != http.MethodGet {
		t.Errorf("requests = %+v, want only the user lookup", got)
	}

	withDryRun(t)
	r = runMutationCLI(t, nil, 0, nil, "apps", "create", "--display-name", "zz", "--grant-policy-id", "p1")
	if r.err != nil || !strings.Contains(r.stdout, `"grantPolicyId": "p1"`) {
		t.Errorf("dry run with a policy id: exit %d stdout %q", r.exit, r.stdout)
	}
}

func TestMarshalMutationBody(t *testing.T) {
	var nilMap map[string]any
	var nilSlice []string
	var nilPtr *struct{}
	cases := []struct {
		name    string
		body    any
		want    string
		wantErr bool
	}{
		{"untyped nil", nil, "", false},
		{"typed-nil map", nilMap, "{}", false},
		{"typed-nil slice", nilSlice, "[]", false},
		{"marshals to null", nilPtr, "", true},
		{"object", map[string]any{"a": 1}, `{"a":1}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := marshalMutationBody(mutation{Method: "POST", Path: "/x", Body: tc.body})
			if (err != nil) != tc.wantErr || string(got) != tc.want {
				t.Errorf("= %q, %v; want %q, err %v", got, err, tc.want, tc.wantErr)
			}
			if tc.name == "untyped nil" && got != nil {
				t.Error("untyped nil must mean no body")
			}
		})
	}
}

// Every apps mutation is annotated, so its pre-helper errors carry applied.
func TestAppsMutationsMarked(t *testing.T) {
	for _, c := range appsCmd.Commands() {
		mutating := !slices.Contains([]string{"get", "list", "owners"}, c.Name())
		if isMutating(c) != mutating {
			t.Errorf("apps %s: isMutating = %v, want %v", c.Name(), isMutating(c), mutating)
		}
	}
}
