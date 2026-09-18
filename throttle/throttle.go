// Package throttle provides token-bucket throttling wrappers that limit
// how often a wrapped circuit can be called.
//
// The bucket starts full with max tokens. Every call consumes one token;
// calls that find the bucket empty fail fast with ErrTooManyCalls. A
// background goroutine refills the bucket by refill tokens every d, up to
// max, and stops when refillCtx is cancelled.
package throttle

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tandem97/wrapper/effector"
)

// ErrTooManyCalls is returned when the token bucket is empty and the call
// is throttled.
var ErrTooManyCalls = errors.New("too many calls")

// ThrottleVoid returns a throttled wrapper around effector: at most max
// calls pass through per refill period, further calls are dropped without
// invoking effector. The result of the wrapped circuit is discarded.
func ThrottleVoid(refillCtx context.Context, effector effector.Void, max uint, refill uint, d time.Duration) effector.Void {
	throttle := ThrottleContext(refillCtx, effector.ValueErrorContext(), max, refill, d)

	return func() {
		_, _ = throttle(context.Background())
	}
}

// Throttle returns a throttled wrapper around effector: at most max calls
// pass through per refill period, further calls return ErrTooManyCalls
// without invoking the circuit.
func Throttle[T any](refillCtx context.Context, effector effector.ValueError[T], max uint, refill uint, d time.Duration) effector.ValueError[T] {
	throttle := ThrottleContext(refillCtx, effector.ValueErrorContext(), max, refill, d)

	return func() (T, error) {
		return throttle(context.Background())
	}
}

// ThrottleContext returns a context-aware throttled wrapper around
// effector: at most max calls pass through per refill period, further
// calls return ErrTooManyCalls without invoking the circuit. The bucket
// is refilled by refill tokens every d by a background goroutine that
// stops when refillCtx is cancelled.
//
// It panics if d is not positive or if max or refill is zero.
func ThrottleContext[T any](refillCtx context.Context, effector effector.ValueErrorContext[T], max uint, refill uint, d time.Duration) effector.ValueErrorContext[T] {
	if d <= 0 {
		panic("throttle: d must be positive")
	}

	if max == 0 {
		panic("throttle: max must be greater than zero")
	}

	if refill == 0 {
		panic("throttle: refill must be greater than zero")
	}

	var (
		tokens = max
		once   sync.Once
		mu     sync.Mutex
	)

	return func(ctx context.Context) (res T, err error) {
		once.Do(func() {
			go func() {
				ticker := time.NewTicker(d)
				defer ticker.Stop()

				for {
					select {
					case <-refillCtx.Done():
						return

					case <-ticker.C:
						mu.Lock()

						// Saturating add: clamp to max without letting
						// tokens += refill overflow uint.
						if remaining := max - tokens; refill >= remaining {
							tokens = max
						} else {
							tokens += refill
						}

						mu.Unlock()
					}
				}
			}()
		})

		mu.Lock()

		if tokens <= 0 {
			mu.Unlock()

			err = ErrTooManyCalls

			return
		}

		tokens--

		mu.Unlock()

		return effector(ctx)
	}
}
