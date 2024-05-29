package effector

import "context"

type (
	ValueErrorContext[T any] func(context.Context) (T, error)
	ValueError[T any]        func() (T, error)
	Void                     func()
)
