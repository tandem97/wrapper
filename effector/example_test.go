package effector_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tandem97/wrapper/debounce"
	"github.com/tandem97/wrapper/effector"
)

// Void is the one shape that returns nothing, so it needs its own type
// rather than a ValueError. Adapting it yields
// ValueErrorContext[struct{}]: the wrapped call reports an empty result
// alongside the error.
func ExampleVoid() {
	var sent int

	notify := effector.Void(func() { sent++ })

	// Coalesce notifications: the first call runs, later ones inside the
	// window are answered from the cache.
	wrapped := debounce.DebounceFirstContext(notify.ValueErrorContext(), 50*time.Millisecond)

	ctx := context.Background()

	res, err := wrapped(ctx)
	fmt.Printf("first:  %v %v\n", res, err)

	res, err = wrapped(ctx)
	fmt.Printf("second: %v %v\n", res, err)

	fmt.Println("sent:", sent)

	// Output:
	// first:  {} <nil>
	// second: {} <nil>
	// sent: 1
}

// ValueError adapts to the context-aware form, so a plain function can be
// handed to any *Context wrapper.
func ExampleValueError() {
	fail := effector.ValueError[int](func() (int, error) { return 0, nil })

	adapted := fail.ValueErrorContext()

	res, err := adapted(context.Background())
	fmt.Println(res, err)

	// Output:
	// 0 <nil>
}
