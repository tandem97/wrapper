package retry

import (
	"context"
	"time"

	"github.com/tandem97/wrapper/effector"
)

type Backoff interface {
	Backoff() time.Duration
}

func Retry[T any](effector effector.ValueError[T], retries int, backoff Backoff) effector.ValueError[T] {
	f := func(context.Context) (T, error) {
		return effector()
	}

	retry := RetryContext(f, retries, backoff)

	return func() (T, error) {
		return retry(context.Background())
	}
}

func RetryContext[T any](effector effector.ValueErrorContext[T], retries int, backoff Backoff) effector.ValueErrorContext[T] {
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
