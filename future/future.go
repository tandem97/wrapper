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
//
// The context bounds the wait only, never the work: f takes no context, so
// it runs to completion whatever a caller does. A result that is already
// cached is returned even when ctx is already done, since there is nothing
// left to wait for; ctx.Err() comes back only while f is still running.
//
// Unlike the other context-aware wrappers in the module, an already
// cancelled context does not short-circuit the call.
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
		// A result that is already there is handed over regardless of ctx.
		// The work is done and handing it over costs nothing, so a caller
		// that has given up waiting still gets the value. Checked first and
		// non-blocking, otherwise the select below would pick between the
		// two ready cases at random.
		select {
		case <-ready:
			return res, err
		default:
		}

		// Nothing cached yet, so the context governs the wait: whichever
		// happens first wins.
		select {
		case <-ready:
			return res, err
		case <-ctx.Done():
			var zero T

			return zero, ctx.Err()
		}
	}
}
