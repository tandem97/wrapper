package debounce_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tandem97/wrapper/debounce"
)

func ExampleDebounceFirst() {
	var calls int

	circuit := func() (string, error) {
		calls++

		return "ok", nil
	}

	debounced := debounce.DebounceFirst(circuit, 50*time.Millisecond)

	for i := 0; i < 3; i++ {
		res, err := debounced()

		fmt.Println("call:", res, err)
	}

	time.Sleep(60 * time.Millisecond)

	res, err := debounced()

	fmt.Println("next window:", res, err)
	fmt.Println("circuit calls:", calls)

	// Output:
	// call: ok <nil>
	// call: ok <nil>
	// call: ok <nil>
	// next window: ok <nil>
	// circuit calls: 2
}

func ExampleDebounceLast() {
	var calls int

	debounced := debounce.DebounceLastContext(func(context.Context) (string, error) {
		calls++

		return "ok", nil
	}, 100*time.Millisecond)

	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, _ = debounced(context.Background())
		}()
	}

	wg.Wait()

	fmt.Println("circuit calls:", calls)

	// Output:
	// circuit calls: 1
}
