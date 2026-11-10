package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// --- C. Blank --page-token ---

// Supplying --page-token with a blank value turned auto-pagination off, so an
// unset shell variable silently truncated the list. Refuse it before any
// request, on both a POST-based and a GET-based list command.
func TestBlankPageTokenRefused(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"users list empty", []string{"users", "list", "--page-token", ""}},
		{"users list whitespace", []string{"users", "list", "--page-token", "  "}},
		{"functions commits empty", []string{"functions", "commits", "fn-1", "--page-token", ""}},
		{"functions commits whitespace", []string{"functions", "commits", "fn-1", "--page-token", "\t"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, `{"list":[],"nextPageToken":"p2"}`))
			res := runListCLI(t, srv.srv, tc.args...)
			if res.exit != exitUsage {
				t.Fatalf("exit = %d, err = %v; want %d", res.exit, res.err, exitUsage)
			}
			if res.err == nil || !strings.Contains(res.err.Error(), "omit the flag to auto-paginate") {
				t.Errorf("err = %v, want the omit-the-flag hint", res.err)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("requests sent: %v", got)
			}
		})
	}
}

// pagingFixture answers page 1 (no cursor, or any non-"p2" cursor) with
// nextPageToken "p2" and the "p2" page with no token, recording every cursor
// it saw ("" for a request without one).
func pagingFixture(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var tokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		if r.Method == http.MethodGet {
			token = r.URL.Query().Get("page_token")
		} else {
			var body struct {
				PageToken string `json:"pageToken"`
			}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			token = body.PageToken
		}
		mu.Lock()
		tokens = append(tokens, token)
		mu.Unlock()
		next := "p2"
		if token == "p2" {
			next = ""
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"list":[],"nextPageToken":"`+next+`"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), tokens...)
	}
}

// A non-empty cursor still fetches exactly one page; omitting the flag still
// auto-paginates to completion.
func TestPageTokenPagingModes(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string // cursors, in request order
	}{
		{"users list manual", []string{"users", "list", "--page-token", "abc"}, []string{"abc"}},
		{"users list auto", []string{"users", "list"}, []string{"", "p2"}},
		{"functions commits manual", []string{"functions", "commits", "fn-1", "--page-token", "abc"}, []string{"abc"}},
		{"functions commits auto", []string{"functions", "commits", "fn-1"}, []string{"", "p2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv, cursors := pagingFixture(t)
			res := runListCLI(t, srv, tc.args...)
			if res.exit != 0 {
				t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
			}
			got := cursors()
			if len(got) != len(tc.want) {
				t.Fatalf("cursors = %v, want %v (%d request(s))", got, tc.want, len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("cursor[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
