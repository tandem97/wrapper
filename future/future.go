// Package future provides a wrapper that runs a slow function in the
// background and caches its result for subsequent callers.
package future

import (
	"sync"

	"github.com/tandem97/wrapper/effector"
)

// WrapSlowFunc returns a wrapper around f that starts f immediately in a
// background goroutine and caches its (T, error) result. The first call
// blocks until f returns; subsequent calls return the cached result
// without re-invoking f.
func WrapSlowFunc[T any](f effector.ValueError[T]) effector.ValueError[T] {
	resCh := make(chan T, 1)
	errCh := make(chan error, 1)

	go func() {
		res, err := f()

		resCh <- res
		errCh <- err
	}()

	var (
		once sync.Once
		res  T
		err  error
	)

	return func() (T, error) {
		once.Do(func() {
			res = <-resCh
			err = <-errCh
		})

		return res, err
	}
}
