package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Each create posts its own route with the flags under their API names, and
// an unset optional field is omitted rather than sent empty.
func TestRequestsCreateActionBodies(t *testing.T) {
	form := `{"zz_name":"zz-group"}`
	cases := []struct {
		name string
		args []string
		path string
		want map[string]any
	}{
		{"action minimal", []string{"action", "--action-id", "act1"}, "/api/v1/task/action",
			map[string]any{"actionId": "act1"}},
		{"action full", []string{"action", "--action-id", "act1", "--description", "why", "--group-only", "--form-values-file", "@" + form}, "/api/v1/task/action",
			map[string]any{"actionId": "act1", "description": "why", "groupOnly": true, "formValues": map[string]any{"zz_name": "zz-group"}}},
		{"resource-action minimal", []string{"resource-action", "--action-id", "act2"}, "/api/v1/task/resource-action",
			map[string]any{"actionId": "act2"}},
		{"resource-action form", []string{"resource-action", "--action-id", "act2", "--form-values-file", "@" + form}, "/api/v1/task/resource-action",
			map[string]any{"actionId": "act2", "formValues": map[string]any{"zz_name": "zz-group"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for i, a := range c.args {
				if len(a) > 1 && a[0] == '@' {
					c.args[i] = writeTempJSON(t, "form.json", a[1:])
				}
			}
			t.Setenv("C1I_URL", "https://example.invalid")
			s := newMutServer(t, fixedReply(http.StatusOK, tblTask("")))
			r := runMutationCLI(t, s.srv, 0, nil, append([]string{"requests", "create"}, c.args...)...)
			if r.exit != 0 {
				t.Fatalf("exit = %d; stderr %q", r.exit, r.stderr)
			}
			if len(s.seen) != 1 || s.seen[0].method != http.MethodPost || s.seen[0].rawPath != c.path {
				t.Fatalf("requests = %+v, want one POST %s", s.seen, c.path)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(s.seen[0].body), &got); err != nil {
				t.Fatal(err)
			}
			gb, _ := json.Marshal(got)
			wb, _ := json.Marshal(c.want)
			if string(gb) != string(wb) {
				t.Errorf("body = %s, want %s", gb, wb)
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &out); err != nil || out["id"] != "task-1" || out["taskView"] != nil {
				t.Errorf("stdout = %s, want the task unwrapped to a top-level id", r.stdout)
			}
		})
	}
}

// The server answers an unknown action id with 404 "action not found".
func TestRequestsCreateActionUnknownIDExits4(t *testing.T) {
	for _, sub := range []string{"action", "resource-action"} {
		t.Run(sub, func(t *testing.T) {
			t.Setenv("C1I_URL", "https://example.invalid")
			s := newMutServer(t, fixedReply(http.StatusNotFound, `{"code":5,"message":"action not found"}`))
			r := runMutationCLI(t, s.srv, 0, nil, "requests", "create", sub, "--action-id", "zzzzzzzzzzzzzzzzzzzzzzzzzzz")
			if r.exit != exitNotFound {
				t.Fatalf("exit = %d, want %d; stderr %q", r.exit, exitNotFound, r.stderr)
			}
		})
	}
}
