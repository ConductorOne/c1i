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
		{"body-file with a convenience flag", []string{"--body-file", "BODY", "--display-name", "x", "--update-mask", "description"}, exitUsage, "mutually exclusive", ""},
		{"mask with steps-file only", []string{"--steps-file", "BODY", "--update-mask", "steps"}, exitUsage, "requires --body-file", ""},
		{"whitespace-only mask", []string{"--body-file", "BODY", "--update-mask", "  "}, exitUsage, "--update-mask requires a non-empty value; name the changed field paths", ""},
		// The mask is checked before the file is read.
		{"mask error beats a missing file", []string{"--body-file", "missing.json", "--update-mask", ""}, exitUsage, "--update-mask requires a non-empty value; name the changed field paths", ""},
		// "" is a set --body-file, so it is read like automations: a missing file, exit 2.
		{"empty body-file", []string{"--body-file", "", "--update-mask", "description"}, exitUsage, "--body-file requires a non-empty value", ""},
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

// The other masked updates share bodyFileMask, so they trim and reject empty
// paths too.
func TestMaskedUpdatesTrimAndRejectEmptyMaskPaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"access-reviews update", []string{"access-reviews", "update", "c-1"}},
		{"mcp classifiers update", []string{"mcp", "classifiers", "update", "cl-1"}},
		{"mcp classifiers policy update", []string{"mcp", "classifiers", "policy", "update"}},
		{"mcp classifiers tool-gates update", []string{"mcp", "classifiers", "tool-gates", "update", "tg-1"}},
	} {
		run := func(t *testing.T, mask string) (mutResult, []entOwnerRequest) {
			s := newMutServer(t, fixedReply(200, `{}`))
			t.Setenv("C1I_URL", mutTestURL)
			body := writeTempJSON(t, "body.json", `{"displayName":"n","description":"d"}`)
			args := append(append([]string{}, tc.args...), "--body-file", body, "--update-mask", mask)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			return r, s.requests()
		}
		t.Run(tc.name+"/padded mask is trimmed", func(t *testing.T) {
			r, reqs := run(t, " displayName, description")
			if r.err != nil || len(reqs) != 1 {
				t.Fatalf("exit %d (%v), %d requests", r.exit, r.err, len(reqs))
			}
			var sent map[string]any
			if err := json.Unmarshal([]byte(reqs[0].body), &sent); err != nil {
				t.Fatal(err)
			}
			if sent["updateMask"] != "displayName,description" {
				t.Errorf("updateMask %v, want displayName,description", sent["updateMask"])
			}
		})
		t.Run(tc.name+"/empty path", func(t *testing.T) {
			r, reqs := run(t, "displayName,,description")
			if r.exit != exitUsage || !strings.Contains(r.err.Error(), "empty path") || len(reqs) != 0 {
				t.Errorf("exit %d (%v), %d requests; want exit 2, empty path, none sent", r.exit, r.err, len(reqs))
			}
		})
	}
}
