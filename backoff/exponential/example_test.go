package exponential_test

import (
	"fmt"
	"time"

	"github.com/tandem97/wrapper/backoff/exponential"
)

func Example() {
	backoff := exponential.New(
		exponential.WithBase(10*time.Millisecond),
		exponential.WithCap(40*time.Millisecond),
	)

	for i := 0; i < 5; i++ {
		fmt.Println(backoff.Backoff())
	}

	backoff.Reset()

	fmt.Println("after reset:", backoff.Backoff())

	// Output:
	// 10ms
	// 20ms
	// 40ms
	// 40ms
	// 40ms
	// after reset: 10ms
}
