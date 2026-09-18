// Package future provides a wrapper that runs a slow function in the
// background and caches its result for subsequent callers.
package future

import (
	"sync"

	"github.com/tandem97/wrapper/effector"
)

// result carries the outcome of the wrapped function over a single
// channel.
type result[T any] struct {
	res T
	err error
}

// WrapSlowFunc returns a wrapper around f that starts f immediately in a
// background goroutine and caches its (T, error) result. The first call
// blocks until f returns; subsequent calls return the cached result
// without re-invoking f.
func WrapSlowFunc[T any](f effector.ValueError[T]) effector.ValueError[T] {
	resultCh := make(chan result[T], 1)

	go func() {
		res, err := f()

		resultCh <- result[T]{res: res, err: err}
	}()

	var (
		once sync.Once
		res  T
		err  error
	)

	return func() (T, error) {
		once.Do(func() {
			r := <-resultCh

			res, err = r.res, r.err
		})

		return res, err
	}
}
