// Package debounce provides leading- and trailing-edge debounce wrappers
// that coalesce calls to a wrapped circuit within a time window.
//
// DebounceFirst runs the circuit immediately on the first call in a window
// and returns the cached (T, error) to subsequent callers until the window
// expires. DebounceLast runs the circuit only after the window of the last
// call expires; calls superseded by newer ones before their circuit could
// run receive ErrDebounce.
//
// The wrapped circuit must respect the provided context. A call with an
// already cancelled context returns ctx.Err() immediately without
// invoking the circuit.
package debounce

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tandem97/wrapper/effector"
)

// ErrDebounce is returned to callers whose trailing-edge call was
// superseded by a newer call before its circuit could run. A circuit that
// had already started is not superseded: it runs to completion and its
// caller gets the result.
var ErrDebounce = errors.New("debounce")

type result[T any] struct {
	res T
	err error
}

// DebounceFirst returns a leading-edge debounce wrapper around circuit:
// the first call executes circuit immediately, and calls within d return
// the cached result and error of that execution.
func DebounceFirst[T any](circuit effector.ValueError[T], d time.Duration) effector.ValueError[T] {
	debounce := DebounceFirstContext(circuit.ValueErrorContext(), d)

	return func() (T, error) {
		return debounce(context.Background())
	}
}

// DebounceFirstContext returns a leading-edge debounce wrapper around
// circuit: the first call executes circuit immediately, and calls within
// d return the cached result and error of that execution. The window
// starts when the circuit completes, so a slow circuit shifts it. Circuit
// must respect the provided context. A call with an already cancelled
// context returns ctx.Err() immediately.
//
// There is one run of the circuit per window, so it runs under the context
// of the caller that opened the window, and that context is the only way to
// cancel it. A caller arriving afterwards waits for that run rather than
// starting its own, and inherits its outcome: if the initiating caller is
// cancelled, the waiters receive the circuit's error, typically
// context.Canceled, even though their own context is still live. Once a
// caller is waiting on someone else's run, nothing about its own context is
// honoured.
func DebounceFirstContext[T any](circuit effector.ValueErrorContext[T], d time.Duration) effector.ValueErrorContext[T] {
	if d <= 0 {
		panic("debounce d must be positive")
	}

	var (
		threshold time.Time
		result    T
		err       error
		mu        sync.RWMutex
		done      chan struct{}
	)

	return func(ctx context.Context) (T, error) {
		if err := ctx.Err(); err != nil {
			var res T

			return res, err
		}

		var (
			shouldCall bool
			doneCh     chan struct{}
		)

		mu.RLock()

		if time.Now().Before(threshold) {
			defer mu.RUnlock()

			return result, err
		}

		mu.RUnlock()
		mu.Lock()

		// Re-check under the write lock: another caller may have started
		// a new window while we were waiting for the lock.
		if time.Now().Before(threshold) {
			defer mu.Unlock()

			return result, err
		}

		if done == nil {
			done = make(chan struct{})
			shouldCall = true
		}

		doneCh = done

		mu.Unlock()

		if shouldCall {
			newResult, newErr := circuit(ctx)

			mu.Lock()
			defer mu.Unlock()

			result, err = newResult, newErr
			threshold = time.Now().Add(d)

			close(done)

			done = nil

			return result, err
		}

		select {
		case <-doneCh:
			mu.RLock()
			defer mu.RUnlock()

			return result, err
		case <-ctx.Done():
			var res T

			return res, ctx.Err()
		}
	}
}

// DebounceLast returns a trailing-edge debounce wrapper around circuit:
// circuit runs only after a quiet period of d has elapsed since the last
// call, and calls superseded by newer ones return ErrDebounce.
func DebounceLast[T any](circuit effector.ValueError[T], d time.Duration) effector.ValueError[T] {
	debounce := DebounceLastContext(circuit.ValueErrorContext(), d)

	return func() (T, error) {
		return debounce(context.Background())
	}
}

// DebounceLastContext returns a trailing-edge debounce wrapper around
// circuit: each call reschedules the execution of the window d, and calls
// superseded by newer ones return ErrDebounce.
//
// Superseding only applies before the circuit starts. Once the trailing
// run is under way a newer call does not cancel it: that caller receives
// the circuit's result rather than ErrDebounce, because throwing away work
// in progress would mean a circuit slower than the gap between calls never
// completes. The cost is that the circuit can be in flight for two callers
// at once, so it must be safe for concurrent use.
//
// Circuit must respect the provided context. A call with an already
// cancelled context returns ctx.Err() immediately.
func DebounceLastContext[T any](circuit effector.ValueErrorContext[T], d time.Duration) effector.ValueErrorContext[T] {
	if d <= 0 {
		panic("debounce d must be positive")
	}

	var (
		mu     sync.Mutex
		timer  *time.Timer
		cancel context.CancelCauseFunc
	)

	return func(parent context.Context) (res T, err error) {
		if err := context.Cause(parent); err != nil {
			return res, err
		}

		cCtx, cCancel := context.WithCancelCause(parent)
		defer cCancel(nil)

		mu.Lock()

		if timer != nil {
			// Only a circuit that has not started yet is superseded. A Stop
			// that returns false means the trailing circuit is already
			// running under that caller's context: cancelling it now would
			// throw away work in progress and hand the caller an
			// ErrDebounce for a run that was under way. Letting it finish
			// means the circuit can be in flight for two callers at once,
			// so it must be safe for concurrent use.
			if timer.Stop() {
				cancel(ErrDebounce)
			}
		}

		cancel = cCancel
		ch := make(chan result[T], 1)
		timer = time.AfterFunc(d, func() {
			res, err := circuit(cCtx)
			ch <- result[T]{res: res, err: err}
		})

		mu.Unlock()

		select {
		case result := <-ch:
			res, err = result.res, result.err
		case <-cCtx.Done():
			err = context.Cause(cCtx)
		}

		return
	}
}
