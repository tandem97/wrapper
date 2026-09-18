package circuitbreaker_test

import (
	"errors"
	"fmt"
	"time"

	"github.com/tandem97/wrapper/circuitbreaker"
)

type fixedBackoff time.Duration

func (b fixedBackoff) Backoff() time.Duration { return time.Duration(b) }
func (b fixedBackoff) Reset()                 {}

func ExampleBreaker() {
	var calls int

	circuit := func() (string, error) {
		calls++

		return "", errors.New("service is down")
	}

	breaker := circuitbreaker.Breaker(circuit, 3, fixedBackoff(time.Hour))

	for i := 0; i < 4; i++ {
		_, err := breaker()

		fmt.Println("call:", err)
	}

	fmt.Println("circuit calls:", calls)

	// Output:
	// call: service is down
	// call: service is down
	// call: service is down
	// call: service unreachable
	// circuit calls: 3
}
