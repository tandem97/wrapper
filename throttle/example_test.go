package throttle_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tandem97/wrapper/throttle"
)

func ExampleThrottle() {
	var calls int

	circuit := func() (string, error) {
		calls++

		return "ok", nil
	}

	refillCtx, stop := context.WithCancel(context.Background())
	defer stop()

	throttled := throttle.Throttle(refillCtx, circuit, 2, 100, time.Second)

	for i := 0; i < 3; i++ {
		res, err := throttled()

		fmt.Printf("call %d: %q %v\n", i+1, res, err)
	}

	fmt.Println("effector calls:", calls)

	// Output:
	// call 1: "ok" <nil>
	// call 2: "ok" <nil>
	// call 3: "" too many calls
	// effector calls: 2
}
