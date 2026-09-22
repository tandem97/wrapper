package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// stubBackoff is a scripted Backoff implementation for tests.
type stubBackoff struct {
	delays []time.Duration
	idx    int
	resets int
}

func (b *stubBackoff) Backoff() time.Duration {
	if b.idx >= len(b.delays) {
		return b.delays[len(b.delays)-1]
	}

	d := b.delays[b.idx]
	b.idx++

	return d
}

func (b *stubBackoff) Reset() { b.resets++ }

func TestBreakerOpensAfterThreshold(t *testing.T) {
	var (
		calls   int
		circuit = func() (int, error) {
			calls++

			return 0, errors.New("boom")
		}
		b = Breaker(circuit, 2, &stubBackoff{delays: []time.Duration{time.Hour}})
	)

	// Two failures open the breaker.
	for i := 0; i < 2; i++ {
		if _, err := b(); err == nil {
			t.Fatal("expected error from circuit")
		}
	}

	// The breaker is now open: the next call fails fast.
	if _, err := b(); !errors.Is(err, ErrServiceUnreachable) {
		t.Fatalf("expected ErrServiceUnreachable, got %v", err)
	}

	if calls != 2 {
		t.Fatalf("circuit invoked %d times, want 2", calls)
	}
}

func TestBreakerProbeSucceedsClosesBreaker(t *testing.T) {
	var (
		calls   int
		circuit = func() (string, error) {
			calls++

			if calls == 1 {
				return "", errors.New("boom")
			}

			return "ok", nil
		}
		backoff = &stubBackoff{delays: []time.Duration{time.Millisecond}}
		b       = Breaker(circuit, 1, backoff)
	)

	if _, err := b(); err == nil {
		t.Fatal("expected error from circuit")
	}

	time.Sleep(5 * time.Millisecond) // let the open window elapse

	// The probe succeeds and closes the breaker.
	if _, err := b(); err != nil {
		t.Fatalf("expected probe success, got %v", err)
	}

	if backoff.resets != 1 {
		t.Fatalf("backoff reset %d times, want 1", backoff.resets)
	}

	// The breaker is closed: the circuit runs normally again.
	if _, err := b(); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if calls != 3 {
		t.Fatalf("circuit invoked %d times, want 3", calls)
	}
}

func TestBreakerProbeFailsReopens(t *testing.T) {
	var (
		calls   int
		circuit = func() (int, error) {
			calls++

			return 0, errors.New("boom")
		}
		b = Breaker(circuit, 1, &stubBackoff{delays: []time.Duration{time.Millisecond}})
	)

	// Open the breaker.
	if _, err := b(); err == nil {
		t.Fatal("expected error")
	}

	time.Sleep(5 * time.Millisecond) // let the open window elapse

	// The probe fails: the breaker opens again.
	if _, err := b(); err == nil {
		t.Fatal("expected probe failure")
	}

	// Still within the new open window: fails fast.
	if _, err := b(); !errors.Is(err, ErrServiceUnreachable) {
		t.Fatalf("expected ErrServiceUnreachable, got %v", err)
	}

	if calls != 2 {
		t.Fatalf("circuit invoked %d times, want 2", calls)
	}
}

func TestBreakerThresholdZero(t *testing.T) {
	var (
		calls   int
		circuit = func() (int, error) {
			calls++

			return 0, errors.New("boom")
		}
		b = Breaker(circuit, 0, &stubBackoff{delays: []time.Duration{time.Hour}})
	)

	// The first call still invokes the circuit.
	if _, err := b(); err == nil {
		t.Fatal("expected error")
	}

	// A threshold of 0 opens the breaker after the first failure.
	if _, err := b(); !errors.Is(err, ErrServiceUnreachable) {
		t.Fatalf("expected ErrServiceUnreachable, got %v", err)
	}

	if calls != 1 {
		t.Fatalf("circuit invoked %d times, want 1", calls)
	}
}

func TestBreakerPanicsOnNegativeThreshold(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Breaker did not panic")
		}
	}()

	Breaker(func() (int, error) { return 0, nil }, -1, &stubBackoff{delays: []time.Duration{time.Hour}})
}

func TestBreakerConcurrent(t *testing.T) {
	var (
		calls   int
		mu      sync.Mutex
		wg      sync.WaitGroup
		circuit = func() (int, error) {
			mu.Lock()
			calls++
			mu.Unlock()

			return 0, errors.New("boom")
		}
		b          = Breaker(circuit, 5, &stubBackoff{delays: []time.Duration{time.Hour}})
		goroutines = 16
	)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < 100; i++ {
				_, _ = b()
			}
		}()
	}

	wg.Wait()
}

