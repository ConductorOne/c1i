package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	tblTrust         = `{"trust":{"clientId":"t-1@tenant.example.com/wfe","servicePrincipalId":"sp-1","providerId":"pr-1","displayName":"zz"}}`
	tblTrustDoc      = `{"clientId":"t-1@tenant.example.com/wfe","servicePrincipalId":"sp-1","providerId":"pr-1","displayName":"zz"}`
	tblWFProvider    = `{"provider":{"id":"pr-1","displayName":"zz","oidc":{}}}`
	tblWFProviderDoc = `{"id":"pr-1","displayName":"zz","oidc":{}}`
)

// runWF runs a workload-federation or trusts command against srv, every
// client seam stubbed.
func runWF(t *testing.T, s *mutServer, args ...string) mutResult {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	return runCLI(t, func() {
		stubMutationClient(t, s.srv, 0)
		stubServicePrincipalsClient(t, s.srv)
		stubListClient(t, s.srv)
	}, nil, args...)
}

func TestSPTrustPathSendsLocalPart(t *testing.T) {
	for _, id := range []string{"word-word-123", "word-word-123@tenant.example.com/wfe"} {
		if got, want := spTrustPath("sp-1", id), "/api/v1/service_principals/sp-1/trusts/word-word-123"; got != want {
			t.Errorf("spTrustPath(%q) = %q, want %q", id, got, want)
		}
	}
	// "@..." leaves an empty segment, which the client refuses (exit 2).
	s := newMutServer(t, fixedReply(200, tblTrust))
	r := runWF(t, s, "service-principals", "trusts", "get", "@tenant.example.com/wfe", "--service-principal-id", "sp-1")
	if r.exit != exitUsage || len(s.requests()) != 0 {
		t.Errorf("exit %d after %d requests, want 2 and none", r.exit, len(s.requests()))
	}
}

func TestSPTrustsGetAcceptsBothIDForms(t *testing.T) {
	for _, id := range []string{"t-1", "t-1@tenant.example.com/wfe"} {
		s := newMutServer(t, fixedReply(200, tblTrust))
		r := runWF(t, s, "service-principals", "trusts", "get", id, "--service-principal-id", "sp-1")
		if r.exit != 0 {
			t.Fatalf("%s: exit %d: %s", id, r.exit, r.stderr)
		}
		if got := s.requests()[0].rawPath; got != "/api/v1/service_principals/sp-1/trusts/t-1" {
			t.Errorf("%s: path %s", id, got)
		}
		if r.stdout != indentJSON(t, tblTrustDoc) {
			t.Errorf("%s: stdout %q", id, r.stdout)
		}
	}
}

// The API stores a trust under an unknown service principal or with an
// unknown scoped role, so create looks both up first, on --dry-run too.
func TestSPTrustsCreateRefusesUnknownRefs(t *testing.T) {
	base := []string{"service-principals", "trusts", "create", "sp-1", "--provider-id", "pr-1", "--display-name", "zz", "--condition", "true"}
	for _, tc := range []struct {
		name     string
		args     []string
		missing  string // GET path that 404s
		status   int
		wantExit int
	}{
		{"unknown sp", nil, "/api/v1/service_principals/sp-1", 404, exitNotFound},
		{"unknown sp dry-run", []string{"--dry-run"}, "/api/v1/service_principals/sp-1", 404, exitNotFound},
		{"unknown role", []string{"--scoped-role", "r-1", "--scoped-role", "r-2"}, "/api/v1/iam/roles/r-2", 404, exitNotFound},
		{"unknown role dry-run", []string{"--scoped-role", "r-2", "--dry-run"}, "/api/v1/iam/roles/r-2", 404, exitNotFound},
		{"human user id (API 500)", nil, "/api/v1/service_principals/sp-1", 500, exitServer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, func(r *http.Request, _ int) mutReply {
				if r.URL.Path == tc.missing {
					return mutReply{status: tc.status, body: `{"code":5,"message":"not found"}`}
				}
				if r.Method == http.MethodGet {
					return mutReply{body: `{"servicePrincipal":{"id":"sp-1"}}`}
				}
				return mutReply{body: tblTrust}
			})
			r := runWF(t, s, append(append([]string{}, base...), tc.args...)...)
			if r.exit != tc.wantExit {
				t.Errorf("exit %d, want %d: %s", r.exit, tc.wantExit, r.stderr)
			}
			if w := writesIn(s.requests()); len(w) != 0 {
				t.Errorf("sent %v, want no write", w)
			}
		})
	}
}

