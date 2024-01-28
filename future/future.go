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

	wg   sync.WaitGroup
	once sync.Once
}

func (f *innerFuture[T]) Result() (T, error) {
	f.once.Do(func() {
		f.wg.Add(1)
		defer f.wg.Done()

		f.res = <-f.resCh
		f.err = <-f.errCh
	})

	f.wg.Wait()

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
