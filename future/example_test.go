package future_test

import (
	"fmt"
	"time"

	"github.com/tandem97/wrapper/future"
)

func ExampleWrapSlowFunc() {
	var calls int

	circuit := func() (string, error) {
		calls++

		time.Sleep(20 * time.Millisecond)

		return "ok", nil
	}

	wrapped := future.WrapSlowFunc(circuit)

	for i := 0; i < 3; i++ {
		res, err := wrapped()

		fmt.Println("call:", res, err)
	}

	fmt.Println("circuit calls:", calls)

	// Output:
	// call: ok <nil>
	// call: ok <nil>
	// call: ok <nil>
	// circuit calls: 1
}
