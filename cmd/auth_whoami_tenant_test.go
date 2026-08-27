package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// defaultIntrospectBody is the shape the live endpoint returns: no "tenant"
// key of its own, but a "tenantId" that must survive untouched.
const defaultIntrospectBody = `{"userId":"u1","principleId":"p1","tenantId":"t1","roles":["r"],"permissions":[],"features":[]}`

// stubWhoamiServer answers introspect (and the follow-up user lookup) with the
// default payload, and points newWhoamiClient at it. status is the code the
// introspect call answers with.
func stubWhoamiServer(t *testing.T, status int) {
	t.Helper()
	stubWhoamiServerBody(t, status, defaultIntrospectBody)
}

// stubWhoamiServerBody is stubWhoamiServer with the introspect body chosen by
// the caller, for the degenerate bodies a 200 can still carry.
func stubWhoamiServerBody(t *testing.T, status int, introspectBody string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/v1/users/") {
			_, _ = w.Write([]byte(`{"userView":{"user":{"displayName":"Ada","email":"ada@example.invalid"}}}`))
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"denied"}`))
			return
		}
		_, _ = w.Write([]byte(introspectBody))
	}))
	t.Cleanup(srv.Close)

	orig := newWhoamiClient
	newWhoamiClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newWhoamiClient = orig })
}

// runWhoami executes `auth whoami` with args and returns stdout plus the
// command error, isolating the flag state each case sets up.
func runWhoami(t *testing.T, args []string) (string, error) {
	t.Helper()
	resetRootURLFlag(t)
	resetCmdFlags(t, authWhoamiCmd)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	rootCmd.SetArgs(append([]string{"auth", "whoami"}, args...))
	err := rootCmd.ExecuteContext(context.Background())
	return out.String(), err
}

// TestWhoamiReportsResolvedTenant is the point of the feature: the tenant a
// write would land on must be readable from JSON, not only from `auth
// status`'s prose or the stderr warning that fires only when --url is
// omitted. tenantSource names the resolution path, so an agent can tell an
// explicit --url from a silent ~/.c1i.yaml fall-through.
func TestWhoamiReportsResolvedTenant(t *testing.T) {
	cases := []struct {
		name       string
		setup      func(t *testing.T)
		args       []string
		wantTenant string
		wantSource string
	}{
		{
			name:       "flag",
			setup:      func(t *testing.T) { t.Setenv("C1I_URL", "") },
			args:       []string{"--url", "https://acme.conductor.one"},
			wantTenant: "https://acme.conductor.one",
			wantSource: "flag",
		},
		{
			name:       "env",
			setup:      func(t *testing.T) { t.Setenv("C1I_URL", "https://acme-env.conductor.one") },
			wantTenant: "https://acme-env.conductor.one",
			wantSource: "env",
		},
		{
			name: "config",
			setup: func(t *testing.T) {
				t.Setenv("C1I_URL", "")
				orig := viper.GetString("url")
				viper.Set("url", "https://acme-config.conductor.one")
				t.Cleanup(func() { viper.Set("url", orig) })
			},
			wantTenant: "https://acme-config.conductor.one",
			wantSource: "config",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			stubWhoamiServer(t, http.StatusOK)

			out, err := runWhoami(t, tc.args)
			if err != nil {
				t.Fatalf("auth whoami: %v (output %q)", err, out)
			}
			var got map[string]any
			if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
				t.Fatalf("output is not JSON: %v (%q)", uerr, out)
			}
			if got["tenant"] != tc.wantTenant {
				t.Errorf("tenant = %v, want %q", got["tenant"], tc.wantTenant)
			}
			if got["tenantSource"] != tc.wantSource {
				t.Errorf("tenantSource = %v, want %q", got["tenantSource"], tc.wantSource)
			}
			if got["userId"] != "u1" {
				t.Errorf("userId = %v, want u1 (the identity summary must survive)", got["userId"])
			}
		})
	}
}

