package retry

import (
	"context"
	"time"
)

type EffectorContext[T any] func(context.Context) (T, error)

type Effector[T any] func() (T, error)

type Backoff interface {
	Backoff() time.Duration
}

func Retry[T any](effector Effector[T], retries int, backoff Backoff) Effector[T] {
	f := func(context.Context) (T, error) {
		return effector()
	}

	retry := RetryContext(f, retries, backoff)

	return func() (T, error) {
		return retry(context.Background())
	}
}

func RetryContext[T any](effector EffectorContext[T], retries int, backoff Backoff) EffectorContext[T] {
	return func(ctx context.Context) (res T, err error) {
		for r := 0; ; r++ {
			res, err = effector(ctx)
			if err == nil || r >= retries {
				return res, err
			}

			timer := time.NewTimer(backoff.Backoff())

			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}

				err = ctx.Err()

				return
			}
		}
	}
}
