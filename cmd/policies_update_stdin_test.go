package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A patch with policySteps but no policyType builds its body twice (offline,
// then inside the type lookup), so stdin must be read once and reused.
func TestPoliciesUpdateStdinInputsSurviveTypeLookup(t *testing.T) {
	const policyID = "zz-c1i-test-policy-stdin"
	steps := `[{"approval":{"self":{}}}]`
	for _, tc := range []struct {
		name  string
		flags map[string]string
		stdin string
	}{
		{"steps-file", map[string]string{"steps-file": "-"}, steps},
		{"body-file", map[string]string{"body-file": "-", "update-mask": "policySteps"},
			`{"policySteps":{"grant":{"steps":` + steps + `}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posted map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodGet:
					_, _ = fmt.Fprint(w, `{"policy":{"id":"`+policyID+`","policyType":"POLICY_TYPE_GRANT"}}`)
				case http.MethodPost:
					b, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(b, &posted)
					_, _ = fmt.Fprint(w, `{"policy":{"id":"`+policyID+`"}}`)
				}
			}))
			defer srv.Close()

			resetCmdFlags(t, policiesUpdateCmd)
			stubPoliciesClient(t, srv)
			withRealDryRun(t)
			t.Setenv("C1I_URL", "https://example.invalid")
			for k, v := range tc.flags {
				_ = policiesUpdateCmd.Flags().Set(k, v)
			}
			var out bytes.Buffer
			policiesUpdateCmd.SetOut(&out)
			policiesUpdateCmd.SetIn(strings.NewReader(tc.stdin))
			t.Cleanup(func() { policiesUpdateCmd.SetIn(nil) })
			policiesUpdateCmd.SetContext(context.Background())

			if err := policiesUpdateCmd.RunE(policiesUpdateCmd, []string{policyID}); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			p, _ := posted["policy"].(map[string]any)
			ps, _ := p["policySteps"].(map[string]any)
			if _, ok := ps["grant"]; !ok {
				t.Errorf("posted body lacks policySteps.grant: %v", posted)
			}
			if !strings.Contains(out.String(), policyID) {
				t.Errorf("stdout = %q, want the updated policy", out.String())
			}
		})
	}
}

func TestPoliciesUpdateRefusesTwoStdinInputs(t *testing.T) {
	resetCmdFlags(t, policiesUpdateCmd)
	_ = policiesUpdateCmd.Flags().Set("steps-file", "-")
	_ = policiesUpdateCmd.Flags().Set("rules-file", "-")
	policiesUpdateCmd.SetContext(context.Background())
	err := policiesUpdateCmd.RunE(policiesUpdateCmd, []string{"pol1"})
	if exitCode(err) != exitUsage || err == nil || !strings.Contains(err.Error(), "only one of") {
		t.Fatalf("err = %v (exit %d), want the one-stdin usage error", err, exitCode(err))
	}
}
