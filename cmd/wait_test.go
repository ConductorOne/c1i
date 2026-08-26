package cmd

import (
	"bytes"
	"context"
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
	"github.com/spf13/pflag"
)

// newWaitTestCmd returns a command whose stdout is captured, so runWait's
// progress lines can be asserted on.
func newWaitTestCmd(ctx context.Context) (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "test"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(ctx)
	return cmd, &out
}

// TestUntilStable pins the predicate presence-waiting cannot express: a value
// must hold steady across n consecutive reads. The n=2 case is the shipped
// bug it exists to prevent -- a pause mid-stream reads as completion.
func TestUntilStable(t *testing.T) {
	cases := []struct {
		name string
		n    int
		seq  []int
		// wantFirstTrue is the index of the first read at which the
		// predicate is satisfied, or -1 if it never is.
		wantFirstTrue int
	}{
		{"n=3 over a streaming discovery, settles at the third equal read", 3, []int{0, 40, 40, 101, 101, 101}, 5},
		{"n=2 fires on a mid-stream pause -- why two equal reads is not enough", 2, []int{0, 40, 40, 101}, 2},
		{"n=3 does not fire on that same pause", 3, []int{0, 40, 40, 101}, -1},
		{"n=1 accepts the very first read", 1, []int{7}, 0},
		{"n=0 also accepts the first read, it does not hang", 0, []int{7}, 0},
		{"negative n also accepts the first read", -5, []int{7}, 0},
		{"a change resets the streak", 3, []int{5, 5, 7, 5, 5}, -1},
		{"streak resumes after a reset", 2, []int{5, 7, 5, 5}, 3},
		{"zero is a value like any other", 2, []int{0, 0}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done := untilStable[int](tc.n)
			got := -1
			for i, v := range tc.seq {
				if done(v) {
					got = i
					break
				}
			}
			if got != tc.wantFirstTrue {
				t.Errorf("untilStable(%d) over %v first satisfied at index %d, want %d",
					tc.n, tc.seq, got, tc.wantFirstTrue)
			}
		})
	}
}

// TestUntilStableOnStrings pins that untilStable works on any comparable
// value, not just counts -- a fingerprint string is the other intended use.
func TestUntilStableOnStrings(t *testing.T) {
	done := untilStable[string](2)
	for i, v := range []string{"a", "b", "b"} {
		got := done(v)
		want := i == 2
		if got != want {
			t.Errorf("read %d (%q): got %v, want %v", i, v, got, want)
		}
	}
}

// TestUntilPresentIsStateless guards the difference between the two
// predicates: untilStable is deliberately stateful, untilPresent must not be,
// or a transient disappearance would be remembered.
func TestUntilPresentIsStateless(t *testing.T) {
	done := untilPresent([]string{"a"})
	for i, tc := range []struct {
		got  []string
		want bool
	}{
		{[]string{"a"}, true},
		{nil, false},
		{[]string{"b", "a"}, true},
		{[]string{"b"}, false},
	} {
		if got := done(tc.got); got != tc.want {
			t.Errorf("call %d: untilPresent([a])(%v) = %v, want %v", i, tc.got, got, tc.want)
		}
	}
}

