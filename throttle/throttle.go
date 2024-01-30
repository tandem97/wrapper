package throttle

import (
	"context"
	"errors"
	"sync"
	"time"
)

type EffectorContext[T any] func(context.Context) (T, error)

var ErrTooManyCalls = errors.New("too many calls")

func ThrottleContext[T any](refillCtx context.Context, effector EffectorContext[T], max uint, refill uint, d time.Duration) EffectorContext[T] {
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
