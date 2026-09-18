package throttle

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tandem97/wrapper/effector"
)

var ErrTooManyCalls = errors.New("too many calls")

func ThrottleVoid(refillCtx context.Context, effector effector.Void, max uint, refill uint, d time.Duration) effector.Void {
	throttle := ThrottleContext(refillCtx, effector.ValueErrorContext(), max, refill, d)

	return func() {
		_, _ = throttle(context.Background())
	}
}

func Throttle[T any](refillCtx context.Context, effector effector.ValueError[T], max uint, refill uint, d time.Duration) effector.ValueError[T] {
	throttle := ThrottleContext(refillCtx, effector.ValueErrorContext(), max, refill, d)

	return func() (T, error) {
		return throttle(context.Background())
	}
}

func ThrottleContext[T any](refillCtx context.Context, effector effector.ValueErrorContext[T], max uint, refill uint, d time.Duration) effector.ValueErrorContext[T] {
	if d <= 0 {
		panic("throttle: d must be positive")
	}

	if max == 0 {
		panic("throttle: max must be greater than zero")
	}

	if refill == 0 {
		panic("throttle: refill must be greater than zero")
	}

	var (
		tokens = max
		once   sync.Once
		mu     sync.Mutex
	)

	return func(ctx context.Context) (res T, err error) {
		once.Do(func() {
			go func() {
				ticker := time.NewTicker(d)
				defer ticker.Stop()

				for {
					select {
					case <-refillCtx.Done():
						return

					case <-ticker.C:
						mu.Lock()

						tokens += refill
						if tokens > max {
							tokens = max
						}

						mu.Unlock()
					}
				}
			}()
		})

		mu.Lock()

		if tokens <= 0 {
			mu.Unlock()

			err = ErrTooManyCalls

			return
		}

		tokens--

		mu.Unlock()

		return effector(ctx)
	}
}