func TestSPTrustsCreateSendsBody(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{body: `{}`}
		}
		return mutReply{body: tblTrust}
	})
	r := runWF(t, s, "service-principals", "trusts", "create", "sp-1", "--provider-id", "pr-1", "--display-name", "zz",
		"--condition", `claims.sub == "x"`, "--scoped-role", "r-1", "--allow-cidr", "10.0.0.0/24", "--passthrough-claim", "repository")
	if r.exit != 0 {
		t.Fatalf("exit %d: %s", r.exit, r.stderr)
	}
	w := writesIn(s.requests())
	if len(w) != 1 || w[0].rawPath != "/api/v1/service_principals/sp-1/trusts" {
		t.Fatalf("writes %v", w)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(w[0].body), &got)
	want := `{"allowSourceCidrs":["10.0.0.0/24"],"conditionExpression":"claims.sub == \"x\"","displayName":"zz","passthroughClaims":["repository"],"providerId":"pr-1","scopedRoleIds":["r-1"]}`
	if b, _ := json.Marshal(got); string(b) != want {
		t.Errorf("body %s, want %s", b, want)
	}
}

func TestSPTrustsUpdateChecksScopedRoles(t *testing.T) {
	for _, args := range [][]string{
		{"--scoped-role", "r-9"},
		{"--body-file", writeTempFile(t, `{"scopedRoleIds":["r-9"]}`), "--update-mask", "scopedRoleIds"},
		{"--body-file", writeTempFile(t, `{"scoped_role_ids":["r-9"]}`), "--update-mask", "scoped_role_ids"},
		{"--body-file", writeTempFile(t, `{"scoped_role_ids":["r-9"]}`), "--update-mask", "scopedRoleIds", "--dry-run"},
	} {
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if r.Method == http.MethodGet {
				return mutReply{status: 404, body: `{"code":5}`}
			}
			return mutReply{body: tblTrust}
		})
		r := runWF(t, s, append([]string{"service-principals", "trusts", "update", "t-1", "--service-principal-id", "sp-1"}, args...)...)
		if r.exit != exitNotFound || len(writesIn(s.requests())) != 0 {
			t.Errorf("%v: exit %d, writes %v; want 4 and none", args, r.exit, writesIn(s.requests()))
		}
	}
}

func TestSPTrustsUpdateBodyFileClearsList(t *testing.T) {
	s := newMutServer(t, fixedReply(200, tblTrust))
	r := runWF(t, s, "service-principals", "trusts", "update", "t-1", "--service-principal-id", "sp-1",
		"--body-file", writeTempFile(t, `{"scopedRoleIds":[]}`), "--update-mask", "scopedRoleIds")
	if r.exit != 0 {
		t.Fatalf("exit %d: %s", r.exit, r.stderr)
	}
	w := s.requests()
	if len(w) != 1 || w[0].method != http.MethodPatch || w[0].body != `{"trust":{"scopedRoleIds":[]},"updateMask":"scopedRoleIds"}` {
		t.Errorf("requests %v", w)
	}
}

const wfTokenReport = `{"overallResult":%s,"jwtDecode":{"stepName":"JWT decode","passed":true},"issuerMatch":{"stepName":"Issuer match","passed":%s},"cidrCheck":{"stepName":"CIDR check","passed":false,"skipped":true}}`

func TestSPTrustsTestTokenExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name, body, stderr string
		exit               int
	}{
		{"pass", strings.Replace(strings.Replace(wfTokenReport, "%s", "true", 1), "%s", "true", 1), "", 0},
		{"reject", strings.Replace(strings.Replace(wfTokenReport, "%s", "false", 1), "%s", "false", 1), "failed steps: Issuer match", exitError},
		{"no overallResult", `{"jwtDecode":{}}`, "overallResult", exitServer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, tc.body))
			restoreCmdWriters(t)
			spTrustsTestTokenCmd.SetIn(strings.NewReader("  header.payload.sig \n"))
			t.Cleanup(func() { spTrustsTestTokenCmd.SetIn(nil) })
			r := runWF(t, s, "service-principals", "trusts", "test-token", "t-1@tenant.example.com/wfe", "--service-principal-id", "sp-1", "--subject-token-file", "-", "--source-ip", "10.0.0.5")
			if r.exit != tc.exit || !strings.Contains(r.stderr, tc.stderr) {
				t.Errorf("exit %d stderr %q, want %d containing %q", r.exit, r.stderr, tc.exit, tc.stderr)
			}
			if tc.exit != exitServer && r.stdout != indentJSON(t, tc.body) {
				t.Errorf("stdout %q, want the report", r.stdout)
			}
			req := s.requests()[0]
			if req.rawPath != "/api/v1/service_principals/sp-1/trusts/t-1/test" || req.body != `{"sourceIp":"10.0.0.5","subjectToken":"header.payload.sig"}` {
				t.Errorf("request %+v", req)
			}
		})
	}
}

