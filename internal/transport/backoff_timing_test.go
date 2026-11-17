package transport

import (
	"context"
	"testing"
	"time"
)

// The production schedule is computed, never slept: pin the constants and the
// doubling so the test-only wait divisor cannot hide a change to them.
func TestProductionBackoffScheduleIsPinned(t *testing.T) {
	orig := jitterFn
	jitterFn = func(d time.Duration) time.Duration { return d }
	t.Cleanup(func() { jitterFn = orig })
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}
	for i, w := range want {
		if got := nextBackoff(i, nil); got != w {
			t.Errorf("attempt %d = %v, want %v", i, got, w)
		}
	}
	if retryMinDelay != 100*time.Millisecond || retryMaxDelay != 30*time.Second {
		t.Errorf("bounds = %v..%v, want 100ms..30s", retryMinDelay, retryMaxDelay)
	}
}

// The default sleep honors the test divisor: a 30s backoff returns at once.
// With the hook ignored this would run into the 10s context.
func TestDefaultSleepHonorsTimingDivisor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sleepFn(ctx, retryMaxDelay); err != nil {
		t.Fatalf("sleepFn = %v", err)
	}
}
