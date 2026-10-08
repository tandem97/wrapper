// Package circuitbreaker provides a circuit breaker that wraps a function
// and stops calling it while it keeps failing.
//
// The breaker counts consecutive failures of the wrapped circuit. Once
// threshold failures happen in a row, the breaker opens: every following
// call fails fast with ErrServiceUnreachable without invoking the circuit
// until the delay returned by the configured Backoff has elapsed. After
// that the breaker allows a single probe request through (the half-open
// state). If the probe succeeds, the failure counter and the backoff are
// reset and normal calls resume; if it fails, the breaker opens again for
// the next backoff delay.
//
// Context errors (context.Canceled and context.DeadlineExceeded) returned
// by the circuit are passed through to the caller but are not counted as
// failures: they mean the caller cancelled the call, not that the circuit
// is down. A cancelled probe re-arms the open window, since it yielded no
// information about the circuit; a cancelled ordinary call changes nothing.
//
// The breaker reacts to failures it has already observed, so it is not an
// admission controller. A burst of concurrent calls reaches the circuit in
// full while none of them has completed yet, whatever the threshold is.
// What the threshold bounds is how many consecutive completed failures it
// takes to open the breaker.
//
// Two further consequences of that design are worth knowing:
//
// A success returned by a call that started before the breaker opened
// resets the failure count, so such a stale success re-closes a breaker
// that has since tripped. Recovering from this needs generation
// tracking, which this implementation does not do.
//
// While a probe is in flight the gate stays closed, so a circuit that
// never returns leaves every call failing fast with
// ErrServiceUnreachable.
package circuitbreaker

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/tandem97/wrapper/effector"
)

// Backoff supplies delays between failed attempts and can be restarted
// with Reset after a successful probe. It is an alias for
// effector.Backoff, the same contract retry uses, so a single generator
// satisfies every consumer in the module.
//
// A breaker calls Backoff when it opens a window, and Reset once per
// successful call, both under its own lock, so one generator per breaker
// is enough: there is no per-call session to isolate. A probe that the
// caller cancelled also draws from Backoff, since the window is re-armed
// with whatever delay the generator returns.
type Backoff = effector.Backoff

// ErrServiceUnreachable is returned by an open breaker instead of calling
// the wrapped circuit.
var ErrServiceUnreachable = errors.New("service unreachable")

// Breaker wraps circuit with a circuit breaker and returns a function with
// the same signature. The returned function may be called concurrently.
//
// threshold is the number of consecutive failures that opens the breaker,
// not a limit on how many calls may be in flight: concurrent calls all
// reach the circuit until their failures have been counted. See the package
// documentation.
//
// It panics if threshold is negative.
func Breaker[T any](circuit effector.ValueError[T], threshold int, backoff Backoff) effector.ValueError[T] {
	breaker := BreakerContext(circuit.ValueErrorContext(), threshold, backoff)

	return func() (T, error) {
		return breaker(context.Background())
	}
}

// BreakerContext is like Breaker but wraps a context-aware circuit. The
// provided context is passed through to the circuit on every call.
//
// It panics if threshold is negative. A threshold of 0 opens the breaker
// after the first failure: until something has failed the breaker is
// closed and passes every call through, so concurrent calls are not
// serialised. Context errors returned by the circuit are passed through
// to the caller but are not counted as failures.
func BreakerContext[T any](circuit effector.ValueErrorContext[T], threshold int, backoff Backoff) effector.ValueErrorContext[T] {
	if threshold < 0 {
		panic("circuitbreaker: threshold must be non-negative")
	}

	var (
		failures      int
		shouldRetryAt time.Time
		probing       bool
		mu            sync.Mutex
	)

	return func(ctx context.Context) (res T, err error) {
		mu.Lock()

		if time.Now().Before(shouldRetryAt) {
			mu.Unlock()

			err = ErrServiceUnreachable

			return
		}

		var isProbe bool

		if failures > 0 && failures >= threshold {
			if probing {
				mu.Unlock()

				err = ErrServiceUnreachable

				return
			}

			probing, isProbe = true, true
		}

		mu.Unlock()

		res, err = circuit(ctx)

		mu.Lock()
		defer mu.Unlock()

		if isProbe {
			probing = false
		}

		if err != nil {
			// A context error means the caller cancelled the call, not that
			// the circuit failed: do not count it as a failure, so caller
			// cancellations cannot open the breaker.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				// A cancelled probe told us nothing about the circuit, so
				// re-arm the window instead of leaving it expired. Without
				// this every following call would immediately become a new
				// probe, and a client that cancels on a deadline would drive
				// the circuit at full rate with the breaker open.
				if isProbe {
					shouldRetryAt = time.Now().Add(backoff.Backoff())
				}

				return
			}

			if failures != math.MaxInt {
				failures++
			}

			if failures < threshold || time.Now().Before(shouldRetryAt) {
				return
			}

			shouldRetryAt = time.Now().Add(backoff.Backoff())

			return
		}

		failures = 0

		backoff.Reset()

		return
	}
}
