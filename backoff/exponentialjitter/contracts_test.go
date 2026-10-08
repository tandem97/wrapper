package exponentialjitter_test

import (
	"testing"
	"time"

	"github.com/tandem97/wrapper/backoff/exponentialjitter"
	"github.com/tandem97/wrapper/effector"
	"github.com/tandem97/wrapper/retry"
)

// The generator must satisfy Backoff, which retry and circuitbreaker
// accept, and Cloneable, which retry probes for to isolate sessions. The
// assertions sit in an external test package so they are checked from the
// outside, where the wrappers consume them; inside the package they would
// be tautologies the compiler checks anyway.
var (
	_ effector.Backoff   = (*exponentialjitter.Backoff)(nil)
	_ effector.Cloneable = (*exponentialjitter.Backoff)(nil)

	// retry.Backoff is an alias for effector.Backoff, so a generator must
	// be assignable to it directly.
	_ retry.Backoff = (*exponentialjitter.Backoff)(nil)
)

// A clone must be a working generator, not merely a type that fits.
func TestCloneIsUsableGenerator(t *testing.T) {
	generator := exponentialjitter.New(
		exponentialjitter.WithBase(time.Millisecond),
		exponentialjitter.WithCeiling(time.Hour),
		// Zero jitter keeps the first delay predictable: with jitter the
		// factor may dip below 1.
		exponentialjitter.WithJitter(0),
	)

	clone := generator.Clone()

	// A fresh clone starts at base and grows from there.
	if d := clone.Backoff(); d != time.Millisecond {
		t.Fatalf("clone first call returned %v, want the base %v", d, time.Millisecond)
	}

	if d := clone.Backoff(); d <= time.Millisecond {
		t.Fatalf("clone second call returned %v, want above the base %v", d, time.Millisecond)
	}

	// Reset returns it to base.
	clone.Reset()

	if d := clone.Backoff(); d != time.Millisecond {
		t.Fatalf("clone after reset returned %v, want the base %v", d, time.Millisecond)
	}
}
