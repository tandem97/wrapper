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
	var (
		calls int
		eff   = func() (int, error) {
			calls++

			return 42, nil
		}
		backoff = &stubBackoff{delays: []time.Duration{time.Millisecond}}
	)

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

	// The call never waited, so the generator was never advanced and is
	// left untouched: no reset either.
	if backoff.resets != 0 {
		t.Fatalf("backoff reset %d times, want 0", backoff.resets)
	}
}

func TestRetrySucceedsAfterFailures(t *testing.T) {
	var (
		calls int
		eff   = func() (string, error) {
			calls++

			if calls < 3 {
				return "", errors.New("transient")
			}

			return "ok", nil
		}
		backoff = &stubBackoff{delays: []time.Duration{time.Microsecond}}
	)

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
	var (
		calls   int
		wantErr = errors.New("permanent")
		eff     = func() (int, error) {
			calls++

			return 0, wantErr
		}
		backoff = &stubBackoff{delays: []time.Duration{time.Microsecond}}
	)

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
	var (
		calls int
		eff   = func(context.Context) (int, error) {
			calls++

			return 0, errors.New("transient")
		}
		ctx, cancel = context.WithCancel(context.Background())
		backoff     = &stubBackoff{delays: []time.Duration{time.Hour}}
		done        = make(chan struct{})
	)

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
	var (
		got = make(chan context.Context, 1)
		eff = func(ctx context.Context) (int, error) {
			got <- ctx

			return 1, nil
		}
		ctx, cancel = context.WithCancel(context.Background())
		retrier     = RetryContext(eff, 3, &stubBackoff{delays: []time.Duration{time.Microsecond}})
	)

	defer cancel()

	if _, err := retrier(ctx); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if c := <-got; c != ctx {
		t.Fatal("effector did not receive the provided context")
	}
}

func TestRetryContextCancelledBeforeCall(t *testing.T) {
	var (
		calls int
		eff   = func(context.Context) (int, error) {
			calls++

			return 1, nil
		}
		ctx, cancel = context.WithCancel(context.Background())
		retrier     = RetryContext(eff, 3, &stubBackoff{delays: []time.Duration{time.Microsecond}})
	)

	cancel()

	_, err := retrier(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}

	if calls != 0 {
		t.Fatalf("effector invoked %d times, want 0", calls)
	}
}

// firstDelayRecorder collects the first delay of every sequence handed
// out, so sessions sharing one position would be visible.
type firstDelayRecorder struct {
	mu     sync.Mutex
	firsts []time.Duration
}

func (r *firstDelayRecorder) record(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.firsts = append(r.firsts, d)
}

func (r *firstDelayRecorder) seen() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]time.Duration(nil), r.firsts...)
}

// recordingBackoff is a cloneable generator that reports the first delay
// of every sequence it hands out to a shared recorder.
type recordingBackoff struct {
	recorder *firstDelayRecorder
	base     time.Duration
	step     time.Duration

	mu       sync.Mutex
	current  time.Duration
	reported bool
}

func (b *recordingBackoff) Backoff() time.Duration {
	b.mu.Lock()

	if !b.reported {
		b.reported = true

		b.recorder.record(b.current)
	}

	d := b.current
	b.current += b.step

	b.mu.Unlock()

	return d
}

func (b *recordingBackoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.current = b.base
}

func (b *recordingBackoff) Clone() Backoff {
	return &recordingBackoff{
		recorder: b.recorder,
		base:     b.base,
		step:     b.step,
		current:  b.base,
	}
}

func TestRetryConcurrentSessionsEscalateIndependently(t *testing.T) {
	// Every session must start at base. Without a per-call clone the
	// sessions would consume one another's steps, so only the first
	// would see base and the rest would start further up the sequence.
	recorder := &firstDelayRecorder{}

	const step = 10 * time.Millisecond

	backoff := &recordingBackoff{base: step, step: step, recorder: recorder}

	failing := func() (int, error) { return 0, errors.New("transient") }
	retrier := Retry(failing, 4, backoff)

	const sessions = 8

	var wg sync.WaitGroup

	for i := 0; i < sessions; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, _ = retrier()
		}()
	}

	wg.Wait()

	firsts := recorder.seen()

	if len(firsts) != sessions {
		t.Fatalf("%d sessions reported a first delay, want %d", len(firsts), sessions)
	}

	for i, d := range firsts {
		if d != step {
			t.Fatalf("session %d started at %v, want %v: sessions share one sequence", i, d, step)
		}
	}
}

