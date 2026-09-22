package throttle

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestThrottleAllowsUpToMax(t *testing.T) {
	var (
		calls   int
		circuit = func() (string, error) {
			calls++

			return "ok", nil
		}
		refillCtx, stop = context.WithCancel(context.Background())
		throttled       = Throttle(refillCtx, circuit, 2, 100, time.Hour)
	)

	defer stop()

	for i := 0; i < 2; i++ {
		if _, err := throttled(); err != nil {
			t.Fatalf("call %d: unexpected error %v", i+1, err)
		}
	}

	if _, err := throttled(); !errors.Is(err, ErrTooManyCalls) {
		t.Fatalf("expected ErrTooManyCalls, got %v", err)
	}

	if calls != 2 {
		t.Fatalf("effector invoked %d times, want 2", calls)
	}
}

func TestThrottleRefills(t *testing.T) {
	var (
		calls   int
		circuit = func() (int, error) {
			calls++

			return 1, nil
		}
		refillCtx, stop = context.WithCancel(context.Background())
		throttled       = Throttle(refillCtx, circuit, 1, 1, 20*time.Millisecond)
	)

	// One token, refilled by one token every 20ms.
	defer stop()

	if _, err := throttled(); err != nil {
		t.Fatal("first call should pass")
	}

	if _, err := throttled(); !errors.Is(err, ErrTooManyCalls) {
		t.Fatalf("expected ErrTooManyCalls, got %v", err)
	}

	time.Sleep(40 * time.Millisecond) // two refill periods

	if _, err := throttled(); err != nil {
		t.Fatalf("call after refill: unexpected error %v", err)
	}

	if calls != 2 {
		t.Fatalf("effector invoked %d times, want 2", calls)
	}
}

func TestThrottleConcurrent(t *testing.T) {
	var (
		calls   int
		mu      sync.Mutex
		circuit = func() (int, error) {
			mu.Lock()
			calls++
			mu.Unlock()

			return 1, nil
		}
		refillCtx, stop = context.WithCancel(context.Background())
		max             = 5
		throttled       = Throttle(refillCtx, circuit, uint(max), 1, time.Hour)
		goroutines      = 100
		wg              sync.WaitGroup
	)

	defer stop()

	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, _ = throttled()
		}()
	}

	wg.Wait()

	if calls != max {
		t.Fatalf("effector invoked %d times, want exactly %d", calls, max)
	}
}

func TestThrottleStopsRefillOnCancellation(t *testing.T) {
	var (
		calls   int
		circuit = func() (int, error) {
			calls++

			return 1, nil
		}
		refillCtx, stop = context.WithCancel(context.Background())
		throttled       = Throttle(refillCtx, circuit, 1, 1, 10*time.Millisecond)
	)

	if _, err := throttled(); err != nil {
		t.Fatal("first call should pass")
	}

	stop() // stop the refill goroutine

	if _, err := throttled(); !errors.Is(err, ErrTooManyCalls) {
		t.Fatalf("expected ErrTooManyCalls, got %v", err)
	}

	time.Sleep(30 * time.Millisecond) // long enough for refills if alive

	if _, err := throttled(); !errors.Is(err, ErrTooManyCalls) {
		t.Fatalf("expected ErrTooManyCalls after cancellation, got %v", err)
	}

	if calls != 1 {
		t.Fatalf("effector invoked %d times, want 1", calls)
	}
}

func TestThrottlePanicsOnInvalidArgs(t *testing.T) {
	tests := []struct {
		name   string
		max    uint
		refill uint
		d      time.Duration
	}{
		{"non-positive d", 1, 1, 0},
		{"zero max", 0, 1, time.Second},
		{"zero refill", 1, 0, time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Throttle did not panic")
				}
			}()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			Throttle(ctx, func() (int, error) { return 0, nil }, tt.max, tt.refill, tt.d)
		})
	}
}

func TestThrottleSaturatesAtMax(t *testing.T) {
	var (
		calls   int
		circuit = func() (int, error) {
			calls++

			return 1, nil
		}
		refillCtx, stop = context.WithCancel(context.Background())
		throttled       = Throttle(refillCtx, circuit, 2, 5, 10*time.Millisecond)
	)

	// refill (5) exceeds max (2): the bucket must saturate at max.
	defer stop()

	// Drain the bucket.
	for i := 0; i < 2; i++ {
		if _, err := throttled(); err != nil {
			t.Fatalf("call %d: unexpected error %v", i+1, err)
		}
	}

	if _, err := throttled(); !errors.Is(err, ErrTooManyCalls) {
		t.Fatalf("expected ErrTooManyCalls, got %v", err)
	}

	// After several refill periods the bucket is full again with exactly
	// max tokens, not overflowing to refill.
	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 2; i++ {
		if _, err := throttled(); err != nil {
			t.Fatalf("call %d after refill: unexpected error %v", i+1, err)
		}
	}

	if _, err := throttled(); !errors.Is(err, ErrTooManyCalls) {
		t.Fatalf("expected ErrTooManyCalls, got %v", err)
	}

	if calls != 4 {
		t.Fatalf("effector invoked %d times, want 4", calls)
	}
}

func TestThrottleContextPassesContext(t *testing.T) {
	var (
		got     = make(chan context.Context, 1)
		circuit = func(ctx context.Context) (int, error) {
			got <- ctx

			return 1, nil
		}
		refillCtx, stop = context.WithCancel(context.Background())
		throttled       = ThrottleContext(refillCtx, circuit, 1, 1, time.Hour)
		ctx, cancel     = context.WithCancel(context.Background())
	)

	defer stop()
	defer cancel()

	if _, err := throttled(ctx); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if c := <-got; c != ctx {
		t.Fatal("effector did not receive the provided context")
	}
}
