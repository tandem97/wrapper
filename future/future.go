// Package future provides a wrapper that runs a slow function in the
// background and caches its result for subsequent callers.
//
// WrapSlowFuncContext additionally lets callers limit their wait for the
// result with a context.
package future

import (
	"context"

	"github.com/tandem97/wrapper/effector"
)

// WrapSlowFunc returns a wrapper around f that starts f immediately in a
// background goroutine and caches its (T, error) result. The first call
// blocks until f returns; subsequent calls return the cached result
// without re-invoking f.
func WrapSlowFunc[T any](f effector.ValueError[T]) effector.ValueError[T] {
	wrapped := WrapSlowFuncContext(f)

	return func() (T, error) {
		return wrapped(context.Background())
	}
}

// WrapSlowFuncContext returns a context-aware wrapper around f that starts
// f immediately in a background goroutine and caches its (T, error)
// result. The first call blocks until f returns or ctx is done; if ctx is
// done first, ctx.Err() is returned and the result is still cached for
// later calls. Subsequent calls return the cached result immediately.
func WrapSlowFuncContext[T any](f effector.ValueError[T]) effector.ValueErrorContext[T] {
	ready := make(chan struct{})

	var (
		res T
		err error
	)

	go func() {
		res, err = f()

		close(ready)
	}()

	return func(ctx context.Context) (T, error) {
		select {
		case <-ready:
			return res, err
		case <-ctx.Done():
			var zero T

			return zero, ctx.Err()
		}
	}
}