// TestRunWaitSucceedsAfterSeveralPolls covers the loop's happy path and the
// first-poll suppression: the "still waiting" line must not appear for the
// poll that happens immediately, before any time has elapsed.
func TestRunWaitSucceedsAfterSeveralPolls(t *testing.T) {
	cmd, out := newWaitTestCmd(context.Background())
	var polls int
	err := runWait(cmd, waitOp[int]{
		Poll:     func(context.Context) (int, error) { polls++; return polls, nil },
		Done:     func(v int) bool { return v >= 3 },
		Interval: time.Millisecond,
		Timeout:  10 * time.Second,
		Subject:  "widgets to settle on thing X",
		Success:  "Widgets settled on thing X",
		Slow:     "settling can take several minutes",
		Recheck:  "c1i widgets get X",
	})
	if err != nil {
		t.Fatalf("runWait returned %v, want nil", err)
	}
	if polls != 3 {
		t.Errorf("polled %d times, want 3", polls)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d output lines, want 2 (one progress + one success):\n%s", len(lines), out.String())
	}
	if !strings.HasPrefix(lines[0], "Still waiting for widgets to settle on thing X (") {
		t.Errorf("progress line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "Widgets settled on thing X after ") || !strings.HasSuffix(lines[1], ".") {
		t.Errorf("success line = %q", lines[1])
	}
}

// TestRunWaitSuppressesProgressOnImmediateSuccess pins that a wait satisfied
// by its very first poll prints only the success line.
func TestRunWaitSuppressesProgressOnImmediateSuccess(t *testing.T) {
	cmd, out := newWaitTestCmd(context.Background())
	if err := runWait(cmd, waitOp[int]{
		Poll:     func(context.Context) (int, error) { return 1, nil },
		Done:     func(int) bool { return true },
		Interval: time.Millisecond,
		Timeout:  time.Second,
		Subject:  "s",
		Success:  "Done",
		Slow:     "it can be slow",
		Recheck:  "c1i check",
	}); err != nil {
		t.Fatalf("runWait returned %v, want nil", err)
	}
	if strings.Contains(out.String(), "Still waiting") {
		t.Errorf("first-poll success printed a progress line:\n%s", out.String())
	}
}

// TestRunWaitHonorsOutWriter pins that op.Out redirects every line runWait
// prints. A list command sets it to stderr; if runWait ignored it, prose would
// land in the middle of that command's NDJSON stream.
func TestRunWaitHonorsOutWriter(t *testing.T) {
	cmd, stdout := newWaitTestCmd(context.Background())
	var elsewhere bytes.Buffer
	var polls int
	if err := runWait(cmd, waitOp[int]{
		Poll:     func(context.Context) (int, error) { polls++; return polls, nil },
		Done:     func(v int) bool { return v >= 3 },
		Interval: time.Millisecond,
		Timeout:  time.Second,
		Subject:  "s",
		Success:  "Done",
		Slow:     "slow",
		Recheck:  "c1i check",
		Out:      &elsewhere,
	}); err != nil {
		t.Fatalf("runWait returned %v, want nil", err)
	}
	if stdout.String() != "" {
		t.Errorf("runWait wrote to stdout despite op.Out:\n%s", stdout.String())
	}
	if !strings.Contains(elsewhere.String(), "Still waiting for s (") ||
		!strings.Contains(elsewhere.String(), "Done after ") {
		t.Errorf("op.Out did not receive both lines:\n%s", elsewhere.String())
	}
}

// TestRunWaitTimeout pins the timeout error: it must name the subject, say it
// is not necessarily a failure, and hand back a recheck command.
func TestRunWaitTimeout(t *testing.T) {
	cmd, _ := newWaitTestCmd(context.Background())
	err := runWait(cmd, waitOp[int]{
		Poll:     func(context.Context) (int, error) { return 0, nil },
		Done:     func(int) bool { return false },
		Interval: time.Millisecond,
		Timeout:  25 * time.Millisecond,
		Subject:  "widgets to settle on thing X",
		Success:  "Widgets settled on thing X",
		Slow:     "settling can take several minutes",
		Recheck:  "c1i widgets get X",
	})
	if err == nil {
		t.Fatal("runWait returned nil, want a timeout error")
	}
	want := "timed out after 25ms waiting for widgets to settle on thing X; " +
		"this is not necessarily a failure — settling can take several minutes, " +
		"check again later with: c1i widgets get X"
	if err.Error() != want {
		t.Errorf("timeout error =\n%q\nwant\n%q", err.Error(), want)
	}
}

// TestRunWaitPollErrorIsReturnedUnwrapped pins that a poll failure surfaces
// the Poll closure's own error unchanged, so cmd/errors.go can still classify
// it via errors.As. runWait must not add a layer of its own.
func TestRunWaitPollErrorIsReturnedUnwrapped(t *testing.T) {
	sentinel := errors.New("boom")
	cmd, _ := newWaitTestCmd(context.Background())
	err := runWait(cmd, waitOp[int]{
		Poll:     func(context.Context) (int, error) { return 0, fmt.Errorf("API error: %w", sentinel) },
		Done:     func(int) bool { return true },
		Interval: time.Millisecond,
		Timeout:  time.Second,
		Subject:  "s",
		Success:  "Done",
		Slow:     "slow",
		Recheck:  "c1i check",
	})
	if err == nil || err.Error() != "API error: boom" {
		t.Fatalf("err = %v, want \"API error: boom\"", err)
	}
	if !errors.Is(err, sentinel) {
		t.Error("poll error lost its %w chain; cmd/errors.go could not classify it")
	}
}

// TestRunWaitCanceled pins that a canceled parent context is reported as a
// cancellation, not as a timeout -- the two mean different things to a script.
func TestRunWaitCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd, _ := newWaitTestCmd(ctx)
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	err := runWait(cmd, waitOp[int]{
		Poll:     func(context.Context) (int, error) { return 0, nil },
		Done:     func(int) bool { return false },
		Interval: 5 * time.Millisecond,
		Timeout:  10 * time.Second,
		Subject:  "widgets to settle on thing X",
		Success:  "Widgets settled on thing X",
		Slow:     "slow",
		Recheck:  "c1i widgets get X",
	})
	want := "canceled while waiting for widgets to settle on thing X"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// TestRunWaitCallsDoneOncePerPoll is what makes a stateful predicate safe:
// untilStable counts consecutive reads, so a second Done call on the same
// poll would inflate the streak and settle early.
func TestRunWaitCallsDoneOncePerPoll(t *testing.T) {
	cmd, _ := newWaitTestCmd(context.Background())
	var polls, dones int
	_ = runWait(cmd, waitOp[int]{
		Poll:     func(context.Context) (int, error) { polls++; return polls, nil },
		Done:     func(int) bool { dones++; return polls >= 4 },
		Interval: time.Millisecond,
		Timeout:  10 * time.Second,
		Subject:  "s",
		Success:  "Done",
		Slow:     "slow",
		Recheck:  "c1i check",
	})
	if polls != dones {
		t.Errorf("Done called %d times for %d polls; must be once per poll", dones, polls)
	}
}

// ownerIDsServer returns an httptest server that answers GET .../ownerids
// with responses[i] on the i-th request, repeating the last one thereafter.
func ownerIDsServer(t *testing.T, wantPath string, responses []string) *httptest.Server {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, wantPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		i := int(atomic.AddInt32(&n, 1)) - 1
		if i >= len(responses) {
			i = len(responses) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[i]))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runOwnersWait drives the real set-owners wait -- fetchOwnerIDs, the shared
// client, and runWait -- against srv, at a test-speed poll interval.
func runOwnersWait(t *testing.T, srv *httptest.Server, appID string, want []string, timeout time.Duration) (string, error) {
	t.Helper()
	c := client.NewForTesting(srv.URL, srv.Client())
	op := ownersWaitOp(c, appID, want, timeout)
	op.Interval = 5 * time.Millisecond
	cmd, out := newWaitTestCmd(context.Background())
	err := runWait(cmd, op)
	return out.String(), err
}

// TestWaitForOwnersConvergesOverHTTP drives the wired end-to-end wait: the
// ownerids response starts empty and converges, and the messages must be
// exactly the ones set-owners printed before the wait loop was extracted.
func TestWaitForOwnersConvergesOverHTTP(t *testing.T) {
	srv := ownerIDsServer(t, "/api/v1/apps/app1/ownerids", []string{
		`{"userIds":[]}`,
		`{"userIds":["u1"]}`,
		`{"userIds":["u1","u2"]}`,
	})
	out, err := runOwnersWait(t, srv, "app1", []string{"u1", "u2"}, 10*time.Second)
	if err != nil {
		t.Fatalf("wait returned %v, want nil", err)
	}
	if !strings.Contains(out, "Still waiting for owners to provision on app app1 (") {
		t.Errorf("missing the progress line:\n%s", out)
	}
	if !strings.Contains(out, "Owners provisioned on app app1 after ") {
		t.Errorf("missing the success line:\n%s", out)
	}
}

// TestWaitForOwnersTimesOutOverHTTP covers the timeout path end-to-end and
// pins the recheck command the operator is handed.
func TestWaitForOwnersTimesOutOverHTTP(t *testing.T) {
	srv := ownerIDsServer(t, "/api/v1/apps/app1/ownerids", []string{`{"userIds":[]}`})
	_, err := runOwnersWait(t, srv, "app1", []string{"u1"}, 30*time.Millisecond)
	if err == nil {
		t.Fatal("wait returned nil, want a timeout error")
	}
	want := "timed out after 30ms waiting for owners to provision on app app1; " +
		"this is not necessarily a failure — provisioning can take several minutes, " +
		"check again later with: c1i apps owners app1"
	if err.Error() != want {
		t.Errorf("timeout error =\n%q\nwant\n%q", err.Error(), want)
	}
}

// TestWaitForOwnersAPIErrorOverHTTP pins that a mid-wait API failure keeps its
// "API error:" prefix and its typed cause, so it exits 4 rather than 1.
func TestWaitForOwnersAPIErrorOverHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runOwnersWait(t, srv, "app1", []string{"u1"}, 5*time.Second)
	if err == nil {
		t.Fatal("wait returned nil, want an API error")
	}
	if !strings.HasPrefix(err.Error(), "API error: ") {
		t.Errorf("error = %q, want an \"API error: \" prefix", err.Error())
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v does not unwrap to *client.APIError; exit-code classification would fall back to 1", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", apiErr.StatusCode)
	}
}

