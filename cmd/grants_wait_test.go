package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// TestGrantSetFingerprint pins what "the grant set changed" means: identity is
// (entitlement, account), page order is not a change, and a differing count
// always is.
func TestGrantSetFingerprint(t *testing.T) {
	mk := func(pairs ...[2]string) []grantListItem {
		items := make([]grantListItem, 0, len(pairs))
		for _, p := range pairs {
			var it grantListItem
			it.Entitlement.AppEntitlement.ID = p[0]
			it.AppEntitlementUserBinding.AppUser.AppUser.ID = p[1]
			items = append(items, it)
		}
		return items
	}
	a := mk([2]string{"e1", "u1"}, [2]string{"e2", "u2"})
	reordered := mk([2]string{"e2", "u2"}, [2]string{"e1", "u1"})
	added := mk([2]string{"e1", "u1"}, [2]string{"e2", "u2"}, [2]string{"e3", "u3"})
	swapped := mk([2]string{"e1", "u2"}, [2]string{"e2", "u1"})

	if grantSetFingerprint(a) != grantSetFingerprint(reordered) {
		t.Error("page order changed the fingerprint; a re-ordered page is not a change")
	}
	if grantSetFingerprint(a) == grantSetFingerprint(added) {
		t.Error("an added grant did not change the fingerprint")
	}
	if grantSetFingerprint(a) == grantSetFingerprint(swapped) {
		t.Error("re-pairing the same ids did not change the fingerprint")
	}
	if grantSetFingerprint(nil) == grantSetFingerprint(mk([2]string{"e1", "u1"})) {
		t.Error("an empty set and a one-grant set share a fingerprint")
	}
}

// TestGrantsQuerySearchBody pins that both the streaming and --wait paths
// build the same request body -- the reason the builder is shared.
func TestGrantsQuerySearchBody(t *testing.T) {
	q := grantsQuery{appID: "app1", userID: "u1", appUserID: "au1", entitlementID: "e1"}
	b, err := json.Marshal(q.searchBody(25, "tok"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"appIds":["app1"],"appUserIds":["au1"],"entitlementRefs":[{"appId":"app1","id":"e1"}],"pageSize":25,"pageToken":"tok","userId":"u1"}`
	if string(b) != want {
		t.Errorf("body = %s\nwant %s", b, want)
	}

	b, err = json.Marshal(grantsQuery{appID: "app1"}.searchBody(50, ""))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"appIds":["app1"],"pageSize":50}`; string(b) != want {
		t.Errorf("empty filters leaked into the body: %s, want %s", b, want)
	}
}

// grantsWaitServer serves POST /api/v1/search/grants. pages[i] is the list of
// (entitlement,account) id pairs returned by the i-th *full read*, split into
// two pages so each poll exercises pagination; the last entry repeats.
func grantsWaitServer(t *testing.T, reads [][][2]string) (*httptest.Server, *int32) {
	t.Helper()
	var fullReads int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search/grants" {
			t.Errorf("path = %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding body: %v", err)
		}
		tok, _ := body["pageToken"].(string)

		idx := int(atomic.LoadInt32(&fullReads))
		if idx >= len(reads) {
			idx = len(reads) - 1
		}
		pairs := reads[idx]

		// First page carries the first item, second page the rest, so a
		// first-page-only implementation would fingerprint a partial set.
		var page [][2]string
		next := ""
		if tok == "" {
			if len(pairs) > 0 {
				page, next = pairs[:1], "p2"
			}
		} else {
			page = pairs[1:]
		}
		if next == "" {
			atomic.AddInt32(&fullReads, 1)
		}

		items := make([]string, 0, len(page))
		for _, p := range page {
			items = append(items, fmt.Sprintf(
				`{"appEntitlementUserBinding":{"appUser":{"appUser":{"id":%q,"appId":"app1"}}},"entitlement":{"appEntitlement":{"id":%q,"appId":"app1"}}}`,
				p[1], p[0]))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":[%s],"nextPageToken":%q}`, strings.Join(items, ","), next)
	}))
	t.Cleanup(srv.Close)
	return srv, &fullReads
}

