// Package effector defines the tiny function types every wrapper in this
// module builds on.
//
// A wrapper takes one of these functions and returns a function of the
// same shape, so wrappers compose freely. Plain functions adapt to the
// context-aware form automatically via ValueErrorContext, keeping
// wrapping and composing to a single line.
//
// The package also declares the Backoff contract consumed by retry and
// circuitbreaker, and the optional Cloneable extension implemented by
// the generators in backoff.
package effector

import (
	"context"
	"time"
)

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

// Backoff supplies delays between retry attempts.
//
// Backoff returns the delay to wait before the next attempt, and Reset
// restarts the sequence at its initial value.
//
// A generator holds the position in its sequence, so one instance
// represents one sequence. Reset rewinds that position, which is what lets a
// caller reuse a generator after it has driven one round of delays.
//
// Implementations only have to be safe for concurrent use on their own.
// Keeping concurrent sessions on separate sequences is the caller's concern,
// which is what Cloneable exists to solve.
type Backoff interface {
	// Backoff returns the delay until the next attempt.
	Backoff() time.Duration

	// Reset restarts the sequence at its initial value.
	Reset()
}

// Cloneable is an optional Backoff extension: a generator that can hand
// out an independent copy of itself.
//
// A Clone starts a fresh sequence with the same configuration, leaving
// the original untouched. retry clones the generator lazily, on the first
// failed attempt, so concurrent calls each escalate on their own and a call
// that succeeds immediately costs no clone at all.
//
// A Backoff that does not implement Cloneable is used as-is, and is therefore
// shared between concurrent calls.
type Cloneable interface {
	Backoff

	// Clone returns an independent copy starting a fresh sequence.
	Clone() Backoff
}

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
