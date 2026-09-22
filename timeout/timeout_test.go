package timeout

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTimeoutReturnsResult(t *testing.T) {
	f := func() (string, error) {
		return "ok", nil
	}

	wrapped := Timeout(f)

	res, err := wrapped(context.Background())
	if err != nil || res != "ok" {
		t.Fatalf("got (%v, %v), want (ok, nil)", res, err)
	}
}

func TestTimeoutReturnsResultBeforeDeadline(t *testing.T) {
	f := func() (int, error) {
		time.Sleep(5 * time.Millisecond)

		return 42, nil
	}

	wrapped := Timeout(f)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	res, err := wrapped(ctx)
	if err != nil || res != 42 {
		t.Fatalf("got (%v, %v), want (42, nil)", res, err)
	}
}

func TestTimeoutReturnsErrorFromF(t *testing.T) {
	boom := errors.New("boom")

	wrapped := Timeout(func() (int, error) {
		return 0, boom
	})

	_, err := wrapped(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}
}

func TestTimeoutFiresOnDeadline(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	f := func() (int, error) {
		close(started)
		<-release

		return 0, nil
	}

	wrapped := Timeout(f)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	done := make(chan struct{})

	var (
		res int
		err error
	)

	go func() {
		defer close(done)

		res, err = wrapped(ctx)
	}()

	<-started
	<-done

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}

	if res != 0 {
		t.Fatalf("got %v, want zero value", res)
	}

	close(release)
}

func TestTimeoutFiresOnCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	f := func() (int, error) {
		close(started)
		<-release

		return 0, nil
	}

	wrapped := Timeout(f)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, err := wrapped(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v, want context.Canceled", err)
		}
	}()

	<-started
	cancel()
	<-done

	close(release)
}

func TestTimeoutAlreadyCancelledDoesNotInvokeF(t *testing.T) {
	var calls int

	wrapped := Timeout(func() (int, error) {
		calls++

		return 1, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := wrapped(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}

	if calls != 0 {
		t.Fatalf("f invoked %d times, want 0", calls)
	}
}

func TestTimeoutReturnsCause(t *testing.T) {
	cause := errors.New("custom cause")
	started := make(chan struct{})
	release := make(chan struct{})

	f := func() (int, error) {
		close(started)
		<-release

		return 0, nil
	}

	wrapped := Timeout(f)

	ctx, cancel := context.WithCancelCause(context.Background())

	done := make(chan struct{})

	var err error

	go func() {
		defer close(done)

		_, err = wrapped(ctx)
	}()

	<-started
	cancel(cause)
	<-done

	if !errors.Is(err, cause) {
		t.Fatalf("got %v, want cause %v", err, cause)
	}

	close(release)
}

func TestTimeoutGoroutineCompletesAfterTimeout(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})

	f := func() (int, error) {
		close(started)
		<-release
		close(finished)

		return 0, nil
	}

	wrapped := Timeout(f)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, _ = wrapped(ctx)

	<-started
	close(release)

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("wrapped function did not finish after timeout")
	}
}

func TestTimeoutConcurrent(t *testing.T) {
	f := func() (int, error) {
		time.Sleep(time.Millisecond)

		return 1, nil
	}

	wrapped := Timeout(f)
	goroutines := 32
	calls := 50

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := 0; i < calls; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				res, err := wrapped(ctx)

				cancel()

				if err != nil || res != 1 {
					t.Errorf("got (%v, %v), want (1, nil)", res, err)
				}
			}
		}()
	}

	wg.Wait()
}
