// Package timeout provides a wrapper that bounds a function without a
// context by a context deadline or cancellation.
//
// The wrapped function runs in a background goroutine and cannot be
// cancelled: once the timeout fires, the caller receives ctx.Err() while
// the function keeps running and its result is discarded. A panic in the
// wrapped function crashes the process.
package timeout

import (
	"context"

	"github.com/tandem97/wrapper/effector"
)

type result[T any] struct {
	res T
	err error
}

// Timeout returns a context-aware wrapper around f that runs f in a
// background goroutine and returns its (T, error) result, or ctx.Err() if
// ctx is done first. Since f does not receive a context, it keeps running
// after a timeout and its result is discarded.
func Timeout[T any](f effector.ValueError[T]) effector.ValueErrorContext[T] {
	return func(ctx context.Context) (res T, err error) {
		if err := ctx.Err(); err != nil {
			return res, err
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
			err = ctx.Err()
			return
		}
	}
}
