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
	"sync"
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

// grantsFake is the fake grants-search server plus what it observed. Recording
// the page sizes it was sent is what ties the --page-size flag to the request
// the wait actually issues; without it, pinning that argument at the call site
// is invisible to every test.
type grantsFake struct {
	srv       *httptest.Server
	fullReads int32

	mu        sync.Mutex
	pageSizes []int
}

func (f *grantsFake) reads() int { return int(atomic.LoadInt32(&f.fullReads)) }

func (f *grantsFake) observedPageSizes() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.pageSizes...)
}

// grantsWaitServer serves POST /api/v1/search/grants. pages[i] is the list of
// (entitlement,account) id pairs returned by the i-th *full read*, split into
// two pages so each poll exercises pagination; the last entry repeats.
func grantsWaitServer(t *testing.T, reads [][][2]string) *grantsFake {
	t.Helper()
	f := &grantsFake{}
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
		if ps, ok := body["pageSize"].(float64); ok {
			f.mu.Lock()
			f.pageSizes = append(f.pageSizes, int(ps))
			f.mu.Unlock()
		}

		idx := int(atomic.LoadInt32(&f.fullReads))
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
			atomic.AddInt32(&f.fullReads, 1)
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
	f.srv = srv
	return f
}

func runGrantsWait(t *testing.T, srv *httptest.Server, stableReads, minGrants int, timeout time.Duration) ([]grantListItem, string, string, error) {
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

	items, err := waitForGrants(cmd, c, grantsQuery{appID: "app1"}, 50, stableReads, minGrants, timeout)
	return items, out.String(), errOut.String(), err
}

