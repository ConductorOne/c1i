package cmd

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// runListCLI is runMutationCLI plus the list-client seam: list commands build
// their client through newListClient, which only that seam substitutes.
func runListCLI(t *testing.T, srv *httptest.Server, args ...string) mutResult {
	t.Helper()
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		if srv == nil {
			t.Error("a list client was built")
			return nil, &client.AuthError{Err: io.EOF}
		}
		return client.NewForTesting(srv.URL, srv.Client(), client.WithMaxRetries(0)), nil
	}
	t.Cleanup(func() { newListClient = orig })
	return runMutationCLI(t, srv, 0, nil, args...)
}

// --- A. Body-carried "."/".." ids ---

// A policy id is carried in the create body, so the transport path guard never
// sees it. Both the preview and the real send must refuse it up front: without
// the pre-send check, `--dry-run` printed it as an id and only a real send
// failed, after the pre-read.
func TestDotSegmentBodyIDFlagRefused(t *testing.T) {
	for _, value := range []string{".", "..", "..;x", ".;", " . ", " ..;x "} {
		for _, dryRun := range []bool{true, false} {
			name := value
			if dryRun {
				name += " --dry-run"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				srv := newMutServer(t, fixedReply(200, `{"app":{"id":"app-1"}}`))
				if dryRun {
					withDryRun(t) // viper-level, so a leaked override cannot mask it
				}
				args := []string{"apps", "create", "--display-name", "x", "--grant-policy-id", value}
				res := runMutationCLI(t, srv.srv, 0, nil, args...)
				if res.exit != exitUsage {
					t.Fatalf("exit = %d, err = %v; want %d", res.exit, res.err, exitUsage)
				}
				if res.err == nil || !strings.Contains(res.err.Error(), "is not an id") {
					t.Errorf("err = %v, want the dot-segment id refusal", res.err)
				}
				if res.stdout != "" {
					t.Errorf("stdout = %q, want nothing written (no preview, no output)", res.stdout)
				}
				if got := srv.requests(); len(got) != 0 {
					t.Errorf("requests sent: %v", got)
				}
			})
		}
	}
}

// entitlements create is a sequence of up to three POSTs, so a bad --owner-id
// used to create the resource type and resource and only then fail on the
// owner step. The pre-send check has to leave the tenant untouched.
func TestDotSegmentRepeatableOwnerIDSendsNothing(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		name := "real"
		if dryRun {
			name = "dry-run"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, `{"appEntitlement":{"id":"ent-1"}}`))
			if dryRun {
				withDryRun(t)
			}
			args := []string{"entitlements", "create", "--app-id", "app-1", "--display-name", "x", "--owner-id", "."}
			res := runMutationCLI(t, srv.srv, 0, nil, args...)
			if res.exit != exitUsage {
				t.Fatalf("exit = %d, err = %v; want %d", res.exit, res.err, exitUsage)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("requests sent: %v (a partial apply must not happen)", got)
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want nothing written", res.stdout)
			}
		})
	}
}

// <function-id> on `functions usage` is compared client-side and never appears
// in a path; --template-id is body-carried too. Both are ids, so both must be
// refused before the request.
func TestDotSegmentBodyCarriedIDsRefused(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"functions usage dot", []string{"functions", "usage", "."}},
		{"functions usage dot-dot matrix", []string{"functions", "usage", "..;x"}},
		{"automations executions list template-id dot", []string{"automations", "executions", "list", "--template-id", "."}},
		{"automations executions list template-id dot-dot", []string{"automations", "executions", "list", "--template-id", ".."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, `{"list":[],"nextPageToken":""}`))
			res := runListCLI(t, srv.srv, tc.args...)
			if res.exit != exitUsage {
				t.Fatalf("exit = %d, err = %v; want %d", res.exit, res.err, exitUsage)
			}
			if res.err == nil || !strings.Contains(res.err.Error(), "is not an id") {
				t.Errorf("err = %v, want the dot-segment id refusal", res.err)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("requests sent: %v", got)
			}
		})
	}
}

// A dotted id is a real id: the refusal must match only the exact dot-segment
// forms, not any value containing a dot.
func TestDottedBodyIDValuesStillAccepted(t *testing.T) {
	t.Run("apps create preview", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		withDryRun(t)
		srv := newMutServer(t, fixedReply(200, `{}`))
		res := runMutationCLI(t, srv.srv, 0, nil, "apps", "create", "--display-name", "x", "--grant-policy-id", "policy.with.dots")
		if res.exit != 0 {
			t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
		}
		if !strings.Contains(res.stdout, "policy.with.dots") {
			t.Errorf("preview = %q, want the dotted policy id", res.stdout)
		}
		if got := srv.requests(); len(got) != 0 {
			t.Errorf("requests sent: %v", got)
		}
	})

	t.Run("entitlements create preview", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		withDryRun(t)
		srv := newMutServer(t, fixedReply(200, `{}`))
		res := runMutationCLI(t, srv.srv, 0, nil, "entitlements", "create", "--app-id", "app.1", "--display-name", "x", "--owner-id", "user.1")
		if res.exit != 0 {
			t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
		}
		if !strings.Contains(res.stdout, "user.1") {
			t.Errorf("preview = %q, want the dotted owner id", res.stdout)
		}
	})

	t.Run("functions usage", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		srv := newMutServer(t, fixedReply(200, `{"list":[],"nextPageToken":""}`))
		res := runListCLI(t, srv.srv, "functions", "usage", "fn.1")
		if res.exit != 0 {
			t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
		}
		if got := srv.requests(); len(got) != 1 {
			t.Errorf("requests sent: %v, want 1", got)
		}
	})

	t.Run("automations executions list", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		srv := newMutServer(t, fixedReply(200, `{"list":[],"nextPageToken":""}`))
		res := runListCLI(t, srv.srv, "automations", "executions", "list", "--template-id", "tmpl.1")
		if res.exit != 0 {
			t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
		}
		if got := srv.requests(); len(got) != 1 {
			t.Errorf("requests sent: %v, want 1", got)
		}
	})
}
