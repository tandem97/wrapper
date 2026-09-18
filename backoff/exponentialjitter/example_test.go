package exponentialjitter_test

import (
	"fmt"
	"time"

	"github.com/tandem97/wrapper/backoff/exponentialjitter"
)

func ExampleBackoff() {
	backoff := exponentialjitter.New(
		exponentialjitter.WithBase(100*time.Millisecond),
		exponentialjitter.WithCap(2*time.Second),
		exponentialjitter.WithMultiplier(2),
		exponentialjitter.WithJitter(0.1),
	)

	// The returned delays grow exponentially and are spread by a random
	// factor in [1-jitter, 1+jitter), so the values vary between runs.
	for i := 0; i < 5; i++ {
		fmt.Println(backoff.Backoff())
	}
}
