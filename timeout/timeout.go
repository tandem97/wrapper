// Package timeout provides a wrapper that bounds a function without a
// context by a context deadline or cancellation.
//
// The wrapped function cannot be cancelled: once the timeout fires, the
// caller receives the context error while the function keeps running and
// its result is discarded. A panic in the wrapped function crashes the
// process.
package timeout

import (
	"context"

	"github.com/tandem97/wrapper/effector"
)

type result[T any] struct {
	res T
	err error
}

// Timeout returns a context-aware wrapper around f that runs f and returns
// its (T, error) result, or the context error if ctx is done first.
//
// When ctx cannot be cancelled (ctx.Done() returns nil, as for
// context.Background), f is invoked synchronously in the caller's
// goroutine. Otherwise f runs in a background goroutine: since f does not
// receive a context, it keeps running after a timeout and its result is
// discarded.
func Timeout[T any](f effector.ValueError[T]) effector.ValueErrorContext[T] {
	return func(ctx context.Context) (res T, err error) {
		if err := context.Cause(ctx); err != nil {
			return res, err
		}

		// A context that cannot be cancelled can never time out, so run
		// f synchronously and skip the goroutine.
		if ctx.Done() == nil {
			return f()
		}

		ch := make(chan result[T], 1)

		go func() {
			res, err := f()
			ch <- result[T]{res: res, err: err}
		}()

		select {
		case result := <-ch:
			return result.res, result.err
		case <-ctx.Done():
			err = context.Cause(ctx)

			return
		}
	}
}
