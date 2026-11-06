package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// policyBindingServer serves GET /api/v1/policies/{id} from policies
// (id -> policyType, "deleted:<type>" for a soft-deleted one; absent -> 404)
// and records every request.
func policyBindingServer(t *testing.T, policies map[string]string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var reqs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, r.Method+" "+r.URL.Path+" "+string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if id, ok := strings.CutPrefix(r.URL.Path, "/api/v1/policies/"); ok && r.Method == http.MethodGet {
			typ, found := policies[id]
			if !found {
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprint(w, `{"code":5,"message":"not found"}`)
				return
			}
			if typ == "nopolicy" {
				_, _ = fmt.Fprint(w, `{}`)
				return
			}
			deleted := "null"
			if rest, isDel := strings.CutPrefix(typ, "deleted:"); isDel {
				typ, deleted = rest, `"2026-01-01T00:00:00Z"`
			}
			_, _ = fmt.Fprintf(w, `{"policy":{"id":%q,"policyType":%q,"deletedAt":%s}}`, id, typ, deleted)
			return
		}
		_, _ = fmt.Fprint(w, `{"app":{"id":"x"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), reqs...)
	}
}

func countRequests(reqs []string, prefix string) int {
	n := 0
	for _, r := range reqs {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

// TestPolicyBindingValidation drives apps update/create through the command
// tree and pins which lookups happen and whether the mutation is sent.
func TestPolicyBindingValidation(t *testing.T) {
	policies := map[string]string{
		"gp": "POLICY_TYPE_GRANT", "rp": "POLICY_TYPE_REVOKE", "cp": "POLICY_TYPE_CERTIFY",
		"del": "deleted:POLICY_TYPE_GRANT", "np": "nopolicy",
	}
	cases := []struct {
		name        string
		args        []string
		dryRun      bool
		wantErr     string // "" means success
		wantLookups int
		wantPosts   int
		wantExit    int    // 0 means exitUsage when wantErr is set
		wantReq     string // substring some request must contain
	}{
		{name: "update 404", args: []string{"apps", "update", "app-1", "--grant-policy-id", "nope"},
			wantErr: `--grant-policy-id "nope": no such policy`, wantLookups: 1},
		{name: "update wrong type", args: []string{"apps", "update", "app-1", "--grant-policy-id", "rp"},
			wantErr: `--grant-policy-id "rp" is a POLICY_TYPE_REVOKE policy; it must be POLICY_TYPE_GRANT`, wantLookups: 1},
		{name: "update deleted", args: []string{"apps", "update", "app-1", "--grant-policy-id", "del"},
			wantErr: "policy is deleted", wantLookups: 1},
		{name: "update right types", args: []string{"apps", "update", "app-1", "--grant-policy-id", "gp", "--revoke-policy-id", "rp", "--certify-policy-id", "cp"},
			wantLookups: 3, wantPosts: 1},
		{name: "update unbind not looked up", args: []string{"apps", "update", "app-1", "--unbind-policy", "grant", "--certify-policy-id", "cp"},
			wantLookups: 1, wantPosts: 1},
		{name: "update duplicate id looked up once", args: []string{"apps", "update", "app-1", "--grant-policy-id", "gp", "--revoke-policy-id", "gp"},
			wantErr: `--revoke-policy-id "gp" is a POLICY_TYPE_GRANT policy`, wantLookups: 1},
		{name: "update 200 without policy is a C1 fault", args: []string{"apps", "update", "app-1", "--grant-policy-id", "np"},
			wantErr: "no policy", wantExit: exitServer, wantLookups: 1},
		{name: "update trims policy id", args: []string{"apps", "update", "app-1", "--grant-policy-id", " gp "},
			wantLookups: 1, wantPosts: 1, wantReq: `"grantPolicyId":"gp"`},
		{name: "create trims policy id", args: []string{"apps", "create", "--display-name", "a", "--grant-policy-id", "\tgp\n"},
			wantLookups: 1, wantPosts: 1, wantReq: `"grantPolicyId":"gp"`},
		{name: "update dry run sends nothing", args: []string{"apps", "update", "app-1", "--grant-policy-id", "nope"}, dryRun: true},
		{name: "create 404", args: []string{"apps", "create", "--display-name", "a", "--revoke-policy-id", "nope"},
			wantErr: "no such policy", wantLookups: 1},
		{name: "create wrong type", args: []string{"apps", "create", "--display-name", "a", "--certify-policy-id", "gp"},
			wantErr: "it must be POLICY_TYPE_CERTIFY", wantLookups: 1},
		{name: "create right type", args: []string{"apps", "create", "--display-name", "a", "--grant-policy-id", "gp"},
			wantLookups: 1, wantPosts: 1},
		{name: "create dry run sends nothing", args: []string{"apps", "create", "--display-name", "a", "--grant-policy-id", "nope"}, dryRun: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, requests := policyBindingServer(t, policies)
			resetCmds(t, appsUpdateCmd, appsCreateCmd)
			stubCatalogClient(t, srv)
			withCatalogDryRun(t, tc.dryRun)
			t.Setenv("C1I_URL", "https://example.invalid")

			err := runRootWithArgs(t, tc.args)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("run: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
				}
				want := tc.wantExit
				if want == 0 {
					want = exitUsage
				}
				if got := exitCode(err); got != want {
					t.Errorf("exit = %d, want %d", got, want)
				}
			}
			reqs := requests()
			if got := countRequests(reqs, "GET /api/v1/policies/"); got != tc.wantLookups {
				t.Errorf("policy lookups = %d, want %d (%v)", got, tc.wantLookups, reqs)
			}
			if got := countRequests(reqs, "POST "); got != tc.wantPosts {
				t.Errorf("mutations = %d, want %d (%v)", got, tc.wantPosts, reqs)
			}
			if tc.wantReq != "" && !strings.Contains(strings.Join(reqs, "\n"), tc.wantReq) {
				t.Errorf("no request contains %s: %v", tc.wantReq, reqs)
			}
		})
	}
}

// TestPolicyBindingServerErrorKeepsClassification pins that a non-404 lookup
// failure is not flattened to a usage error.
func TestPolicyBindingServerErrorKeepsClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("mutation sent after a failed lookup: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	resetCmds(t, appsUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")

	err := runRootWithArgs(t, []string{"apps", "update", "app-1", "--grant-policy-id", "gp"})
	if got := exitCode(err); got != exitAuth {
		t.Errorf("exit = %d (%v), want %d", got, err, exitAuth)
	}
}

// TestPolicyUnbindAndEmptyID pins explicit unbinding: an empty policy id is a
// usage error, --unbind-policy sends "" with the mask and does no lookup.
func TestPolicyUnbindAndEmptyID(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantErr  string
		wantBody string // the exact POST body
	}{
		{name: "update empty grant id", args: []string{"apps", "update", "app-1", "--grant-policy-id", ""}, wantErr: "--grant-policy-id requires a non-empty"},
		{name: "update whitespace revoke id", args: []string{"apps", "update", "app-1", "--revoke-policy-id", "  "}, wantErr: "--revoke-policy-id requires a non-empty"},
		{name: "create empty certify id", args: []string{"apps", "create", "--display-name", "a", "--certify-policy-id", ""}, wantErr: "--certify-policy-id requires a non-empty"},
		{name: "unbind grant", args: []string{"apps", "update", "app-1", "--unbind-policy", "grant"},
			wantBody: `{"app":{"grantPolicyId":"","id":"app-1"},"updateMask":"grantPolicyId"}`},
		{name: "unbind revoke", args: []string{"apps", "update", "app-1", "--unbind-policy", "REVOKE"},
			wantBody: `{"app":{"id":"app-1","revokePolicyId":""},"updateMask":"revokePolicyId"}`},
		{name: "unbind certify and grant", args: []string{"apps", "update", "app-1", "--unbind-policy", "certify", "--unbind-policy", "grant"},
			wantBody: `{"app":{"certifyPolicyId":"","grantPolicyId":"","id":"app-1"},"updateMask":"grantPolicyId,certifyPolicyId"}`},
		{name: "unknown kind", args: []string{"apps", "update", "app-1", "--unbind-policy", "emergency"}, wantErr: `invalid --unbind-policy "emergency"`},
		{name: "empty kind", args: []string{"apps", "update", "app-1", "--unbind-policy", ""}, wantErr: "--unbind-policy requires a non-empty"},
		{name: "set and unbind same kind", args: []string{"apps", "update", "app-1", "--grant-policy-id", "gp", "--unbind-policy", "grant"},
			wantErr: "--grant-policy-id and --unbind-policy grant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var bodies []string
			var lookups int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					lookups++
				} else {
					b, _ := io.ReadAll(r.Body)
					bodies = append(bodies, string(b))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"app":{"id":"x"}}`)
			}))
			defer srv.Close()
			resetCmds(t, appsUpdateCmd, appsCreateCmd)
			stubCatalogClient(t, srv)
			withCatalogDryRun(t, false)
			t.Setenv("C1I_URL", "https://example.invalid")

			err := runRootWithArgs(t, tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || exitCode(err) != exitUsage {
					t.Fatalf("err = %v (exit %d), want exit 2 containing %q", err, exitCode(err), tc.wantErr)
				}
				if len(bodies)+lookups != 0 {
					t.Errorf("requests sent before a usage error: %d lookups, bodies %v", lookups, bodies)
				}
				return
			}
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if lookups != 0 {
				t.Errorf("unbind did %d policy lookups, want 0", lookups)
			}
			if len(bodies) != 1 || bodies[0] != tc.wantBody {
				t.Errorf("bodies = %v, want [%s]", bodies, tc.wantBody)
			}
		})
	}
}
