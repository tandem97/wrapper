// Package retry provides a wrapper that retries a function with a backoff
// between attempts until it succeeds or the attempts are exhausted.
package retry

import (
	"context"
	"time"

	"github.com/tandem97/wrapper/effector"
)

// Backoff supplies delays between retry attempts.
//
// Backoff returns the delay to wait before the next attempt, and Reset
// restarts the sequence at its initial value. Retry resets the backoff
// after every call so that each retry session starts from a clean state.
type Backoff interface {
	Backoff() time.Duration
	Reset()
}

// Retry returns a wrapper around effector that makes up to retries
// attempts (retries must be at least 1), waiting backoff.Backoff() between
// failed attempts. It returns the first successful (T, error) pair, or the
// error of the last attempt when all attempts fail.
func Retry[T any](effector effector.ValueError[T], retries int, backoff Backoff) effector.ValueError[T] {
	retry := RetryContext(effector.ValueErrorContext(), retries, backoff)

	return func() (T, error) {
		return retry(context.Background())
	}
}

// RetryContext is like Retry but passes ctx to effector on every attempt
// and interrupts the backoff wait when ctx is done, returning ctx.Err().
func RetryContext[T any](effector effector.ValueErrorContext[T], retries int, backoff Backoff) effector.ValueErrorContext[T] {
	if retries <= 0 {
		panic("retry: retries must be at least 1")
	}

	return func(ctx context.Context) (res T, err error) {
		defer backoff.Reset()

		for r := 0; ; r++ {
			res, err = effector(ctx)
			if err == nil || r >= retries-1 {
				return res, err
			}

			timer := time.NewTimer(backoff.Backoff())

			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}

				err = ctx.Err()

				return
			}
		}
	}
}
