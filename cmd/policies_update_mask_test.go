package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// policies update shares automations' --body-file/--update-mask rules
// (bodyFileMask); these drive them through the CLI.
func TestPoliciesUpdateBodyFileMaskRules(t *testing.T) {
	const id = "pol-1"
	body := `{"description":"d","steps":[]}`
	for _, tc := range []struct {
		name     string
		args     []string
		wantExit int
		wantErr  string
		wantMask string
	}{
		{"mask without body-file", []string{"--display-name", "x", "--update-mask", "description"}, exitUsage, "requires --body-file", ""},
		{"padded mask is trimmed", []string{"--body-file", "BODY", "--update-mask", " description, steps"}, 0, "", "description,steps"},
		{"empty path", []string{"--body-file", "BODY", "--update-mask", "description,,steps"}, exitUsage, "empty path", ""},
		// "" is a set --body-file, so it is read like automations: a missing file, exit 2.
		{"empty body-file", []string{"--body-file", "", "--update-mask", "description"}, exitUsage, "reading --body-file", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, `{"policy":{"id":"`+id+`"}}`))
			t.Setenv("C1I_URL", mutTestURL)
			bodyPath := writeTempJSON(t, "body.json", body)
			args := []string{"policies", "update", id}
			for _, a := range tc.args {
				args = append(args, strings.ReplaceAll(a, "BODY", bodyPath))
			}
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.wantExit {
				t.Fatalf("exit %d (%v), want %d", r.exit, r.err, tc.wantExit)
			}
			if tc.wantErr != "" {
				if !strings.Contains(r.err.Error(), tc.wantErr) {
					t.Errorf("err %q, want it to contain %q", r.err, tc.wantErr)
				}
				if n := len(s.requests()); n != 0 {
					t.Errorf("%d requests sent, want none", n)
				}
				return
			}
			reqs := s.requests()
			if len(reqs) != 1 {
				t.Fatalf("%d requests, want 1", len(reqs))
			}
			var sent map[string]any
			if err := json.Unmarshal([]byte(reqs[0].body), &sent); err != nil {
				t.Fatal(err)
			}
			if sent["updateMask"] != tc.wantMask {
				t.Errorf("updateMask %v, want %q", sent["updateMask"], tc.wantMask)
			}
		})
	}
}
