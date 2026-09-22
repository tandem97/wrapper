package retry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tandem97/wrapper/backoff/exponential"
)

// stubBackoff is a scripted Backoff implementation for tests.
type stubBackoff struct {
	delays []time.Duration
	idx    int
	calls  int
	resets int
}

func (b *stubBackoff) Backoff() time.Duration {
	b.calls++

	if b.idx >= len(b.delays) {
		return b.delays[len(b.delays)-1]
	}

	d := b.delays[b.idx]
	b.idx++

	return d
}

func (b *stubBackoff) Reset() { b.resets++ }

func TestRetrySucceedsOnFirstAttempt(t *testing.T) {
	var calls int

	eff := func() (int, error) {
		calls++

		return 42, nil
	}

	backoff := &stubBackoff{delays: []time.Duration{time.Millisecond}}

	res, err := Retry(eff, 5, backoff)()
	if err != nil || res != 42 {
		t.Fatalf("got (%v, %v), want (42, nil)", res, err)
	}

	if calls != 1 {
		t.Fatalf("effector invoked %d times, want 1", calls)
	}

	if backoff.calls != 0 {
		t.Fatalf("backoff invoked %d times, want 0", backoff.calls)
	}

	if backoff.resets != 1 {
		t.Fatalf("backoff reset %d times, want 1", backoff.resets)
	}
}

func TestRetrySucceedsAfterFailures(t *testing.T) {
	var calls int

	eff := func() (string, error) {
		calls++

		if calls < 3 {
			return "", errors.New("transient")
		}

		return "ok", nil
	}

	backoff := &stubBackoff{delays: []time.Duration{time.Microsecond}}

	res, err := Retry(eff, 5, backoff)()
	if err != nil || res != "ok" {
		t.Fatalf("got (%v, %v), want (ok, nil)", res, err)
	}

	if calls != 3 {
		t.Fatalf("effector invoked %d times, want 3", calls)
	}

	if backoff.calls != 2 {
		t.Fatalf("backoff invoked %d times, want 2", backoff.calls)
	}
}

func TestRetryExhaustsAttempts(t *testing.T) {
	var calls int

	wantErr := errors.New("permanent")

	eff := func() (int, error) {
		calls++

		return 0, wantErr
	}

	backoff := &stubBackoff{delays: []time.Duration{time.Microsecond}}

	_, err := Retry(eff, 3, backoff)()
	if !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}

	if calls != 3 {
		t.Fatalf("effector invoked %d times, want 3", calls)
	}

	if backoff.calls != 2 {
		t.Fatalf("backoff invoked %d times, want 2", backoff.calls)
	}

	if backoff.resets != 1 {
		t.Fatalf("backoff reset %d times, want 1", backoff.resets)
	}
}

func TestRetryContextCancellation(t *testing.T) {
	var calls int

	eff := func(context.Context) (int, error) {
		calls++

		return 0, errors.New("transient")
	}

	ctx, cancel := context.WithCancel(context.Background())
	backoff := &stubBackoff{delays: []time.Duration{time.Hour}}

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, err := RetryContext(eff, 5, backoff)(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v, want context.Canceled", err)
		}
	}()

	// Let the first attempt fail and the backoff wait begin.
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry did not return after cancellation")
	}

	if calls != 1 {
		t.Fatalf("effector invoked %d times, want 1", calls)
	}
}

func TestRetryPanicsOnNonPositiveRetries(t *testing.T) {
	backoff := &stubBackoff{delays: []time.Duration{time.Microsecond}}

	for _, retries := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Retry with retries=%d did not panic", retries)
				}
			}()

			Retry(func() (int, error) { return 0, nil }, retries, backoff)
		}()
	}
}

func TestRetryContextPassesContext(t *testing.T) {
	got := make(chan context.Context, 1)

	eff := func(ctx context.Context) (int, error) {
		got <- ctx

		return 1, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	retrier := RetryContext(eff, 3, &stubBackoff{delays: []time.Duration{time.Microsecond}})

	if _, err := retrier(ctx); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if c := <-got; c != ctx {
		t.Fatal("effector did not receive the provided context")
	}
}

func TestRetryContextCancelledBeforeCall(t *testing.T) {
	var calls int

	eff := func(context.Context) (int, error) {
		calls++

		return 1, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	retrier := RetryContext(eff, 3, &stubBackoff{delays: []time.Duration{time.Microsecond}})

	_, err := retrier(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}

	if calls != 0 {
		t.Fatalf("effector invoked %d times, want 0", calls)
	}
}

func TestRetryConcurrent(t *testing.T) {
	var (
		calls int
		mu    sync.Mutex
		wg    sync.WaitGroup
		eff   = func() (int, error) {
			mu.Lock()
			calls++
			mu.Unlock()

			return 1, nil
		}
		retrier    = Retry(eff, 3, exponential.New(exponential.WithBase(time.Millisecond)))
		goroutines = 16
	)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			res, err := retrier()
			if err != nil || res != 1 {
				t.Errorf("got (%v, %v), want (1, nil)", res, err)
			}
		}()
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if calls != goroutines {
		t.Fatalf("effector invoked %d times, want %d", calls, goroutines)
	}
}
