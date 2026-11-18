package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const auditsTaskID = "zz-c1i-audit-task"

// runTasksAudits runs `tasks audits` against route with the list client stubbed.
func runTasksAudits(t *testing.T, route func(r *http.Request, n int) mutReply, args ...string) (*mutServer, mutResult) {
	t.Helper()
	s := newMutServer(t, route)
	r := runCLI(t, func() {
		orig := newListClient
		newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
			return client.NewForTesting(s.srv.URL, s.srv.Client()), nil
		}
		t.Cleanup(func() { newListClient = orig })
		t.Setenv("C1I_URL", "https://example.invalid")
	}, nil, append([]string{"tasks", "audits"}, args...)...)
	return s, r
}

// auditsRoute answers the task pre-read, then serves pages in order.
func auditsRoute(pages ...string) func(r *http.Request, n int) mutReply {
	return func(r *http.Request, n int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{status: 200, body: `{"taskView":{"task":{"id":"` + auditsTaskID + `"}}}`}
		}
		i := n - 2
		if i >= len(pages) {
			i = len(pages) - 1
		}
		return mutReply{status: 200, body: pages[i]}
	}
}

// The server answers an unknown task with an empty 200, so the pre-read's 404
// is what exits 4; nothing is listed.
func TestTasksAuditsUnknownTaskExits4(t *testing.T) {
	s, r := runTasksAudits(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{status: 404, body: `{"code":5,"message":"Task with the given ID is not found."}`}
		}
		return mutReply{status: 200, body: `{"list":[],"nextPageToken":""}`}
	}, auditsTaskID)
	if r.exit != exitNotFound {
		t.Fatalf("exit = %d, want %d; stderr %q", r.exit, exitNotFound, r.stderr)
	}
	if len(s.seen) != 1 || s.seen[0].method != http.MethodGet || s.seen[0].rawPath != "/api/v1/tasks/"+auditsTaskID {
		t.Errorf("requests = %+v, want only the task GET", s.seen)
	}
}

// Rows are the raw events, every page, and the body carries the task id and
// each filter flag under its API name.
func TestTasksAuditsEmitsRawEventsAcrossPages(t *testing.T) {
	s, r := runTasksAudits(t, auditsRoute(
		`{"list":[{"id":"ev1","taskCreated":{}}],"nextPageToken":"t2","totalCount":"2"}`,
		`{"list":[{"id":"ev2","comment":{"comment":"hi"}}],"nextPageToken":"","totalCount":"2"}`,
	), auditsTaskID, "--newest-first", "--exclude-comments")
	if r.exit != 0 {
		t.Fatalf("exit = %d; stderr %q", r.exit, r.stderr)
	}
	rows := decodeNDJSONRows(t, r.stdout)
	if len(rows) != 2 || rows[0]["id"] != "ev1" || rows[1]["id"] != "ev2" {
		t.Fatalf("rows = %v, want ev1 then ev2", rows)
	}
	if _, ok := rows[1]["comment"].(map[string]any); !ok {
		t.Errorf("row 2 = %v, want the raw event with its comment object", rows[1])
	}
	if strings.Contains(r.stdout, "totalCount") {
		t.Errorf("stdout carries the envelope's totalCount: %q", r.stdout)
	}
	if len(s.seen) != 3 {
		t.Fatalf("requests = %d, want GET + 2 pages", len(s.seen))
	}
	var first, second map[string]any
	_ = json.Unmarshal([]byte(s.seen[1].body), &first)
	_ = json.Unmarshal([]byte(s.seen[2].body), &second)
	if s.seen[1].rawPath != "/api/v1/task/audits" || first["taskId"] != auditsTaskID ||
		first["newestFirst"] != true || first["excludeComments"] != true {
		t.Errorf("first page = %s %s", s.seen[1].rawPath, s.seen[1].body)
	}
	if _, ok := first["commentsOnly"]; ok {
		t.Errorf("first page sent an unset commentsOnly: %s", s.seen[1].body)
	}
	if second["pageToken"] != "t2" || second["taskId"] != auditsTaskID {
		t.Errorf("second page = %s, want the cursor and the task id", s.seen[2].body)
	}
}

// The server refuses the pair with a 400; c1i refuses it before any request.
func TestTasksAuditsCommentFlagsAreExclusive(t *testing.T) {
	s, r := runTasksAudits(t, auditsRoute(`{"list":[]}`), auditsTaskID, "--comments-only", "--exclude-comments")
	if r.exit != exitUsage {
		t.Fatalf("exit = %d, want %d; stderr %q", r.exit, exitUsage, r.stderr)
	}
	if len(s.seen) != 0 {
		t.Errorf("sent %d request(s), want none", len(s.seen))
	}
}
