package debounce

import (
	"context"
	"sync"
	"time"
)

type CircuitContext[T any] func(context.Context) (T, error)

type Circuit[T any] func() (T, error)

type result[T any] struct {
	res T
	err error
}

func DebounceFirst[T any](circuit Circuit[T], d time.Duration) Circuit[T] {
	f := func(context.Context) (T, error) {
		return circuit()
	}

	debounce := DebounceFirstContext(f, d)

	return func() (T, error) {
		return debounce(context.Background())
	}
}

func DebounceFirstContext[T any](circuit CircuitContext[T], d time.Duration) CircuitContext[T] {
	var (
		threshold time.Time
		result    T
		err       error
		mu        sync.Mutex
	)

	return func(ctx context.Context) (T, error) {
		mu.Lock()
		defer mu.Unlock()

		if time.Now().Before(threshold) {
			return result, err
		}

		result, err = circuit(ctx)
		threshold = time.Now().Add(d)

		return result, err
	}
}

func DebounceLast[T any](circuit Circuit[T], d time.Duration) Circuit[T] {
	f := func(context.Context) (T, error) {
		return circuit()
	}

	debounce := DebounceLastContext(f, d)

	return func() (T, error) {
		return debounce(context.Background())
	}
}

func DebounceLastContext[T any](circuit CircuitContext[T], d time.Duration) CircuitContext[T] {
	var (
		mu     sync.RWMutex
		timer  *time.Timer
		ctx    context.Context
		cancel context.CancelFunc
	)

	return func(parent context.Context) (res T, err error) {
		mu.RLock()

		if timer != nil {
			timer.Stop()
			cancel()
		}

		mu.RUnlock()

		mu.Lock()

		ctx, cancel = context.WithCancel(parent)
		ch := make(chan result[T], 1)
		timer = time.AfterFunc(d, func() {
			res, err := circuit(ctx)
			ch <- result[T]{res: res, err: err}
		})

		mu.Unlock()

		mu.RLock()

		select {
		case result := <-ch:
			res, err = result.res, result.err
		case <-ctx.Done():
			err = ctx.Err()
		}

		mu.RUnlock()

		return
	}
}