func TestBreakerContextCancellationNotCounted(t *testing.T) {
	var (
		calls   int
		circuit = func(ctx context.Context) (int, error) {
			calls++

			return 0, ctx.Err()
		}
		b = BreakerContext(circuit, 2, &stubBackoff{delays: []time.Duration{time.Hour}})
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Cancellations are passed through but never open the breaker.
	for i := 0; i < 5; i++ {
		if _, err := b(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("call %d: got %v, want context.Canceled", i+1, err)
		}
	}

	if calls != 5 {
		t.Fatalf("circuit invoked %d times, want 5", calls)
	}
}

func TestBreakerCountsRealFailures(t *testing.T) {
	var (
		calls   int
		circuit = func(context.Context) (int, error) {
			calls++

			return 0, errors.New("boom")
		}
		b = BreakerContext(circuit, 2, &stubBackoff{delays: []time.Duration{time.Hour}})
	)

	for i := 0; i < 2; i++ {
		if _, err := b(context.Background()); err == nil {
			t.Fatal("expected error from circuit")
		}
	}

	// The breaker is open: the next call fails fast.
	if _, err := b(context.Background()); !errors.Is(err, ErrServiceUnreachable) {
		t.Fatalf("expected ErrServiceUnreachable, got %v", err)
	}

	if calls != 2 {
		t.Fatalf("circuit invoked %d times, want 2", calls)
	}
}

func TestBreakerResetsCounterOnSuccess(t *testing.T) {
	var (
		calls   int
		circuit = func() (int, error) {
			calls++

			if calls%3 == 0 {
				return 1, nil
			}

			return 0, errors.New("boom")
		}
		b = Breaker(circuit, 2, &stubBackoff{delays: []time.Duration{time.Millisecond}})
	)

	// fail, fail → open
	if _, err := b(); err == nil {
		t.Fatal("expected error")
	}

	if _, err := b(); err == nil {
		t.Fatal("expected error")
	}

	time.Sleep(5 * time.Millisecond) // let the open window elapse

	// The probe succeeds and resets the counter.
	if _, err := b(); err != nil {
		t.Fatalf("expected probe success, got %v", err)
	}

	// fail, fail → open again
	if _, err := b(); err == nil {
		t.Fatal("expected error")
	}

	if _, err := b(); err == nil {
		t.Fatal("expected error")
	}

	time.Sleep(5 * time.Millisecond)

	// The probe succeeds again.
	if _, err := b(); err != nil {
		t.Fatalf("expected probe success, got %v", err)
	}

	if calls != 6 {
		t.Fatalf("circuit invoked %d times, want 6", calls)
	}
}

func TestBreakerBackoffResetOnSuccess(t *testing.T) {
	backoff := &stubBackoff{delays: []time.Duration{time.Hour}}
	b := Breaker(func() (int, error) { return 1, nil }, 2, backoff)

	if _, err := b(); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if backoff.resets != 1 {
		t.Fatalf("backoff reset %d times, want 1", backoff.resets)
	}
}

func TestBreakerContextPassesContext(t *testing.T) {
	got := make(chan context.Context, 1)
	circuit := func(ctx context.Context) (int, error) {
		got <- ctx

		return 1, nil
	}
	b := BreakerContext(circuit, 2, &stubBackoff{delays: []time.Duration{time.Hour}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := b(ctx); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if c := <-got; c != ctx {
		t.Fatal("circuit did not receive the provided context")
	}
}

func TestBreakerReturnsCircuitError(t *testing.T) {
	boom := errors.New("boom")
	b := Breaker(func() (int, error) { return 0, boom }, 3, &stubBackoff{delays: []time.Duration{time.Hour}})

	_, err := b()
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}
}

func TestBreakerSingleProbe(t *testing.T) {
	var (
		calls   int
		mu      sync.Mutex
		started = make(chan struct{})
		release = make(chan struct{})
		circuit = func() (int, error) {
			mu.Lock()
			calls++
			mu.Unlock()

			if calls == 1 {
				return 0, errors.New("boom")
			}

			close(started)
			<-release

			return 1, nil
		}
		b = Breaker(circuit, 1, &stubBackoff{delays: []time.Duration{time.Millisecond}})
	)

	// Open the breaker.
	if _, err := b(); err == nil {
		t.Fatal("expected error")
	}

	time.Sleep(5 * time.Millisecond) // let the open window elapse

	// Start a probe.
	probeDone := make(chan struct{})

	go func() {
		defer close(probeDone)

		_, _ = b()
	}()

	<-started

	// While the probe is in flight, other calls fail fast.
	if _, err := b(); !errors.Is(err, ErrServiceUnreachable) {
		t.Fatalf("expected ErrServiceUnreachable while probing, got %v", err)
	}

	close(release)
	<-probeDone

	mu.Lock()
	defer mu.Unlock()

	if calls != 2 { // 1 to open the breaker + 1 probe
		t.Fatalf("circuit invoked %d times, want 2", calls)
	}
}
