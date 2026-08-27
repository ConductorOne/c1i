package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
)

// waitOp is one "poll until a condition holds" operation. Every --wait in this
// CLI is built from it, so the loop's shape -- immediate first poll, progress
// lines, and a timeout that says so without claiming failure -- exists once.
//
// The four string fields are what the loop cannot infer: it prints them, it
// does not compose English.
type waitOp[T any] struct {
	// Poll reads the current state. It must wrap its errors with %w so
	// cmd/errors.go can still classify them.
	Poll func(context.Context) (T, error)
	// Done reports whether the polled state satisfies the wait. It may be
	// stateful across calls (see untilStable), so runWait calls it exactly
	// once per poll.
	Done func(T) bool
	// Interval must be positive: time.NewTicker panics otherwise. Callers set
	// it from a package constant, so this is a programming error, not input.
	Interval time.Duration
	Timeout  time.Duration

	// Subject reads after "waiting for": "owners to provision on app X".
	Subject string
	// Success is the completion sentence stem, printed as "<Success> after 12s.".
	Success string
	// Slow says why a timeout is not necessarily a failure: "provisioning can
	// take several minutes".
	Slow string
	// Recheck is the command to suggest on timeout: "c1i apps owners X".
	Recheck string

	// Out receives the progress and success lines; nil means cmd's stdout.
	// A list command must set this to stderr, or its prose lines land in the
	// middle of the NDJSON stream a caller is piping to jq.
	Out io.Writer
}

// runWait polls op until op.Done is satisfied, op.Timeout elapses, or cmd's
// context is canceled, writing progress lines to op.Out. It returns the
// satisfying poll's value, so a caller that needs to print what it settled on
// does not have to smuggle it out of the Poll closure.
//
// A timeout is an error (so scripts can branch on it) but deliberately not
// phrased as a failure: the write it is waiting on was already accepted. It is
// a bare error, so it exits 1 -- the same code set-owners' --wait has always
// returned. Giving it a code of its own would change that, and belongs with
// the README/agents.md exit-code table, not here.
func runWait[T any](cmd *cobra.Command, op waitOp[T]) (T, error) {
	var zero T
	out := op.Out
	if out == nil {
		out = cmd.OutOrStdout()
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), op.Timeout)
	defer cancel()

	start := time.Now()
	ticker := time.NewTicker(op.Interval)
	defer ticker.Stop()

	// firstPoll suppresses the "still waiting" line on the very first check:
	// that poll happens immediately after the write, before any real waiting
	// has elapsed, so printing it there would misleadingly imply time has
	// already passed. Starting with the second poll, real time (>= one tick)
	// has actually elapsed, so the message is accurate.
	firstPoll := true
	for {
		got, err := op.Poll(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break // fall through to the timeout/cancellation report below
			}
			return zero, err
		}
		if op.Done(got) {
			_, _ = fmt.Fprintf(out, "%s after %s.\n", op.Success, time.Since(start).Round(time.Second))
			return got, nil
		}
		if !firstPoll {
			_, _ = fmt.Fprintf(out, "Still waiting for %s (%s elapsed)...\n",
				op.Subject, time.Since(start).Round(time.Second))
		}
		firstPoll = false

		select {
		case <-ctx.Done():
		case <-ticker.C:
			continue
		}
		break
	}

	if cmd.Context().Err() != nil {
		return zero, fmt.Errorf("canceled while waiting for %s", op.Subject)
	}
	return zero, fmt.Errorf(
		"timed out after %s waiting for %s; "+
			"this is not necessarily a failure — %s, "+
			"check again later with: %s",
		op.Timeout, op.Subject, op.Slow, op.Recheck)
}

// untilPresent is the Done predicate for "every wanted id has shown up".
// Extras in the polled set, ordering, and duplicates don't matter; an empty
// want list is trivially satisfied. Stateless.
func untilPresent(want []string) func([]string) bool {
	return func(got []string) bool {
		gotSet := make(map[string]struct{}, len(got))
		for _, id := range got {
			gotSet[id] = struct{}{}
		}
		for _, id := range want {
			if _, ok := gotSet[id]; !ok {
				return false
			}
		}
		return true
	}
}

// untilStable is the Done predicate for "the value stopped changing": it is
// satisfied once n consecutive polls read the same value. n <= 1 settles on
// the very first read -- a footgun, so a command exposing n as a flag should
// reject anything below 2 itself. The returned closure is stateful -- one per
// wait, called once per poll.
//
// Presence-waiting cannot express this. MCP tool discovery streams (reported
// from the field: 0 -> 40 -> 101, with pauses mid-stream), so "at least one
// tool exists" reports success on a partial result. Two equal reads is not
// enough either: a pause between batches is indistinguishable from completion,
// which is also this predicate's limit -- it is a heuristic, not a proof. Pick
// n so that (n-1) poll intervals exceed the longest mid-stream pause actually
// observed for that endpoint.
func untilStable[T comparable](n int) func(T) bool {
	var last T
	streak := 0
	return func(v T) bool {
		if streak > 0 && v == last {
			streak++
		} else {
			last = v
			streak = 1
		}
		return streak >= n
	}
}

// stableAndAtLeast gates a stability predicate behind a floor: the value must
// hold steady AND clear floor before the wait settles. It exists because
// "stopped changing" alone answers "did my write land?" with a confident,
// fast "no" -- an empty result is perfectly stable, so a query matching
// nothing settles at the first opportunity and exits 0 with no rows, which
// reads as "it did not happen" rather than "it has not happened yet".
//
// key extracts the comparable part untilStable settles on; floor is checked
// against the whole value. The stability predicate is fed on EVERY poll, before
// the floor is consulted: writing this as "floor(v) && stable(key(v))" would
// short-circuit past a stateful predicate on any poll below the floor, so a set
// that dipped below it and came back would look like it never changed.
func stableAndAtLeast[T any, K comparable](n int, key func(T) K, floor func(T) bool) func(T) bool {
	stable := untilStable[K](n)
	return func(v T) bool {
		settled := stable(key(v))
		return settled && floor(v)
	}
}

// waitTimeoutFlagUsage is --wait-timeout's help text everywhere. Shared
// because the pair's wording has drifted between commands before;
// TestWaitFlagsAreRegisteredIdentically fails CI if a command hand-rolls it.
const waitTimeoutFlagUsage = "max time to wait with --wait (e.g. 30s, 5m)"

const (
	waitFlagUsagePrefix = "block and poll "
	waitFlagUsageSuffix = ", or --wait-timeout elapses"
)

// addWaitFlags declares the --wait/--wait-timeout pair. until is the only
// per-command part of --wait's help: what is polled and what ends the wait,
// e.g. "GET .../ownerids until the requested owners appear".
func addWaitFlags(cmd *cobra.Command, until string, defaultTimeout time.Duration) {
	cmd.Flags().Bool("wait", false, waitFlagUsagePrefix+until+waitFlagUsageSuffix)
	cmd.Flags().Duration("wait-timeout", defaultTimeout, waitTimeoutFlagUsage)
}

// waitFlagValues reads the pair addWaitFlags declared. A non-positive timeout
// is only an error when --wait was actually asked for.
func waitFlagValues(cmd *cobra.Command) (bool, time.Duration, error) {
	wait, _ := cmd.Flags().GetBool("wait")
	timeout, _ := cmd.Flags().GetDuration("wait-timeout")
	if wait && timeout <= 0 {
		return false, 0, &usageError{fmt.Errorf("--wait-timeout must be positive")}
	}
	return wait, timeout, nil
}
