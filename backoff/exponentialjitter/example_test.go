package exponentialjitter_test

import (
	"fmt"
	"time"

	"github.com/tandem97/wrapper/backoff/exponentialjitter"
)

// The generator seeded with a fixed value is deterministic, so the
// output below is stable for a given Go release. The PCG source is not
// guaranteed across releases, so pin the toolchain when relying on exact
// values.
func ExampleBackoff() {
	backoff := exponentialjitter.New(
		exponentialjitter.WithBase(100*time.Millisecond),
		exponentialjitter.WithCeiling(2*time.Second),
		exponentialjitter.WithMultiplier(2),
		exponentialjitter.WithSeed(42, 1),
	)

	fmt.Println(backoff.Backoff())
	fmt.Println(backoff.Backoff())
	fmt.Println(backoff.Backoff())

	// Without a seed and with jitter, the delays vary between runs and
	// the jitter factor is drawn from [1-jitter, 1+jitter).
	random := exponentialjitter.New(
		exponentialjitter.WithBase(100*time.Millisecond),
		exponentialjitter.WithCeiling(2*time.Second),
		exponentialjitter.WithMultiplier(2),
		exponentialjitter.WithJitter(0.1),
	)

	for i := 0; i < 3; i++ {
		fmt.Println("jittered:", random.Backoff() > 0)
	}

	// Output:
	// 112.7947ms
	// 165.540273ms
	// 374.259432ms
	// jittered: true
	// jittered: true
	// jittered: true
}
