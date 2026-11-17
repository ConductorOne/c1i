package timing

import (
	"context"
	"testing"
	"time"
)

// Deliberately no testenv TestMain: this pins the production default.
func TestProductionDefaultIsUnscaled(t *testing.T) {
	if Divisor != 1 || Scale(7*time.Second) != 7*time.Second {
		t.Fatalf("Divisor = %d, Scale(7s) = %v; production must not scale waits", Divisor, Scale(7*time.Second))
	}
}

func TestDivisorShortensSleepAndCtxStillWins(t *testing.T) {
	defer func(d time.Duration) { Divisor = d }(Divisor)
	Divisor = 1000
	if got := Scale(time.Second); got != time.Millisecond {
		t.Fatalf("Scale(1s) = %v, want 1ms", got)
	}
	start := time.Now()
	if err := Sleep(context.Background(), 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("Sleep ignored Divisor")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Sleep(ctx, time.Hour); err != context.Canceled {
		t.Fatalf("Sleep on a cancelled ctx = %v", err)
	}
}

func TestPositiveWaitNeverScalesToZero(t *testing.T) {
	defer func(d time.Duration) { Divisor = d }(Divisor)
	Divisor = 1000
	for _, d := range []time.Duration{1, time.Microsecond, 500 * time.Microsecond, 999 * time.Microsecond} {
		if got := Scale(d); got < time.Millisecond {
			t.Errorf("Scale(%v) = %v, below the 1ms floor", d, got)
		}
	}
	if got := Scale(0); got != 0 {
		t.Errorf("Scale(0) = %v, want 0", got)
	}
	Divisor = 1
	if got := Scale(time.Microsecond); got != time.Microsecond {
		t.Errorf("production Scale(1us) = %v, want unchanged", got)
	}
}