// TestWhoamiVerboseReportsResolvedTenant pins that --verbose carries the same
// two keys: `--fields tenant` must not depend on whether --verbose was
// passed. The server's own tenantId is a different fact (an id, not a host)
// and must still come through untouched.
func TestWhoamiVerboseReportsResolvedTenant(t *testing.T) {
	t.Setenv("C1I_URL", "")
	stubWhoamiServer(t, http.StatusOK)

	out, err := runWhoami(t, []string{"--url", "https://acme.conductor.one", "--verbose"})
	if err != nil {
		t.Fatalf("auth whoami --verbose: %v (output %q)", err, out)
	}
	var got map[string]any
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("output is not JSON: %v (%q)", uerr, out)
	}
	if got["tenant"] != "https://acme.conductor.one" {
		t.Errorf("tenant = %v, want the resolved base URL", got["tenant"])
	}
	if got["tenantSource"] != "flag" {
		t.Errorf("tenantSource = %v, want flag", got["tenantSource"])
	}
	if got["tenantId"] != "t1" {
		t.Errorf("tenantId = %v, want t1 (the payload's own key must survive)", got["tenantId"])
	}
}

// TestWhoamiTenantSurvivesFieldsProjection drives the exact guardrail an
// agent runs before a write. `--fields tenant` must yield the tenant and exit
// 0 — if the key were missing, writeObject would classify it as a zero-match
// usage error instead.
func TestWhoamiTenantSurvivesFieldsProjection(t *testing.T) {
	t.Setenv("C1I_URL", "")
	stubWhoamiServer(t, http.StatusOK)

	orig := viper.GetString("fields")
	viper.Set("fields", "tenant")
	t.Cleanup(func() { viper.Set("fields", orig) })

	out, err := runWhoami(t, []string{"--url", "https://acme.conductor.one"})
	if err != nil {
		t.Fatalf("auth whoami --fields tenant: %v (output %q)", err, out)
	}
	var got map[string]any
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("output is not JSON: %v (%q)", uerr, out)
	}
	if len(got) != 1 || got["tenant"] != "https://acme.conductor.one" {
		t.Errorf("projected output = %v, want exactly {tenant: https://acme.conductor.one}", got)
	}
}

// TestWhoamiReportsNoTenantWhenUnauthenticated pins the fail-closed half of
// the contract: the tenant is only reported once the credentials are proven
// against it, so a 401 exits 3 with no tenant rather than naming a target the
// caller cannot actually reach.
func TestWhoamiReportsNoTenantWhenUnauthenticated(t *testing.T) {
	t.Setenv("C1I_URL", "")
	stubWhoamiServer(t, http.StatusUnauthorized)

	out, err := runWhoami(t, []string{"--url", "https://acme.conductor.one"})
	if err == nil {
		t.Fatalf("expected an error for a 401 introspect, got nil (output %q)", out)
	}
	if got, want := exitCode(err), exitAuth; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitAuth)", err, got, want)
	}
	if strings.Contains(out, `"tenant"`) {
		t.Errorf("stdout = %q, an unauthenticated whoami must not report a tenant", out)
	}
}

// TestWhoamiUnusableIntrospectBodyIsC1Failure walks the degenerate bodies a
// 200 can still carry. The invariant is a usable identity, not a non-nil map:
// `null` leaves the payload nil (writing the tenant into it panics), `{}`
// leaves it empty and non-nil, and an all-null identity leaves the keys
// present but useless — all three produced, or would have produced, a tenant
// reported next to a null identity with exit 0, which is a guardrail
// confirming a target off a response that proves nothing. A body that isn't a
// JSON object at all belongs in the same bucket rather than the generic exit
// 1 a bare fmt.Errorf gives. All of it is the remote failing its JSON
// contract: exitServer, in both output modes.
func TestWhoamiUnusableIntrospectBodyIsC1Failure(t *testing.T) {
	bodies := map[string]string{
		"null":            `null`,
		"empty object":    `{}`,
		"null identity":   `{"userId":null,"principleId":null,"roles":[]}`,
		"empty identity":  `{"userId":"","principleId":""}`,
		"json array":      `[]`,
		"json scalar":     `0`,
		"truncated":       `{"userId":"u1"`,
		"empty body":      ``,
		"whitespace body": "  \n",
	}
	for name, body := range bodies {
		for _, args := range [][]string{
			{"--url", "https://acme.conductor.one"},
			{"--url", "https://acme.conductor.one", "--verbose"},
		} {
			t.Run(name+" "+strings.Join(args, " "), func(t *testing.T) {
				t.Setenv("C1I_URL", "")
				stubWhoamiServerBody(t, http.StatusOK, body)

				out, err := runWhoami(t, args)
				if err == nil {
					t.Fatalf("expected an error for introspect body %q, got nil (output %q)", body, out)
				}
				if got, want := exitCode(err), exitServer; got != want {
					t.Errorf("body %q: exitCode(%v) = %d, want %d (exitServer)", body, err, got, want)
				}
				if strings.Contains(out, `"tenant"`) {
					t.Errorf("stdout = %q, an unusable introspect body must not report a tenant", out)
				}
			})
		}
	}
}

