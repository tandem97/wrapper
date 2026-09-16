package effector

import "context"

type (
	ValueErrorContext[T any] func(context.Context) (T, error)
	ValueError[T any]        func() (T, error)
	Void                     func()
)

func (f Void) ValueErrorContext() ValueErrorContext[struct{}] {
	return func(ctx context.Context) (_ struct{}, _ error) {
		f()

		return
	}
}

func (f ValueError[T]) ValueErrorContext() ValueErrorContext[T] {
	return func(ctx context.Context) (T, error) {
		return f()
	}
}
