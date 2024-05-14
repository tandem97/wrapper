package future

import (
	"sync"
)

type Future[T any] interface {
	Result() (T, error)
}

type innerFuture[T any] struct {
	res T
	err error

	resCh <-chan T
	errCh <-chan error

	once sync.Once
}

func (f *innerFuture[T]) Result() (T, error) {
	f.once.Do(func() {
		f.res = <-f.resCh
		f.err = <-f.errCh
	})

	return f.res, f.err
}

func WrapSlowFunc[T any](f func() (T, error)) Future[T] {
	resCh := make(chan T, 1)
	errCh := make(chan error, 1)

	go func() {
		res, err := f()

		resCh <- res
		errCh <- err
	}()

	return &innerFuture[T]{
		resCh: resCh,
		errCh: errCh,
	}
}
