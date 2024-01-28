package circuitbreaker

import (
	"errors"
	"math"
	"sync"
	"time"
)

type Circuit[T any] func() (T, error)

type Backoff interface {
	Backoff() time.Duration
	Reset()
}

var ErrServiceUnreachable = errors.New("service unreachable")

func Breaker[T any](circuit Circuit[T], threshold int, backoff Backoff) Circuit[T] {
	var (
		failures int
		last     = time.Now()
		mu       sync.RWMutex
	)

	return func() (res T, err error) {
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

		res, err = circuit()

		mu.Lock()
		defer mu.Unlock()

		last = time.Now()

		if err != nil {
			if failures == math.MaxInt {
				failures = threshold
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
