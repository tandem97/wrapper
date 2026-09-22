// Package effector defines the tiny function types every wrapper in this
// module builds on.
//
// A wrapper takes one of these functions and returns a function of the
// same shape, so wrappers compose freely. Plain functions adapt to the
// context-aware form automatically via ValueErrorContext, keeping
// wrapping and composing to a single line.
package effector

import "context"

type (
	// ValueErrorContext is a context-aware function that returns a value
	// and an error. It is the shape every *Context wrapper accepts and
	// returns.
	ValueErrorContext[T any] func(context.Context) (T, error)

	// ValueError is a plain function that returns a value and an error.
	// It is the shape every plain wrapper accepts and returns.
	ValueError[T any] func() (T, error)

	// Void is a side-effect-only function that returns nothing.
	Void func()
)

// ValueErrorContext adapts f to the context-aware form: the returned
// function ignores ctx and calls f, returning the zero value of struct{}
// and a nil error.
func (f Void) ValueErrorContext() ValueErrorContext[struct{}] {
	return func(ctx context.Context) (_ struct{}, _ error) {
		f()

		return
	}
}

// ValueErrorContext adapts f to the context-aware form: the returned
// function ignores ctx and calls f.
func (f ValueError[T]) ValueErrorContext() ValueErrorContext[T] {
	return func(ctx context.Context) (T, error) {
		return f()
	}
}
