package circuitbreaker

import (
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
	var calls int

	circuit := func() (int, error) {
		calls++

		return 0, errors.New("boom")
	}

	b := Breaker(circuit, 2, &stubBackoff{delays: []time.Duration{time.Hour}})

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
	var calls int

	circuit := func() (string, error) {
		calls++

		if calls == 1 {
			return "", errors.New("boom")
		}

		return "ok", nil
	}

	backoff := &stubBackoff{delays: []time.Duration{time.Millisecond}}
	b := Breaker(circuit, 1, backoff)

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
	var calls int

	circuit := func() (int, error) {
		calls++

		return 0, errors.New("boom")
	}

	b := Breaker(circuit, 1, &stubBackoff{delays: []time.Duration{time.Millisecond}})

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
	var calls int

	circuit := func() (int, error) {
		calls++

		return 0, errors.New("boom")
	}

	b := Breaker(circuit, 0, &stubBackoff{delays: []time.Duration{time.Hour}})

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
	var calls int
	var mu sync.Mutex

	circuit := func() (int, error) {
		mu.Lock()
		calls++
		mu.Unlock()

		return 0, errors.New("boom")
	}

	b := Breaker(circuit, 5, &stubBackoff{delays: []time.Duration{time.Hour}})

	const goroutines = 16

	var wg sync.WaitGroup
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