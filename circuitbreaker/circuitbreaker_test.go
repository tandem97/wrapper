package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

// A threshold of 0 must not read as "already open". Before anything has
// failed the breaker is closed, so concurrent calls all reach the circuit
// instead of being rejected with ErrServiceUnreachable.
func TestBreakerThresholdZeroClosedAdmitsConcurrentCalls(t *testing.T) {
	const sessions = 8

	var (
		entered atomic.Int32
		gate    = make(chan struct{})
		b       = Breaker(func() (int, error) {
			entered.Add(1)
			<-gate

			return 1, nil
		}, 0, &stubBackoff{delays: []time.Duration{time.Hour}})
		wg sync.WaitGroup
	)

	for i := 0; i < sessions; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if _, err := b(); err != nil {
				t.Errorf("got %v, want the call to pass through a closed breaker", err)
			}
		}()
	}

	// Let every goroutine reach the circuit before releasing it.
	assertEventually(t, func() bool { return entered.Load() == sessions })

	close(gate)
	wg.Wait()

	if got := entered.Load(); got != sessions {
		t.Fatalf("circuit invoked %d times, want %d", got, sessions)
	}
}

// The mirror image: once the single permitted failure has happened, a
// threshold of 0 must behave exactly like any other open breaker.
func TestBreakerThresholdZeroOpensAfterFirstFailure(t *testing.T) {
	var (
		calls   atomic.Int32
		circuit = func() (int, error) {
			calls.Add(1)

			return 0, errors.New("boom")
		}
		b = Breaker(circuit, 0, &stubBackoff{delays: []time.Duration{time.Hour}})
	)

	_, _ = b() // the one failure that opens the breaker

	if _, err := b(); !errors.Is(err, ErrServiceUnreachable) {
		t.Fatalf("got %v, want ErrServiceUnreachable", err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("circuit invoked %d times, want 1", got)
	}
}

// assertEventually waits for cond, failing the test if it never holds.
func assertEventually(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("condition never held")
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

	// The burst itself is not throttled, since no call completed while
	// the others were in flight, but the breaker must have opened by the
	// time they all returned.
	if _, err := b(); !errors.Is(err, ErrServiceUnreachable) {
		t.Fatalf("got %v, want the breaker to be open after the burst", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if calls == 0 {
		t.Fatal("circuit was never invoked")
	}
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
	var (
		backoff = &stubBackoff{delays: []time.Duration{time.Hour}}
		b       = Breaker(func() (int, error) { return 1, nil }, 2, backoff)
	)

	if _, err := b(); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if backoff.resets != 1 {
		t.Fatalf("backoff reset %d times, want 1", backoff.resets)
	}
}

func TestBreakerContextPassesContext(t *testing.T) {
	var (
		got     = make(chan context.Context, 1)
		circuit = func(ctx context.Context) (int, error) {
			got <- ctx

			return 1, nil
		}
		b = BreakerContext(circuit, 2, &stubBackoff{delays: []time.Duration{time.Hour}})
	)

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
	var (
		boom = errors.New("boom")
		b    = Breaker(func() (int, error) { return 0, boom }, 3, &stubBackoff{delays: []time.Duration{time.Hour}})
	)

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

// A call that started while the breaker was closed holds no probe flag,
// and must not clear one that a real probe is holding. Otherwise a slow
// normal call would admit a second probe alongside the first.
func TestBreakerProbeGateSurvivesLateCall(t *testing.T) {
	const dur = 5 * time.Millisecond

	var (
		mu         sync.Mutex
		entries    int
		admitted   atomic.Int32
		probesSeen atomic.Int32
		lateIn     = make(chan struct{})
		probeIn    = make(chan struct{}, 16)
		gateLate   = make(chan struct{})
		gateProbe  = make(chan struct{})
	)

	// The late call fails, so it does not reset the failure count and the
	// probe flag is the only thing under test.
	circuit := func() (int, error) {
		mu.Lock()
		entries++

		switch entries {
		case 1:
			mu.Unlock()
			admitted.Add(1)
			close(lateIn)
			<-gateLate

			return 0, errors.New("boom")

		case 2:
			mu.Unlock()
			admitted.Add(1)

			return 0, errors.New("boom")

		default:
			mu.Unlock()
			admitted.Add(1)
			probesSeen.Add(1)

			probeIn <- struct{}{}

			<-gateProbe

			return 0, errors.New("boom")
		}
	}

	b := Breaker(circuit, 1, &stubBackoff{delays: []time.Duration{dur}})

	// Always release a probe, so a regression surfaces as a wrong count
	// rather than as a hung test.
	defer close(gateProbe)

	// A call in flight while the breaker is still closed.
	go func() { _, _ = b() }()

	<-lateIn

	// Another call fails and opens the breaker.
	_, _ = b()

	// Once the window elapses, the real probe goes through.
	time.Sleep(2 * dur)

	go func() { _, _ = b() }()

	<-probeIn

	// The late call finishes while the probe is in flight.
	close(gateLate)
	time.Sleep(2 * dur)

	// A second call must be turned away, not admitted as another probe.
	// Run it in the background: if the gate is wrongly cleared it reaches
	// the circuit and parks there, and the deferred close lets it out.
	turned := make(chan error, 1)

	go func() { _, err := b(); turned <- err }()

	select {
	case err := <-turned:
		if !errors.Is(err, ErrServiceUnreachable) {
			t.Fatalf("got %v, want ErrServiceUnreachable: a second probe was admitted", err)
		}
	case <-time.After(2 * dur):
		// Still parked, which means the gate held it: the definitive
		// check is the count below.
	}

	// A single probe means exactly one call reached the circuit while the
	// gate was held. Two would mean the late call cleared it.
	if got := probesSeen.Load(); got != 1 {
		t.Fatalf("%d calls reached the circuit while probing, want 1", got)
	}
}

// A probe that the caller cancelled told us nothing about the circuit, so
// it must re-arm the open window. Otherwise the next call becomes a probe
// straight away, and a client that cancels on a deadline drives the circuit
// at full rate with the breaker open.
func TestBreakerCancelledProbeReArmsWindow(t *testing.T) {
	const window = 20 * time.Millisecond

	var (
		mu     sync.Mutex
		probes int
	)

	// The first call fails and opens the breaker; every probe is cancelled.
	circuit := func() (int, error) {
		mu.Lock()
		probes++
		which := probes
		mu.Unlock()

		if which == 1 {
			return 0, errors.New("boom")
		}

		return 0, context.Canceled
	}

	b := Breaker(circuit, 1, &stubBackoff{delays: []time.Duration{window}})

	_, _ = b() // opens the breaker

	time.Sleep(window)

	// Hammer the breaker for a fraction of the window. At most one probe
	// may reach the circuit: the window must not have expired again. The
	// margin keeps the assertion off the exact boundary, so a loaded
	// machine cannot turn a correct run into a failure.
	deadline := time.Now().Add(window / 4)
	for time.Now().Before(deadline) {
		_, _ = b()
	}

	mu.Lock()
	reached := probes
	mu.Unlock()

	// One failure to open, one probe allowed inside the window.
	if reached != 2 {
		t.Fatalf("circuit invoked %d times, want 2: a cancelled probe did not re-arm the window", reached)
	}
}

// A cancelled ordinary call must not re-arm anything: it holds no probe
// flag, so the window stays expired and the next call probes normally.
func TestBreakerCancelledOrdinaryCallLeavesWindowAlone(t *testing.T) {
	var (
		mu     sync.Mutex
		probes int
	)

	circuit := func() (int, error) {
		mu.Lock()
		probes++
		mu.Unlock()

		return 0, context.Canceled
	}

	b := Breaker(circuit, 2, &stubBackoff{delays: []time.Duration{time.Millisecond}})

	for i := 0; i < 5; i++ {
		if _, err := b(); !errors.Is(err, context.Canceled) {
			t.Fatalf("call %d: got %v, want context.Canceled", i+1, err)
		}
	}

	// The breaker never tripped: every call was an ordinary call, so
	// nothing was re-armed and every one of them reached the circuit.
	mu.Lock()
	reached := probes
	mu.Unlock()

	if reached != 5 {
		t.Fatalf("circuit invoked %d times, want 5", reached)
	}
}
