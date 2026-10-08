// Package retry provides a wrapper that retries a function with a backoff
// between attempts until it succeeds or the attempts are exhausted.
package retry

import (
	"context"
	"time"

	"github.com/tandem97/wrapper/effector"
)

// Backoff supplies delays between retry attempts. It is an alias for
// effector.Backoff, so generators can satisfy this interface, the one in
// circuitbreaker, and effector.Cloneable with a single Clone signature.
type Backoff = effector.Backoff

// cloneable is effector.Cloneable, restated locally because the effector
// parameter of the wrappers shadows the package name.
type cloneable interface {
	Backoff
	Clone() Backoff
}

// Retry returns a wrapper around effector that makes up to retries
// attempts, waiting backoff.Backoff() between failed attempts. It returns
// the first successful (T, error) pair, or the error of the last attempt
// when all attempts fail.
//
// Once a call has waited at least once, the backoff is reset before the
// wrapper returns, so it is reusable by the next call. A call that never
// waits leaves it untouched.
//
// It panics if retries is not positive.
func Retry[T any](effector effector.ValueError[T], retries int, backoff Backoff) effector.ValueError[T] {
	retry := RetryContext(effector.ValueErrorContext(), retries, backoff)

	return func() (T, error) {
		return retry(context.Background())
	}
}

// RetryContext is like Retry but passes ctx to effector on every attempt
// and interrupts the backoff wait when ctx is done, returning ctx.Err().
// A call with an already cancelled context returns ctx.Err() immediately
// without invoking the effector.
//
// Once a call has waited at least once, the backoff is reset before the
// wrapper returns, so it is reusable by the next call. A call that never
// waits leaves it untouched, so a generator advanced by the caller survives a
// call that succeeds immediately.
//
// When backoff implements effector.Cloneable — as both bundled generators do
// — it is cloned on the first failed attempt, so the escalation runs on a
// throwaway copy: concurrent calls escalate independently and no call's reset
// rewinds another's progress. The clone is only allocated once a retry
// actually happens, so a call that succeeds immediately costs nothing.
//
// A backoff without Clone is shared instead of copied: concurrent calls
// advance one position together and one call's reset rewinds another's
// progress. Such a backoff must be safe for concurrent use on its own, and is
// best given to one retry wrapper at a time.
//
// It panics if retries is not positive.
func RetryContext[T any](effector effector.ValueErrorContext[T], retries int, backoff Backoff) effector.ValueErrorContext[T] {
	if retries <= 0 {
		panic("retry: retries must be at least 1")
	}

	return func(ctx context.Context) (res T, err error) {
		// session is the generator this call escalates on: the caller's
		// own, or a private copy taken once a retry is needed. Waiting
		// sets once to false, which is also what makes the caller's
		// generator worth resetting on the way out.
		session := backoff
		once := true

		defer func() {
			if !once {
				backoff.Reset()
			}
		}()

		for r := 0; ; r++ {
			if err := ctx.Err(); err != nil {
				return res, err
			}

			res, err = effector(ctx)
			if err == nil || r >= retries-1 {
				return res, err
			}

			if once {
				once = false

				if c, ok := backoff.(cloneable); ok {
					session = c.Clone()
				}
			}

			timer := time.NewTimer(session.Backoff())

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