// TestWaitFlagsAreRegisteredIdentically walks the whole command tree and
// fails if any --wait pair was hand-rolled instead of going through
// addWaitFlags. The pair's help text has drifted between commands before;
// this is the guard that keeps one wording.
func TestWaitFlagsAreRegisteredIdentically(t *testing.T) {
	var walk func(*cobra.Command)
	seen := 0
	walk = func(c *cobra.Command) {
		waitFlag := c.Flags().Lookup("wait")
		timeoutFlag := c.Flags().Lookup("wait-timeout")
		if waitFlag != nil || timeoutFlag != nil {
			seen++
			path := c.CommandPath()
			if waitFlag == nil || timeoutFlag == nil {
				t.Errorf("%s declares only one of --wait/--wait-timeout; use addWaitFlags", path)
			} else {
				if !strings.HasPrefix(waitFlag.Usage, waitFlagUsagePrefix) || !strings.HasSuffix(waitFlag.Usage, waitFlagUsageSuffix) {
					t.Errorf("%s --wait usage %q was hand-rolled; use addWaitFlags", path, waitFlag.Usage)
				}
				if timeoutFlag.Usage != waitTimeoutFlagUsage {
					t.Errorf("%s --wait-timeout usage %q drifted from %q", path, timeoutFlag.Usage, waitTimeoutFlagUsage)
				}
				if d, err := time.ParseDuration(timeoutFlag.DefValue); err != nil || d <= 0 {
					t.Errorf("%s --wait-timeout default %q must be a positive duration", path, timeoutFlag.DefValue)
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	if seen == 0 {
		t.Fatal("found no --wait flags in the command tree; this guard is inert")
	}
}

// TestWaitFlagValues pins that --wait-timeout is only validated when --wait
// was actually asked for, and that a bad value is a usage error (exit 2).
func TestWaitFlagValues(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "x"}
		addWaitFlags(c, "something until it happens", 4*time.Minute)
		return c
	}

	t.Run("defaults", func(t *testing.T) {
		wait, timeout, err := waitFlagValues(newCmd())
		if err != nil || wait || timeout != 4*time.Minute {
			t.Errorf("got (%v, %v, %v), want (false, 4m0s, nil)", wait, timeout, err)
		}
	})

	t.Run("non-positive timeout without --wait is accepted", func(t *testing.T) {
		c := newCmd()
		mustSet(t, c.Flags(), "wait-timeout", "0s")
		if _, _, err := waitFlagValues(c); err != nil {
			t.Errorf("err = %v, want nil (no --wait, so the timeout is unused)", err)
		}
	})

	for _, bad := range []string{"0s", "-1s"} {
		t.Run("--wait with "+bad, func(t *testing.T) {
			c := newCmd()
			mustSet(t, c.Flags(), "wait", "true")
			mustSet(t, c.Flags(), "wait-timeout", bad)
			_, _, err := waitFlagValues(c)
			var ue *usageError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v, want a *usageError (exit 2)", err)
			}
			if !strings.Contains(err.Error(), "--wait-timeout must be positive") {
				t.Errorf("err = %q, want it to name --wait-timeout", err.Error())
			}
		})
	}
}

func mustSet(t *testing.T, fs *pflag.FlagSet, name, value string) {
	t.Helper()
	if err := fs.Set(name, value); err != nil {
		t.Fatalf("setting --%s=%s: %v", name, value, err)
	}
}