// TestWaitForGrantsSettles drives the wired --wait path: it must keep polling
// through a change, paginate each poll to completion, and return the set that
// held steady -- not the first one it saw.
func TestWaitForGrantsSettles(t *testing.T) {
	fake := grantsWaitServer(t, [][][2]string{
		{{"e1", "u1"}},
		{{"e1", "u1"}, {"e2", "u2"}},
		{{"e1", "u1"}, {"e2", "u2"}},
		{{"e1", "u1"}, {"e2", "u2"}},
	})
	items, out, errOut, err := runGrantsWait(t, fake.srv, 3, 0, 10*time.Second)
	if err != nil {
		t.Fatalf("waitForGrants returned %v, want nil", err)
	}
	if len(items) != 2 {
		t.Errorf("settled on %d grants, want 2 (both pages of the settled read)", len(items))
	}
	if got := fake.reads(); got != 4 {
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
	fake := grantsWaitServer(t, reads)
	items, out, _, err := runGrantsWait(t, fake.srv, 3, 0, 40*time.Millisecond)
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

// TestGrantsListWaitEndToEnd drives the user-visible path -- grantsListCmd.RunE
// with --wait -- rather than waitForGrants directly, so every flag is tied to
// the outcome it is supposed to produce. Calling the helper directly leaves the
// plumbing untested: replacing an argument at the call site with a literal, or
// transposing two adjacent ints, both compile and change behavior.
func TestGrantsListWaitEndToEnd(t *testing.T) {
	threeGrants := [][2]string{{"e1", "u1"}, {"e2", "u2"}, {"e3", "u3"}}
	oneGrant := [][2]string{{"e1", "u1"}}

	for _, tc := range []struct {
		name  string
		reads [][][2]string
		flags map[string]string
		// parentTimeout bounds the command's own context. A case that expects
		// a --wait-timeout to fire sets this well above it, so pinning
		// waitTimeout to its 4m default fails promptly with a cancellation
		// instead of hanging the suite.
		parentTimeout time.Duration
		wantErr       string
		wantRows      int
		wantReads     int
		wantPageSize  int
	}{
		{
			name:         "settles and prints the whole set",
			reads:        [][][2]string{oneGrant, threeGrants, threeGrants},
			flags:        map[string]string{"wait-stable": "2"},
			wantRows:     3,
			wantReads:    3,
			wantPageSize: 50,
		},
		{
			name:         "limit truncates what is printed, not what it settles on",
			reads:        [][][2]string{oneGrant, threeGrants, threeGrants},
			flags:        map[string]string{"wait-stable": "2", "limit": "2"},
			wantRows:     2,
			wantReads:    3,
			wantPageSize: 50,
		},
		{
			// Three empty reads, so --wait-min=0 would settle on them at the
			// third; the correct wait must outlast them and take the grant.
			// Transposing --wait-stable and --wait-min turns this into a floor
			// of 3 that one grant can never clear, so it times out instead.
			name:         "wait-min outlasts an empty prefix",
			reads:        [][][2]string{nil, nil, nil, oneGrant, oneGrant, oneGrant},
			flags:        map[string]string{"wait-stable": "3", "wait-min": "1"},
			wantRows:     1,
			wantReads:    6,
			wantPageSize: 50,
		},
		{
			name:          "wait-min times out rather than settling empty",
			reads:         [][][2]string{nil},
			flags:         map[string]string{"wait-stable": "2", "wait-min": "1", "wait-timeout": "60ms"},
			parentTimeout: 3 * time.Second,
			wantErr:       "timed out after 60ms waiting for at least 1 matching grant(s) to appear and stop changing; this is not necessarily a failure \u2014 grant provisioning can take several minutes, check again later with: c1i grants list --app-id=app1",
		},
		{
			name:         "page-size reaches the request the wait issues",
			reads:        [][][2]string{threeGrants, threeGrants},
			flags:        map[string]string{"wait-stable": "2", "page-size": "37"},
			wantRows:     3,
			wantReads:    2,
			wantPageSize: 37,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := grantsWaitServer(t, tc.reads)
			orig := newListClient
			newListClient = func(*cobra.Command, string) (*client.Client, error) {
				return client.NewForTesting(fake.srv.URL, fake.srv.Client()), nil
			}
			t.Cleanup(func() { newListClient = orig })
			t.Setenv("C1I_URL", "https://example.invalid")

			origInterval := grantsWaitPollInterval
			grantsWaitPollInterval = 5 * time.Millisecond
			t.Cleanup(func() { grantsWaitPollInterval = origInterval })

			resetCmdFlags(t, grantsListCmd)
			mustSet(t, grantsListCmd.Flags(), "app-id", "app1")
			mustSet(t, grantsListCmd.Flags(), "wait", "true")
			for name, val := range tc.flags {
				mustSet(t, grantsListCmd.Flags(), name, val)
			}

			ctx := context.Background()
			if tc.parentTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.parentTimeout)
				defer cancel()
			}

			var out, errOut bytes.Buffer
			grantsListCmd.SetOut(&out)
			grantsListCmd.SetErr(&errOut)
			grantsListCmd.SetContext(ctx)
			err := grantsListCmd.RunE(grantsListCmd, nil)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("RunE returned nil, want %q", tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Fatalf("RunE error =\n%q\nwant\n%q", err.Error(), tc.wantErr)
				}
				if out.String() != "" {
					t.Errorf("wrote rows to stdout despite not settling:\n%s", out.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("RunE returned %v, want nil", err)
			}

			lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
			if len(lines) != tc.wantRows {
				t.Fatalf("printed %d rows, want %d:\n%s", len(lines), tc.wantRows, out.String())
			}
			for i, line := range lines {
				var row map[string]any
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					t.Fatalf("stdout line %d is not JSON (%v): %q", i, err, line)
				}
				if row["entitlement_id"] == "" || row["app_user_id"] == "" {
					t.Errorf("row %d is missing its ids: %v", i, row)
				}
			}
			if got := fake.reads(); got != tc.wantReads {
				t.Errorf("made %d full reads, want %d", got, tc.wantReads)
			}
			for i, ps := range fake.observedPageSizes() {
				if ps != tc.wantPageSize {
					t.Errorf("request %d asked for pageSize %d, want %d", i, ps, tc.wantPageSize)
				}
			}
			if !strings.Contains(errOut.String(), "Grants settled after ") {
				t.Errorf("stderr missing the success line:\n%s", errOut.String())
			}
		})
	}
}

