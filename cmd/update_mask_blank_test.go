package cmd

import (
	"strings"
	"testing"
)

// A blank --update-mask must be reported as blank, not as missing.
func TestBlankUpdateMaskIsNotReportedAsMissing(t *testing.T) {
	for name, base := range map[string][]string{
		"policies update":               {"policies", "update", "p-1"},
		"automations update":            {"automations", "update", "a-1"},
		"access-reviews update":         {"access-reviews", "update", "c-1"},
		"mcp classifiers update":        {"mcp", "classifiers", "update", "cl-1"},
		"mcp classifiers policy update": {"mcp", "classifiers", "policy", "update"},
		"mcp classifiers tool-gates":    {"mcp", "classifiers", "tool-gates", "update", "tg-1"},
	} {
		run := func(t *testing.T, extra ...string) mutResult {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, `{}`))
			body := writeTempJSON(t, "body.json", `{"displayName":"n"}`)
			args := append(append(append([]string{}, base...), "--body-file", body), extra...)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if n := len(s.requests()); n != 0 {
				t.Errorf("%d request(s) sent, want none", n)
			}
			return r
		}
		t.Run(name+"/blank", func(t *testing.T) {
			for _, blank := range []string{" ", ""} {
				r := run(t, "--update-mask", blank)
				msg := r.err.Error()
				// Adopters cobra already marks required report a blank mask
				// through validateBlankFlags; the rest reach bodyFileMask.
				if r.exit != exitUsage || strings.Contains(msg, "required together") ||
					(!strings.Contains(msg, "--update-mask is blank") && !strings.Contains(msg, "requires a non-empty value")) {
					t.Errorf("mask %q: exit %d, err %v; want exit 2 saying the mask is blank", blank, r.exit, r.err)
				}
			}
		})
		if name != "policies update" && name != "automations update" {
			continue // cobra rejects the missing flag first
		}
		t.Run(name+"/not given", func(t *testing.T) {
			r := run(t)
			if r.exit != exitUsage || !strings.Contains(r.err.Error(), "--update-mask is required together with --body-file") {
				t.Errorf("exit %d, err %v; want the required-together message", r.exit, r.err)
			}
		})
	}
}
