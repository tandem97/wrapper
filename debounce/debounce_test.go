package debounce

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDebounceFirstCachesResult(t *testing.T) {
	var (
		calls   int
		circuit = func() (int, error) {
			calls++

			return 42, nil
		}
		d = DebounceFirst(circuit, 50*time.Millisecond)
	)

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
	var (
		calls int
		mu    sync.Mutex
		wg    sync.WaitGroup

		circuit = func() (int, error) {
			mu.Lock()
			calls++
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)

			return 7, nil
		}
		d          = DebounceFirst(circuit, time.Second)
		goroutines = 16
	)

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
	var (
		started = make(chan struct{})
		circuit = func(ctx context.Context) (int, error) {
			close(started)

			<-ctx.Done()

			return 0, ctx.Err()
		}
		d               = DebounceFirstContext(circuit, time.Second)
		callCtx, cancel = context.WithCancel(context.Background())
	)

	defer cancel()

	go func() { _, _ = d(callCtx) }()

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
	var (
		calls   int
		circuit = func() (string, error) {
			calls++

			return "ok", nil
		}
		d = DebounceLast(circuit, 20*time.Millisecond)
	)

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
	var (
		calls   int
		circuit = func() (int, error) {
			calls++

			return 1, nil
		}
		d       = DebounceLast(circuit, 30*time.Millisecond)
		results = make(chan error, 2)
	)

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
	var (
		d           = DebounceLastContext(func(context.Context) (int, error) { return 1, nil }, time.Minute)
		ctx, cancel = context.WithCancel(context.Background())
	)

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
	var (
		boom    = errors.New("boom")
		calls   int
		circuit = func() (int, error) {
			calls++

			return 0, boom
		}
		d = DebounceFirst(circuit, 50*time.Millisecond)
	)

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
	var (
		calls   int
		circuit = func(context.Context) (int, error) {
			calls++

			return 1, nil
		}
		d           = DebounceFirstContext(circuit, time.Second)
		ctx, cancel = context.WithCancel(context.Background())
	)

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
	var (
		got     = make(chan context.Context, 1)
		circuit = func(ctx context.Context) (int, error) {
			got <- ctx

			return 1, nil
		}
		d           = DebounceFirstContext(circuit, time.Second)
		ctx, cancel = context.WithCancel(context.Background())
	)

	defer cancel()

	if _, err := d(ctx); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	if c := <-got; c != ctx {
		t.Fatal("circuit did not receive the first caller's context")
	}
}

func TestDebounceLastConcurrent(t *testing.T) {
	var (
		calls int
		mu    sync.Mutex
		wg    sync.WaitGroup

		circuit = func() (int, error) {
			mu.Lock()
			calls++
			mu.Unlock()

			return 1, nil
		}
		d          = DebounceLast(circuit, 50*time.Millisecond)
		goroutines = 16
	)

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
	var (
		calls   int
		circuit = func(context.Context) (int, error) {
			calls++

			return 1, nil
		}
		d           = DebounceLastContext(circuit, time.Second)
		ctx, cancel = context.WithCancel(context.Background())
	)

	cancel()

	_, err := d(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}

	if calls != 0 {
		t.Fatalf("circuit invoked %d times, want 0", calls)
	}
}