func TestRetryUsesBackoffAsIsWithoutClone(t *testing.T) {
	// A backoff that cannot be cloned is shared, and retry must still
	// use it rather than fail.
	backoff := &stubBackoff{delays: []time.Duration{time.Microsecond}}

	failing := func() (int, error) { return 0, errors.New("transient") }
	retrier := Retry(failing, 2, backoff)

	_, _ = retrier()

	if backoff.calls == 0 {
		t.Fatal("non-cloneable backoff was not used")
	}
}

func TestRetryClonesBackoffPerCall(t *testing.T) {
	// A cloneable backoff must be cloned once per call, so the original
	// is never advanced and never reset by a retry session.
	backoff := exponential.New(exponential.WithBase(10*time.Millisecond), exponential.WithCap(40*time.Millisecond))

	failing := func() (int, error) { return 0, errors.New("transient") }
	retrier := Retry(failing, 3, backoff)

	for i := 0; i < 3; i++ {
		_, _ = retrier()
	}

	// The original generator is still at base: the sessions worked on
	// their own clones.
	if got, want := backoff.Backoff(), 10*time.Millisecond; got != want {
		t.Fatalf("original generator advanced to %v, want %v", got, want)
	}
}

// countingBackoff records how often it is cloned, asked for a delay and
// reset, with the counters shared across clones so a whole session is
// visible at once.
type countingBackoff struct {
	counts *backoffCounts
}

type backoffCounts struct {
	mu      sync.Mutex
	clones  int
	backofs int
	resets  int
}

func (c *backoffCounts) stats() (clones, backoffs, resets int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.clones, c.backofs, c.resets
}

func (b *countingBackoff) Backoff() time.Duration {
	b.counts.mu.Lock()
	defer b.counts.mu.Unlock()

	b.counts.backofs++

	return time.Millisecond
}

func (b *countingBackoff) Reset() {
	b.counts.mu.Lock()
	defer b.counts.mu.Unlock()

	b.counts.resets++
}

func (b *countingBackoff) Clone() Backoff {
	b.counts.mu.Lock()
	b.counts.clones++
	b.counts.mu.Unlock()

	return &countingBackoff{counts: b.counts}
}

// A session must clone at most once: the copy is what the escalation runs
// on, and cloning again per attempt would discard the progress made so
// far. A call that never fails must not clone at all.
func TestRetryClonesOncePerCall(t *testing.T) {
	tests := []struct {
		name         string
		retries      int
		fail         bool
		wantClones   int
		wantBackoffs int
		wantResets   int
	}{
		{"succeeds on first attempt", 5, false, 0, 0, 0},
		{"fails once", 5, true, 1, 4, 1},
		{"exhausts attempts", 3, true, 1, 2, 1},
		{"single attempt", 1, true, 0, 0, 0},
		{"two attempts", 2, true, 1, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts := &backoffCounts{}

			var (
				ok    = func() (int, error) { return 1, nil }
				fail  = func() (int, error) { return 0, errors.New("transient") }
				effer = ok
			)

			if tt.fail {
				effer = fail
			}

			_, _ = Retry(effer, tt.retries, &countingBackoff{counts: counts})()

			clones, backoffs, resets := counts.stats()

			if clones != tt.wantClones {
				t.Errorf("Clone called %d times, want %d", clones, tt.wantClones)
			}

			if backoffs != tt.wantBackoffs {
				t.Errorf("Backoff called %d times, want %d", backoffs, tt.wantBackoffs)
			}

			if resets != tt.wantResets {
				t.Errorf("Reset called %d times, want %d", resets, tt.wantResets)
			}
		})
	}
}