// TestWhoamiServicePrincipalWithoutUserIDSucceeds is the other side of that
// guard, and the more dangerous direction to get wrong: a principal with a
// principleId but no userId is a legitimate caller (a service principal), and
// rejecting it would break whoami for callers it works for today — a worse
// failure than the degenerate body the guard exists to catch. The identity
// enrichment is skipped without a userId, so displayName/email are absent;
// the tenant keys must still be reported.
func TestWhoamiServicePrincipalWithoutUserIDSucceeds(t *testing.T) {
	t.Setenv("C1I_URL", "")
	stubWhoamiServerBody(t, http.StatusOK,
		`{"principleId":"service:svc1","tenantId":"t1","roles":[],"permissions":[],"features":[]}`)

	out, err := runWhoami(t, []string{"--url", "https://acme.conductor.one"})
	if err != nil {
		t.Fatalf("auth whoami for a service principal: %v (output %q)", err, out)
	}
	var got map[string]any
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("output is not JSON: %v (%q)", uerr, out)
	}
	if got["principleId"] != "service:svc1" {
		t.Errorf("principleId = %v, want service:svc1", got["principleId"])
	}
	if got["tenant"] != "https://acme.conductor.one" {
		t.Errorf("tenant = %v, want the resolved base URL", got["tenant"])
	}
}

// TestWhoamiVerboseTenantIsClientResolved pins the documented precedence: if
// the payload ever grows a "tenant" key of its own, the client-resolved base
// URL still wins, because `--fields tenant` is a pre-write guardrail and must
// mean the same thing in every mode. The server's value would be a different
// fact under the same name; tenantId (a real payload key) is unaffected.
func TestWhoamiVerboseTenantIsClientResolved(t *testing.T) {
	t.Setenv("C1I_URL", "")
	stubWhoamiServerBody(t, http.StatusOK,
		`{"userId":"u1","principleId":"p1","tenantId":"t1","tenant":{"name":"acme"},"roles":[],"permissions":[],"features":[]}`)

	out, err := runWhoami(t, []string{"--url", "https://acme.conductor.one", "--verbose"})
	if err != nil {
		t.Fatalf("auth whoami --verbose: %v (output %q)", err, out)
	}
	var got map[string]any
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("output is not JSON: %v (%q)", uerr, out)
	}
	if got["tenant"] != "https://acme.conductor.one" {
		t.Errorf("tenant = %v, want the client-resolved base URL to win", got["tenant"])
	}
	if got["tenantId"] != "t1" {
		t.Errorf("tenantId = %v, want t1", got["tenantId"])
	}
}

// TestURLSourceTokenIsStable guards the machine-readable identifiers against
// being reworded the way the human-facing urlSourceLabel strings can be: they
// are a parsed value, so a prose label must never leak into tenantSource.
func TestURLSourceTokenIsStable(t *testing.T) {
	want := map[URLSource]string{
		URLSourceFlag:   "flag",
		URLSourceEnv:    "env",
		URLSourceConfig: "config",
		URLSourceNone:   "unknown",
	}
	for source, token := range want {
		got := urlSourceToken(source)
		if got != token {
			t.Errorf("urlSourceToken(%d) = %q, want %q", source, got, token)
		}
		if strings.ContainsAny(got, " -~") {
			t.Errorf("urlSourceToken(%d) = %q, want a bare identifier, not prose like urlSourceLabel's", source, got)
		}
	}
}
