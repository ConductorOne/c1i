// Package timing holds the one test-only knob for real-time waits: login's
// poll interval and the transport's retry backoff.
package timing

import (
	"context"
	"time"
)

// Divisor shrinks every wait that goes through Scale or Sleep. It is 1 in
// production and is set only by internal/testenv, before any test runs.
var Divisor time.Duration = 1

// minScaled keeps a positive wait positive once shrunk, so a poll or retry
// loop never degenerates into a busy loop.
const minScaled = time.Millisecond

// Scale returns d as slept: unchanged in production, otherwise d/Divisor but
// never below minScaled for a positive d.
func Scale(d time.Duration) time.Duration {
	if Divisor <= 1 || d <= 0 {
		return d
	}
	return max(d/Divisor, minScaled)
}

// Sleep waits Scale(d) or until ctx ends, returning ctx.Err() in that case.
func Sleep(ctx context.Context, d time.Duration) error {
	d = Scale(d)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