// The clone must be per call, not per wrapper: a second call needs a fresh
// sequence of its own.
func TestRetryClonesOncePerCallNotPerWrapper(t *testing.T) {
	counts := &backoffCounts{}
	fail := func() (int, error) { return 0, errors.New("transient") }
	retrier := Retry(fail, 3, &countingBackoff{counts: counts})

	for i := 0; i < 3; i++ {
		_, _ = retrier()
	}

	clones, backoffs, resets := counts.stats()

	if clones != 3 {
		t.Errorf("Clone called %d times over 3 calls, want 3", clones)
	}

	if backoffs != 6 {
		t.Errorf("Backoff called %d times over 3 calls, want 6", backoffs)
	}

	if resets != 3 {
		t.Errorf("Reset called %d times over 3 calls, want 3", resets)
	}
}

// base is the configured base of the generator these tests build, and the
// position it must return to after a call.
const base = time.Millisecond

// Behaviour of the wrapped function, used to shape a session.
const (
	modeSucceed = "succeed"
	modeFailOne = "failOnce"
	modeFailAll = "alwaysFail"
)

// The wrapper resets the generator once it has actually waited on it, which
// is what makes the generator reusable for the next call. A call that never
// waited leaves the generator alone, so it must not be reset either.
func TestRetryResetsBackoffAfterWaiting(t *testing.T) {
	tests := []struct {
		name       string
		retries    int
		mode       string
		cancelLate bool
		wantReset  bool
	}{
		{"succeeds on first attempt", 3, modeSucceed, false, false},
		{"single attempt fails", 1, modeFailAll, false, false},
		{"waits then succeeds", 3, modeFailOne, false, true},
		{"exhausts every attempt", 3, modeFailAll, false, true},
		{"cancelled while waiting", 3, modeFailAll, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backoff := exponential.New(
				exponential.WithBase(base),
				exponential.WithCap(time.Hour),
			)

			// Advance the generator first. Without this the next Backoff
			// would return the base anyway and the check could not tell a
			// reset from a generator that was never moved.
			advance(t, backoff, 3)

			calls := 0
			effer := func() (int, error) {
				calls++

				switch tt.mode {
				case modeSucceed:
					return 1, nil
				case modeFailOne:
					if calls == 1 {
						return 0, errors.New("transient")
					}

					return 1, nil
				default:
					return 0, errors.New("transient")
				}
			}

			if tt.cancelLate {
				// Cancel while the wrapper sits in its backoff wait.
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				defer cancel()

				_, _ = RetryContext(
					func(context.Context) (int, error) { return 0, errors.New("transient") },
					tt.retries,
					backoff,
				)(ctx)
			} else {
				_, _ = Retry(effer, tt.retries, backoff)()
			}

			got := backoff.Backoff()

			if tt.wantReset {
				if got != base {
					t.Fatalf("backoff at %v after waiting, want the base %v", got, base)
				}

				return
			}

			if got == base {
				t.Fatal("backoff was reset although the call never waited")
			}
		})
	}

	t.Run("context cancelled before the call", func(t *testing.T) {
		backoff := exponential.New(
			exponential.WithBase(base),
			exponential.WithCap(time.Hour),
		)

		advance(t, backoff, 3)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		retrier := RetryContext(func(context.Context) (int, error) { return 1, nil }, 3, backoff)

		if _, err := retrier(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}

		if got := backoff.Backoff(); got == base {
			t.Fatal("backoff was reset although the call never waited")
		}
	})
}

// advance moves b n steps past its base, so that a later read of the base
// can only mean the generator was reset.
func advance(t *testing.T, b *exponential.Backoff, n int) {
	t.Helper()

	for i := 0; i < n; i++ {
		b.Backoff()
	}

	// Confirm the generator really moved, otherwise the assertion below
	// would pass for the wrong reason.
	if got, want := b.Backoff(), base*(2<<(n-1)); got != want {
		t.Fatalf("after advancing %d steps: at %v, want %v", n, got, want)
	}
}

// A backoff without Clone is used as-is, so the reset lands on the
// generator the caller passed in. That is documented behaviour: such a
// backoff is shared, and belongs to one wrapper at a time.
func TestRetryResetsUnclonableBackoff(t *testing.T) {
	backoff := &stubBackoff{delays: []time.Duration{time.Millisecond}}
	fail := func() (int, error) { return 0, errors.New("transient") }

	_, _ = Retry(fail, 2, backoff)()

	if backoff.resets != 1 {
		t.Fatalf("Reset called %d times, want 1", backoff.resets)
	}

	if backoff.calls != 1 {
		t.Fatalf("Backoff called %d times, want 1", backoff.calls)
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
