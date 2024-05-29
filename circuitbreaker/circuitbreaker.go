package circuitbreaker

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/tandem97/wrapper/effector"
)

type Backoff interface {
	Backoff() time.Duration
	Reset()
}

var ErrServiceUnreachable = errors.New("service unreachable")

func Breaker[T any](circuit effector.ValueError[T], threshold int, backoff Backoff) effector.ValueError[T] {
	f := func(context.Context) (T, error) {
		return circuit()
	}

	breaker := BreakerContext(f, threshold, backoff)

	return func() (T, error) {
		return breaker(context.Background())
	}
}

func BreakerContext[T any](circuit effector.ValueErrorContext[T], threshold int, backoff Backoff) effector.ValueErrorContext[T] {
	var (
		failures int
		last     = time.Now()
		mu       sync.RWMutex
	)

	return func(ctx context.Context) (res T, err error) {
		mu.RLock()

		d := failures - threshold

		if d > 0 {
			shouldRetryAt := last.Add(backoff.Backoff())
			if !time.Now().After(shouldRetryAt) {
				mu.RUnlock()

				err = ErrServiceUnreachable

				return
			}
		}

		mu.RUnlock()

		res, err = circuit(ctx)

		mu.Lock()
		defer mu.Unlock()

		last = time.Now()

		if err != nil {
			if failures == math.MaxInt {
				failures = threshold + 1
				return
			}

			failures++

			return
		}

		failures = 0

		backoff.Reset()

		return
	}
}