// A circuit that has not started yet is superseded: the timer is stopped
// and the waiting caller receives ErrDebounce. The circuit never runs for
// it at all, which is the point of trailing-edge debounce.
func TestDebounceLastSupersededBeforeStartNeverRuns(t *testing.T) {
	type callerKey struct{}

	var (
		mu      sync.Mutex
		ranFor  = map[string]int{}
		circuit = func(ctx context.Context) (int, error) {
			who, _ := ctx.Value(callerKey{}).(string)

			mu.Lock()
			ranFor[who]++
			mu.Unlock()

			return 1, nil
		}
		d = DebounceLastContext(circuit, 50*time.Millisecond)
	)

	ctx := func(who string) context.Context {
		return context.WithValue(context.Background(), callerKey{}, who)
	}

	firstDone := make(chan error, 1)

	go func() {
		_, err := d(ctx("superseded"))
		firstDone <- err
	}()

	// Supersede it well before the timer fires.
	time.Sleep(10 * time.Millisecond)

	go func() { _, _ = d(ctx("winner")) }()

	select {
	case err := <-firstDone:
		if !errors.Is(err, ErrDebounce) {
			t.Fatalf("got %v, want ErrDebounce", err)
		}
	case <-time.After(time.Second):
		t.Fatal("superseded caller never returned")
	}

	// Past the original deadline, its circuit must still not have run.
	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if ranFor["superseded"] != 0 {
		t.Fatalf("circuit ran %d times for the superseded caller, want 0", ranFor["superseded"])
	}

	if ranFor["winner"] != 1 {
		t.Fatalf("circuit ran %d times for the surviving caller, want 1", ranFor["winner"])
	}
}

// A circuit that has already started is left to finish: its caller gets
// the result, not ErrDebounce. Throwing away work in progress would mean a
// circuit slower than the gap between calls never completes.
func TestDebounceLastRunningCircuitIsNotCancelled(t *testing.T) {
	var (
		started   = make(chan struct{})
		release   = make(chan struct{})
		mu        sync.Mutex
		runnings  int
		inFlight  int
		maxFlight int
		circuit   = func(ctx context.Context) (int, error) {
			mu.Lock()
			runnings++

			if inFlight++; inFlight > maxFlight {
				maxFlight = inFlight
			}

			first := runnings == 1
			mu.Unlock()

			if first {
				close(started)
			}

			select {
			case <-release:
			case <-ctx.Done():
			}

			mu.Lock()
			inFlight--
			mu.Unlock()

			return 1, nil
		}
		d = DebounceLastContext(circuit, 10*time.Millisecond)
	)

	// The first call runs its circuit, which blocks until released.
	firstDone := make(chan error, 1)

	go func() {
		_, err := d(context.Background())
		firstDone <- err
	}()

	<-started // the trailing circuit is now in flight

	// A newer call supersedes it, but the circuit has already started.
	secondDone := make(chan error, 1)

	go func() {
		_, err := d(context.Background())
		secondDone <- err
	}()

	// Give the newer call time to reach the debounce and try to cancel.
	time.Sleep(50 * time.Millisecond)

	close(release) // let the in-flight circuit finish

	// The running circuit delivers its result rather than ErrDebounce.
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("got %v, want the running circuit to finish with its result", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the running circuit was cancelled instead of being allowed to finish")
	}

	<-secondDone
}

// A caller that is waiting on someone else's run can still give up: it
// returns its own context error while the run carries on for the caller
// that started it. This is the one case where a caller's own context is
// honoured after the window is open.
func TestDebounceFirstWaiterCanAbandon(t *testing.T) {
	var (
		started = make(chan struct{})
		release = make(chan struct{})

		circuit = func(ctx context.Context) (int, error) {
			select {
			case <-started:
			default:
				close(started)
			}

			select {
			case <-release:
				return 7, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}

		d = DebounceFirstContext(circuit, time.Minute)
	)

	// The initiator runs the circuit and stays with it.
	initiator := make(chan error, 1)

	go func() {
		_, err := d(context.Background())
		initiator <- err
	}()

	<-started

	// A second caller waits on that run and gives up on its own context.
	waiterCtx, cancelWaiter := context.WithCancel(context.Background())

	waiter := make(chan error, 1)

	go func() {
		_, err := d(waiterCtx)
		waiter <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancelWaiter()

	select {
	case err := <-waiter:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter got %v, want its own context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not give up on its own context")
	}

	// The run continues for the initiator.
	close(release)

	select {
	case err := <-initiator:
		if err != nil {
			t.Fatalf("initiator got %v, want the circuit's result", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initiator never returned")
	}
}
