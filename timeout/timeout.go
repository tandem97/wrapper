package timeout

import "context"

type Effector[T any] func() (T, error)

type WithContext[T any] func(context.Context) (T, error)

type result[T any] struct {
	res T
	err error
}

func Timeout[T any](f Effector[T]) WithContext[T] {
	return func(ctx context.Context) (res T, err error) {
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