func TestSPTrustsTestTokenRefusesBlankToken(t *testing.T) {
	s := newMutServer(t, fixedReply(200, `{}`))
	r := runWF(t, s, "service-principals", "trusts", "test-token", "t-1", "--service-principal-id", "sp-1", "--subject-token-file", writeTempFile(t, " \n"))
	if r.exit != exitUsage || len(s.requests()) != 0 {
		t.Errorf("exit %d after %d requests, want 2 and none", r.exit, len(s.requests()))
	}
}

func TestWFTestCelExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		exit       int
	}{
		{"matched", `{"matched":true,"error":"","expression":"e"}`, 0},
		{"not matched", `{"matched":false,"error":"","expression":"e"}`, exitError},
		{"error", `{"matched":false,"error":"expression must return bool, got int","expression":"1"}`, exitUsage},
		{"unknown shape", `{"result":true}`, exitServer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, tc.body))
			r := runWF(t, s, "workload-federation", "test-cel", "--expression", "e", "--claims", `{"sub":"x"}`)
			if r.exit != tc.exit {
				t.Errorf("exit %d, want %d: %s", r.exit, tc.exit, r.stderr)
			}
			if tc.exit != exitServer && r.stdout != indentJSON(t, tc.body) {
				t.Errorf("stdout %q, want the answer first", r.stdout)
			}
			if got := s.requests()[0].body; got != `{"claimsJson":"{\"sub\":\"x\"}","expression":"e"}` {
				t.Errorf("body %s", got)
			}
		})
	}
}

func TestWFTestCelClaimsInput(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--claims", "{}", "--claims-file", writeTempFile(t, "{}")},
	} {
		s := newMutServer(t, fixedReply(200, `{"matched":true}`))
		r := runWF(t, s, append([]string{"workload-federation", "test-cel", "--expression", "e"}, args...)...)
		if r.exit != exitUsage || len(s.requests()) != 0 {
			t.Errorf("%v: exit %d after %d requests, want 2 and none", args, r.exit, len(s.requests()))
		}
	}
}

func TestWFProvidersCreateSettings(t *testing.T) {
	base := []string{"workload-federation", "providers", "create", "--display-name", "zz"}
	for _, tc := range []struct {
		name string
		args []string
		body string // "" = usage error, nothing sent
	}{
		{"oidc", []string{"--issuer-url", "https://issuer.example.com", "--well-known-provider", "github-actions"},
			`{"displayName":"zz","issuerUrl":"https://issuer.example.com","oidc":{},"wellKnownProvider":"WELL_KNOWN_WORKLOAD_PROVIDER_GITHUB_ACTIONS"}`},
		{"spiffe", []string{"--issuer-url", "spiffe://td.example.com", "--well-known-provider", "spiffe", "--bundle-endpoint-url", "https://td.example.com/keys"},
			`{"displayName":"zz","issuerUrl":"spiffe://td.example.com","spiffe":{"bundleEndpointUrl":"https://td.example.com/keys"},"wellKnownProvider":"WELL_KNOWN_WORKLOAD_PROVIDER_SPIFFE"}`},
		{"spiffe without bundle", []string{"--issuer-url", "spiffe://td.example.com", "--well-known-provider", "spiffe"}, ""},
		{"bundle on oidc", []string{"--issuer-url", "https://issuer.example.com", "--well-known-provider", "custom", "--bundle-endpoint-url", "https://x.example.com/keys"}, ""},
		{"c1-edge not offered", []string{"--issuer-url", "https://issuer.example.com", "--well-known-provider", "c1-edge"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, tblWFProvider))
			r := runWF(t, s, append(append([]string{}, base...), tc.args...)...)
			if tc.body == "" {
				if r.exit != exitUsage || len(s.requests()) != 0 {
					t.Errorf("exit %d after %d requests, want 2 and none", r.exit, len(s.requests()))
				}
				return
			}
			if r.exit != 0 || len(s.requests()) != 1 || s.requests()[0].body != tc.body {
				t.Errorf("exit %d requests %v, want body %s", r.exit, s.requests(), tc.body)
			}
		})
	}
}

