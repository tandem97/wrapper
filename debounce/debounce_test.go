package debounce

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDebounceFirstCachesResult(t *testing.T) {
	var calls int

	circuit := func() (int, error) {
		calls++

		return 42, nil
	}

	d := DebounceFirst(circuit, 50*time.Millisecond)

	// Calls within the window return the cached result.
	for i := 0; i < 3; i++ {
		res, err := d()
		if err != nil || res != 42 {
			t.Fatalf("call %d: got (%v, %v), want (42, nil)", i+1, res, err)
		}
	}

	if calls != 1 {
		t.Fatalf("circuit invoked %d times, want 1", calls)
	}

	time.Sleep(60 * time.Millisecond)

	// The next window invokes the circuit again.
	if _, err := d(); err != nil {
		t.Fatalf("next window: unexpected error %v", err)
	}

	if calls != 2 {
		t.Fatalf("circuit invoked %d times, want 2", calls)
	}
}

func TestDebounceFirstConcurrent(t *testing.T) {
	var calls int
	var mu sync.Mutex

	circuit := func() (int, error) {
		mu.Lock()
		calls++
		mu.Unlock()

		time.Sleep(10 * time.Millisecond)

		return 7, nil
	}

	d := DebounceFirst(circuit, time.Second)

	const goroutines = 16

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			res, err := d()
			if err != nil || res != 7 {
				t.Errorf("got (%v, %v), want (7, nil)", res, err)
			}
		}()
	}
	wg.Wait()

	if calls != 1 {
		t.Fatalf("circuit invoked %d times, want 1", calls)
	}
}

func TestDebounceFirstContextCancellation(t *testing.T) {
	started := make(chan struct{})

	circuit := func(ctx context.Context) (int, error) {
		close(started)

		<-ctx.Done()

		return 0, ctx.Err()
	}

	d := DebounceFirstContext(circuit, time.Second)

	callCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d(callCtx)

	<-started

	// A waiter whose context is already cancelled returns ctx.Err().
	ctx, cancelWait := context.WithCancel(context.Background())
	cancelWait()

	res, err := d(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	if res != 0 {
		t.Fatalf("expected zero result, got %v", res)
	}
}

func TestDebounceLastRunsAfterQuietPeriod(t *testing.T) {
	var calls int

	circuit := func() (string, error) {
		calls++

		return "ok", nil
	}

	d := DebounceLast(circuit, 20*time.Millisecond)

	res, err := d()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if res != "ok" {
		t.Fatalf("got %q, want ok", res)
	}

	if calls != 1 {
		t.Fatalf("circuit invoked %d times, want 1", calls)
	}
}

func TestDebounceLastSupersededCalls(t *testing.T) {
	var calls int

	circuit := func() (int, error) {
		calls++

		return 1, nil
	}

	d := DebounceLast(circuit, 30*time.Millisecond)

	results := make(chan error, 2)

	go func() {
		_, err := d()
		results <- err
	}()

	time.Sleep(5 * time.Millisecond)

	go func() {
		_, err := d()
		results <- err
	}()

	var got []error

	for i := 0; i < 2; i++ {
		got = append(got, <-results)
	}

	if calls != 1 {
		t.Fatalf("circuit invoked %d times, want 1", calls)
	}

	// The first call was superseded, the second one ran the circuit.
	if !errors.Is(got[0], ErrDebounce) {
		t.Fatalf("first call: got %v, want ErrDebounce", got[0])
	}

	if got[1] != nil {
		t.Fatalf("second call: got %v, want nil", got[1])
	}
}

func TestDebounceLastContextCancellation(t *testing.T) {
	d := DebounceLastContext(func(context.Context) (int, error) { return 1, nil }, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := d(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDebounceFirstPanicsOnNonPositive(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("DebounceFirst did not panic")
		}
	}()

	DebounceFirst(func() (int, error) { return 0, nil }, 0)
}

func TestDebounceLastPanicsOnNonPositive(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("DebounceLast did not panic")
		}
	}()

	DebounceLast(func() (int, error) { return 0, nil }, -time.Second)
}

func TestDebounceFirstCachesError(t *testing.T) {
	boom := errors.New("boom")
	var calls int

	circuit := func() (int, error) {
		calls++

		return 0, boom
	}

	d := DebounceFirst(circuit, 50*time.Millisecond)

	for i := 0; i < 3; i++ {
		_, err := d()
		if !errors.Is(err, boom) {
			t.Fatalf("call %d: got %v, want boom", i+1, err)
		}
	}

	if calls != 1 {
		t.Fatalf("circuit invoked %d times, want 1", calls)
	}
}

func TestDebounceFirstContextCancelledAtEntry(t *testing.T) {
	var calls int

	circuit := func(context.Context) (int, error) {
		calls++

		return 1, nil
	}

	d := DebounceFirstContext(circuit, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := d(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}

	if calls != 0 {
		t.Fatalf("circuit invoked %d times, want 0", calls)
	}
}

func TestDebounceFirstFirstCallerContextGoverns(t *testing.T) {
	got := make(chan context.Context, 1)

	circuit := func(ctx context.Context) (int, error) {
		got <- ctx

		return 1, nil
	}

	d := DebounceFirstContext(circuit, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := d(ctx); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if c := <-got; c != ctx {
		t.Fatal("circuit did not receive the first caller's context")
	}
}

func TestDebounceLastConcurrent(t *testing.T) {
	var calls int
	var mu sync.Mutex

	circuit := func() (int, error) {
		mu.Lock()
		calls++
		mu.Unlock()

		return 1, nil
	}

	d := DebounceLast(circuit, 50*time.Millisecond)

	const goroutines = 16

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, _ = d()
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if calls != 1 {
		t.Fatalf("circuit invoked %d times, want 1", calls)
	}
}

func TestDebounceLastContextCancelledAtEntry(t *testing.T) {
	var calls int

	circuit := func(context.Context) (int, error) {
		calls++

		return 1, nil
	}

	d := DebounceLastContext(circuit, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := d(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}

	if calls != 0 {
		t.Fatalf("circuit invoked %d times, want 0", calls)
	}
}

func TestDebounceLastSupersededCircuitCancelled(t *testing.T) {
	started := make(chan struct{})
	gotCancel := make(chan error, 1)

	var (
		mu          sync.Mutex
		invocations int
	)

	circuit := func(ctx context.Context) (int, error) {
		mu.Lock()
		invocations++
		first := invocations == 1
		mu.Unlock()

		if !first {
			return 1, nil
		}

		close(started)

		<-ctx.Done()

		gotCancel <- ctx.Err()

		return 0, ctx.Err()
	}

	d := DebounceLastContext(circuit, 10*time.Millisecond)

	firstDone := make(chan struct{})

	go func() {
		defer close(firstDone)

		_, _ = d(context.Background())
	}()

	// Wait until the first circuit is running, then supersede it.
	<-started

	secondDone := make(chan struct{})

	go func() {
		defer close(secondDone)

		_, _ = d(context.Background())
	}()

	// The superseded circuit must observe the cancellation.
	select {
	case err := <-gotCancel:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("superseded circuit was not cancelled")
	}

	<-firstDone
	<-secondDone
}
