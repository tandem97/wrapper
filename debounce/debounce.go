package debounce

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tandem97/wrapper/effector"
)

var ErrDebounce = errors.New("debounce")

type result[T any] struct {
	res T
	err error
}

func DebounceFirst[T any](circuit effector.ValueError[T], d time.Duration) effector.ValueError[T] {
	f := func(context.Context) (T, error) {
		return circuit()
	}

	debounce := DebounceFirstContext(f, d)

	return func() (T, error) {
		return debounce(context.Background())
	}
}

func DebounceFirstContext[T any](circuit effector.ValueErrorContext[T], d time.Duration) effector.ValueErrorContext[T] {
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

func DebounceLast[T any](circuit effector.ValueError[T], d time.Duration) effector.ValueError[T] {
	f := func(context.Context) (T, error) {
		return circuit()
	}

	debounce := DebounceLastContext(f, d)

	return func() (T, error) {
		return debounce(context.Background())
	}
}

func DebounceLastContext[T any](circuit effector.ValueErrorContext[T], d time.Duration) effector.ValueErrorContext[T] {
	var (
		mu     sync.Mutex
		timer  *time.Timer
		cancel context.CancelCauseFunc
	)

	return func(parent context.Context) (res T, err error) {
		cCtx, cCancel := context.WithCancelCause(parent)
		defer cCancel(nil)

		mu.Lock()

		if timer != nil {
			timer.Stop()
			cancel(ErrDebounce)
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
