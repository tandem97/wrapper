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
// is down.
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
// with Reset after a successful probe.
type Backoff interface {
	// Backoff returns the delay until the next probe is allowed
	Backoff() time.Duration

	// Reset restarts the sequence so that the next Backoff call returns
	// the initial delay.
	Reset()
}

// ErrServiceUnreachable is returned by an open breaker instead of calling
// the wrapped circuit.
var ErrServiceUnreachable = errors.New("service unreachable")

// Breaker wraps circuit with a circuit breaker and returns a function with
// the same signature. The returned function may be called concurrently.
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
// after the first failure. Context errors returned by the circuit are
// passed through to the caller but are not counted as failures.
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

		if failures >= threshold {
			if probing {
				mu.Unlock()

				err = ErrServiceUnreachable

				return
			}

			probing = true
		}

		mu.Unlock()

		res, err = circuit(ctx)

		mu.Lock()
		defer mu.Unlock()

		probing = false

		if err != nil {
			// A context error means the caller cancelled the call, not
			// that the circuit failed: do not count it as a failure, so
			// caller cancellations cannot open the breaker.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}

			if failures != math.MaxInt {
				failures++
			}

			if failures < threshold {
				return
			}

			if time.Now().Before(shouldRetryAt) {
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
