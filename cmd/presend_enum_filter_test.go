package cmd

import (
	"strings"
	"testing"
)

// --- B. Whitespace-only enum filters ---

// A whitespace-only enum value used to be trimmed to "" and dropped, so the
// command exited 0 with an unfiltered list. It must be refused instead.
func TestWhitespaceOnlyEnumFiltersRefused(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"tasks list --state", []string{"tasks", "list", "--state", " "}},
		{"requests list --state", []string{"requests", "list", "--state", " ", "--user-id", "user-1"}},
		{"requests list --type", []string{"requests", "list", "--type", " ", "--user-id", "user-1"}},
		{"automations executions list --state", []string{"automations", "executions", "list", "--state", " "}},
		{"export events --sort", []string{"export", "events", "--sort", " "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, `{"list":[],"nextPageToken":""}`))
			res := runListCLI(t, srv.srv, tc.args...)
			if res.exit != exitUsage {
				t.Fatalf("exit = %d, err = %v; want %d", res.exit, res.err, exitUsage)
			}
			if res.err == nil || !strings.Contains(res.err.Error(), "invalid --") {
				t.Errorf("err = %v, want the enum usage error", res.err)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("requests sent: %v", got)
			}
		})
	}
}

// An explicitly empty value still means "no filter"/"the default"; only a
// non-empty raw value reaches the parser.
func TestEmptyEnumFiltersStayUnfiltered(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    []string
		notWant []string
	}{
		{
			name:    "tasks list --state",
			args:    []string{"tasks", "list", "--state", ""},
			notWant: []string{`"taskStates"`},
		},
		{
			name: "requests list --state",
			args: []string{"requests", "list", "--state", "", "--user-id", "user-1"},
			want: []string{`"taskTypes":[{"grant":{}},{"revoke":{}}]`},
		},
		{
			name: "requests list --type",
			args: []string{"requests", "list", "--type", "", "--user-id", "user-1"},
			want: []string{`"taskTypes":[{"grant":{}},{"revoke":{}}]`},
		},
		{
			name:    "automations executions list --state",
			args:    []string{"automations", "executions", "list", "--state", ""},
			notWant: nil,
		},
		{
			name: "export events --sort",
			args: []string{"export", "events", "--sort", ""},
			want: []string{`"sortDirection":"SORT_DIRECTION_ASC"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, `{"list":[],"nextPageToken":""}`))
			res := runListCLI(t, srv.srv, tc.args...)
			if res.exit != 0 {
				t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
			}
			got := srv.requests()
			if len(got) != 1 {
				t.Fatalf("requests = %v, want 1", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got[0].body, want) {
					t.Errorf("body = %s, want %s", got[0].body, want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(got[0].body, notWant) {
					t.Errorf("body = %s, want no %s", got[0].body, notWant)
				}
			}
		})
	}
}

// A real value still filters.
func TestEnumFiltersStillApply(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{"tasks list --state open", []string{"tasks", "list", "--state", "open"}, []string{`"taskStates":["TASK_STATE_OPEN"]`}},
		{"requests list --type grant", []string{"requests", "list", "--type", "grant", "--user-id", "user-1"}, []string{`"taskTypes":[{"grant":{}}]`}},
		{"requests list --state closed", []string{"requests", "list", "--state", "closed", "--user-id", "user-1"}, []string{`"taskStates":["TASK_STATE_CLOSED"]`}},
		{"export events --sort desc", []string{"export", "events", "--sort", "desc"}, []string{`"sortDirection":"SORT_DIRECTION_DESC"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, `{"list":[],"nextPageToken":""}`))
			res := runListCLI(t, srv.srv, tc.args...)
			if res.exit != 0 {
				t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
			}
			got := srv.requests()
			if len(got) != 1 {
				t.Fatalf("requests = %v, want 1", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got[0].body, want) {
					t.Errorf("body = %s, want %s", got[0].body, want)
				}
			}
		})
	}

	t.Run("automations executions list --state done", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		srv := newMutServer(t, fixedReply(200, `{"list":[`+
			`{"automationExecution":{"id":"e1","state":"AUTOMATION_EXECUTION_STATE_DONE"}},`+
			`{"automationExecution":{"id":"e2","state":"AUTOMATION_EXECUTION_STATE_ERROR"}}`+
			`],"nextPageToken":""}`))
		res := runListCLI(t, srv.srv, "automations", "executions", "list", "--state", "done")
		if res.exit != 0 {
			t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
		}
		if !strings.Contains(res.stdout, `"id":"e1"`) || strings.Contains(res.stdout, `"id":"e2"`) {
			t.Errorf("stdout = %q, want only the DONE row", res.stdout)
		}
	})
}
