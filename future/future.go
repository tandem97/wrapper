package future

import (
	"sync"

	"github.com/tandem97/wrapper/effector"
)

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
