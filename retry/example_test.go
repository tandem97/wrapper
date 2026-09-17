package retry_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tandem97/wrapper/retry"
)

type fixedBackoff time.Duration

func (b fixedBackoff) Backoff() time.Duration { return time.Duration(b) }
func (b fixedBackoff) Reset()                 {}

func ExampleRetry() {
	var attempts int

	failing := func() (string, error) {
		attempts++

		if attempts < 3 {
			return "", errors.New("transient failure")
		}

		return "ok", nil
	}

	retrier := retry.Retry(failing, 5, fixedBackoff(time.Microsecond))

	res, err := retrier()

	fmt.Println("result:", res, err)
	fmt.Println("attempts:", attempts)

	// Output:
	// result: ok <nil>
	// attempts: 3
}

func ExampleRetryContext() {
	var attempts int

	failing := func(context.Context) (string, error) {
		attempts++

		return "", errors.New("transient failure")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	retrier := retry.RetryContext(failing, 5, fixedBackoff(time.Hour))

	_, err := retrier(ctx)

	fmt.Println("attempts:", attempts)
	fmt.Println("error:", err)

	// Output:
	// attempts: 1
	// error: context canceled
}