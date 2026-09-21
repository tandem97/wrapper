package timeout_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tandem97/wrapper/timeout"
)

func ExampleTimeout() {
	circuit := func() (string, error) {
		time.Sleep(20 * time.Millisecond)

		return "ok", nil
	}

	wrapped := timeout.Timeout(circuit)

	res, err := wrapped(context.Background())
	fmt.Printf("no timeout: %q %v\n", res, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	res, err = wrapped(ctx)
	fmt.Printf("with timeout: %q %v\n", res, err)

	// Output:
	// no timeout: "ok" <nil>
	// with timeout: "" context deadline exceeded
}