// TestFetchAllGrantsRejectsAStuckCursor pins the same-token guard: without it a
// server re-issuing one cursor turns every poll into a request storm bounded
// only by --wait-timeout.
func TestFetchAllGrantsRejectsAStuckCursor(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"list":[],"nextPageToken":"stuck"}`))
	}))
	t.Cleanup(srv.Close)

	// Bounded, so removing the guard fails this test promptly instead of
	// hanging it until go test's own panic timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := client.NewForTesting(srv.URL, srv.Client())
	_, err := fetchAllGrants(ctx, c, grantsQuery{appID: "app1"}, 50)
	if err == nil {
		t.Fatal("fetchAllGrants returned nil; a stuck cursor must be an error, not a loop")
	}
	if !strings.Contains(err.Error(), "same nextPageToken twice in a row") {
		t.Errorf("err = %q, want it to name the stuck cursor", err.Error())
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Errorf("made %d requests before giving up, want 2", got)
	}
}

// TestGrantsListWaitUsageErrors pins the combinations --wait rejects, all as
// exit-2 usage errors.
func TestGrantsListWaitUsageErrors(t *testing.T) {
	cases := []struct {
		name  string
		flags map[string]string
		want  string
	}{
		{"page-token", map[string]string{"app-id": "app1", "wait": "true", "page-token": "tok"}, "--wait cannot be combined with --page-token"},
		{"wait-stable below 2", map[string]string{"app-id": "app1", "wait": "true", "wait-stable": "1"}, "--wait-stable must be at least 2"},
		{"wait-stable can never fit in wait-timeout", map[string]string{"app-id": "app1", "wait": "true", "wait-stable": "4", "wait-timeout": "10s"}, "can never allow"},
		{"negative wait-min", map[string]string{"app-id": "app1", "wait": "true", "wait-min": "-1"}, "--wait-min cannot be negative"},
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

// TestWaitForGrantsSettlesEmptyWithoutAMinimum is the reproduction, kept as a
// test so the default's behavior is a choice on record and not an accident: an
// empty result IS stable, so with no floor the wait settles fast and exits 0
// with zero rows. That is correct for a revoke and wrong for a grant, which is
// what --wait-min exists to say.
func TestWaitForGrantsSettlesEmptyWithoutAMinimum(t *testing.T) {
	fake := grantsWaitServer(t, [][][2]string{nil})
	items, _, errOut, err := runGrantsWait(t, fake.srv, 3, 0, 10*time.Second)
	if err != nil {
		t.Fatalf("wait returned %v, want nil (empty-and-stable is a settle)", err)
	}
	if len(items) != 0 {
		t.Errorf("settled on %d grants, want 0", len(items))
	}
	if got := fake.reads(); got != 3 {
		t.Errorf("made %d full reads, want 3 (it settles as soon as the streak fills)", got)
	}
	if !strings.Contains(errOut, "Grants settled after ") {
		t.Errorf("stderr missing the success line:\n%s", errOut)
	}
}

// TestWaitForGrantsMinimumOutwaitsAnEmptySet is the fix: with --wait-min the
// wait must NOT settle on the empty reads that precede the grant landing, and
// must return the grant once it appears.
func TestWaitForGrantsMinimumOutwaitsAnEmptySet(t *testing.T) {
	// Empty for the first four reads, then the grant lands and holds.
	reads := [][][2]string{nil, nil, nil, nil,
		{{"e1", "u1"}}, {{"e1", "u1"}}, {{"e1", "u1"}}}
	fake := grantsWaitServer(t, reads)
	items, _, _, err := runGrantsWait(t, fake.srv, 3, 1, 10*time.Second)
	if err != nil {
		t.Fatalf("wait returned %v, want nil", err)
	}
	if len(items) != 1 {
		t.Fatalf("settled on %d grants, want 1; an empty prefix was accepted", len(items))
	}
}

// TestWaitForGrantsMinimumTimesOutRatherThanSettlingEmpty pins the other half:
// if the grant never lands, --wait-min must produce a timeout, not a confident
// empty success.
func TestWaitForGrantsMinimumTimesOutRatherThanSettlingEmpty(t *testing.T) {
	fake := grantsWaitServer(t, [][][2]string{nil})
	items, out, _, err := runGrantsWait(t, fake.srv, 3, 1, 40*time.Millisecond)
	if err == nil {
		t.Fatal("wait returned nil; an unmet --wait-min must time out, not settle empty")
	}
	if items != nil {
		t.Errorf("returned %d grants, want none", len(items))
	}
	if out != "" {
		t.Errorf("wrote to stdout despite not settling:\n%s", out)
	}
	if !strings.Contains(err.Error(), "at least 1 matching grant(s) to appear and stop changing") {
		t.Errorf("timeout error %q does not say the minimum was the unmet condition", err.Error())
	}
}

// TestStableAndAtLeastFeedsStabilityOnEveryPoll pins the ordering rule the
// combinator exists for. The set dips below the floor and comes back to the
// SAME value; short-circuiting past the stateful predicate on the low poll
// would hide that dip and settle on a stale streak.
func TestStableAndAtLeastFeedsStabilityOnEveryPoll(t *testing.T) {
	done := stableAndAtLeast(
		3,
		func(n int) int { return n },
		func(n int) bool { return n >= 1 },
	)
	// 5,5 builds a streak of 2; the 0 must reset it; the run after must build
	// a fresh streak of 3, so the first true is the final read.
	seq := []int{5, 5, 0, 5, 5, 5}
	firstTrue := -1
	for i, v := range seq {
		if done(v) {
			firstTrue = i
			break
		}
	}
	if firstTrue != 5 {
		t.Errorf("first satisfied at index %d over %v, want 5; the dip below the floor was not fed to the stability predicate", firstTrue, seq)
	}
}