func runGrantsWait(t *testing.T, srv *httptest.Server, stableReads int, timeout time.Duration) ([]grantListItem, string, string, error) {
	t.Helper()
	c := client.NewForTesting(srv.URL, srv.Client())
	cmd := &cobra.Command{Use: "list"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	origInterval := grantsWaitPollInterval
	grantsWaitPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { grantsWaitPollInterval = origInterval })

	items, err := waitForGrants(cmd, c, grantsQuery{appID: "app1"}, 50, stableReads, timeout)
	return items, out.String(), errOut.String(), err
}

// TestWaitForGrantsSettles drives the wired --wait path: it must keep polling
// through a change, paginate each poll to completion, and return the set that
// held steady -- not the first one it saw.
func TestWaitForGrantsSettles(t *testing.T) {
	srv, fullReads := grantsWaitServer(t, [][][2]string{
		{{"e1", "u1"}},
		{{"e1", "u1"}, {"e2", "u2"}},
		{{"e1", "u1"}, {"e2", "u2"}},
		{{"e1", "u1"}, {"e2", "u2"}},
	})
	items, out, errOut, err := runGrantsWait(t, srv, 3, 10*time.Second)
	if err != nil {
		t.Fatalf("waitForGrants returned %v, want nil", err)
	}
	if len(items) != 2 {
		t.Errorf("settled on %d grants, want 2 (both pages of the settled read)", len(items))
	}
	if got := atomic.LoadInt32(fullReads); got != 4 {
		t.Errorf("made %d full reads, want 4 (one changing + three identical)", got)
	}
	if out != "" {
		t.Errorf("progress went to stdout, which is the NDJSON stream:\n%s", out)
	}
	if !strings.Contains(errOut, "Grants settled after ") {
		t.Errorf("stderr missing the success line:\n%s", errOut)
	}
}

// TestWaitForGrantsTimesOut covers the timeout path over HTTP and pins that
// the recheck command reproduces the filters the search was run with.
func TestWaitForGrantsTimesOut(t *testing.T) {
	// Every read differs, so the set never settles.
	var reads [][][2]string
	for i := range 50 {
		reads = append(reads, [][2]string{{"e1", "u1"}, {fmt.Sprintf("e%d", i+2), "u2"}})
	}
	srv, _ := grantsWaitServer(t, reads)
	items, out, _, err := runGrantsWait(t, srv, 3, 40*time.Millisecond)
	if err == nil {
		t.Fatal("waitForGrants returned nil, want a timeout error")
	}
	if items != nil {
		t.Errorf("returned %d grants on timeout, want none printed", len(items))
	}
	if out != "" {
		t.Errorf("timed-out wait wrote to stdout:\n%s", out)
	}
	want := "check again later with: c1i grants list --app-id=app1"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err.Error(), want)
	}
}

// TestGrantsListWaitUsageErrors pins the two combinations --wait rejects, both
// as exit-2 usage errors.
func TestGrantsListWaitUsageErrors(t *testing.T) {
	cases := []struct {
		name  string
		flags map[string]string
		want  string
	}{
		{"page-token", map[string]string{"app-id": "app1", "wait": "true", "page-token": "tok"}, "--wait cannot be combined with --page-token"},
		{"wait-stable below 2", map[string]string{"app-id": "app1", "wait": "true", "wait-stable": "1"}, "--wait-stable must be at least 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCmdFlags(t, grantsListCmd)
			for k, v := range tc.flags {
				mustSet(t, grantsListCmd.Flags(), k, v)
			}
			grantsListCmd.SetContext(context.Background())
			err := grantsListCmd.RunE(grantsListCmd, nil)
			var ue *usageError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v, want a *usageError (exit 2)", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}