func TestWFProvidersUpdateBundleMask(t *testing.T) {
	s := newMutServer(t, fixedReply(200, tblWFProvider))
	r := runWF(t, s, "workload-federation", "providers", "update", "pr-1", "--bundle-endpoint-url", "https://td.example.com/keys", "--disabled")
	if r.exit != 0 {
		t.Fatalf("exit %d: %s", r.exit, r.stderr)
	}
	if got, want := s.requests()[0].body, `{"provider":{"disabled":true,"spiffe":{"bundleEndpointUrl":"https://td.example.com/keys"}},"updateMask":"disabled,spiffe.bundleEndpointUrl"}`; got != want {
		t.Errorf("body %s, want %s", got, want)
	}
}

func TestWFListRowsKeepTypes(t *testing.T) {
	trust := `{"list":[{"clientId":"t-1@tenant.example.com/wfe","disabled":false,"scopedRoleIds":null,"allowSourceCidrs":["10.0.0.0/24"],"createdAt":null}],"nextPageToken":""}`
	s := newMutServer(t, fixedReply(200, trust))
	r := runWF(t, s, "service-principals", "trusts", "search", "--provider-id", "pr-1", "--query", "q")
	if r.exit != 0 {
		t.Fatalf("exit %d: %s", r.exit, r.stderr)
	}
	want := `{"allow_source_cidrs":["10.0.0.0/24"],"client_id":"t-1@tenant.example.com/wfe","condition_expression":null,"created_at":null,"description":null,"disabled":false,"display_name":null,"passthrough_claims":[],"provider_id":null,"scoped_role_ids":[],"service_principal_id":null,"updated_at":null}` + "\n"
	if r.stdout != want {
		t.Errorf("row %s, want %s", r.stdout, want)
	}
	req := s.requests()[0]
	if req.method != http.MethodPost || req.rawPath != spTrustsSearchPath || !strings.Contains(req.body, `"providerId":"pr-1"`) || !strings.Contains(req.body, `"query":"q"`) || strings.Contains(req.body, "servicePrincipalId") {
		t.Errorf("request %+v", req)
	}

	prov := `{"list":[{"id":"p1","wellKnownProvider":"WELL_KNOWN_WORKLOAD_PROVIDER_SPIFFE","disabled":true,"spiffe":{"bundleEndpointUrl":"https://td.example.com/keys"}},{"id":"p2","disabled":false,"oidc":{}}],"nextPageToken":""}`
	s = newMutServer(t, fixedReply(200, prov))
	r = runWF(t, s, "workload-federation", "providers", "list")
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if r.exit != 0 || len(lines) != 2 || !strings.Contains(lines[0], `"bundle_endpoint_url":"https://td.example.com/keys"`) || !strings.Contains(lines[0], `"disabled":true`) || strings.Contains(lines[1], "bundle_endpoint_url") {
		t.Errorf("exit %d rows:\n%s", r.exit, r.stdout)
	}
}

// A mask naming a list the body omits would make the API clear it; an empty
// scopedRoleIds widens the trust to the principal's roles.
func TestSPTrustsUpdateRefusesMaskedListTheBodyOmits(t *testing.T) {
	for _, tc := range []struct{ body, mask string }{
		{`{"displayName":"zz"}`, "displayName,scopedRoleIds"},
		{`{}`, "scoped_role_ids"},
		{`{"scopedRoleIds":null}`, "scopedRoleIds"},
		{`{}`, "trust.scopedRoleIds"},
		{`{}`, " allowSourceCidrs"},
		{`{}`, "passthrough_claims"},
	} {
		for _, dry := range []bool{false, true} {
			s := newMutServer(t, fixedReply(200, tblTrust))
			args := []string{"service-principals", "trusts", "update", "t-1", "--service-principal-id", "sp-1",
				"--body-file", writeTempFile(t, tc.body), "--update-mask", tc.mask}
			if dry {
				args = append(args, "--dry-run")
			}
			r := runWF(t, s, args...)
			if r.exit != exitUsage || len(s.requests()) != 0 || !strings.Contains(r.stderr, "explicit []") {
				t.Errorf("%s / %q dry=%v: exit %d after %d requests, stderr %q; want 2, none, naming the explicit []", tc.body, tc.mask, dry, r.exit, len(s.requests()), r.stderr)
			}
		}
	}
	// The snake_case clear is allowed too.
	s := newMutServer(t, fixedReply(200, tblTrust))
	r := runWF(t, s, "service-principals", "trusts", "update", "t-1", "--service-principal-id", "sp-1",
		"--body-file", writeTempFile(t, `{"allow_source_cidrs":[]}`), "--update-mask", "allow_source_cidrs")
	if r.exit != 0 || len(s.requests()) != 1 {
		t.Errorf("explicit [] clear: exit %d after %d requests: %s", r.exit, len(s.requests()), r.stderr)
	}
}
