package future

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestWrapSlowFuncCachesResult(t *testing.T) {
	var (
		calls int
		f     = func() (int, error) {
			calls++

			time.Sleep(20 * time.Millisecond)

			return 42, nil
		}
		wrapped = WrapSlowFunc(f)
	)

	for i := 0; i < 3; i++ {
		res, err := wrapped()
		if err != nil || res != 42 {
			t.Fatalf("call %d: got (%v, %v), want (42, nil)", i+1, res, err)
		}
	}

	if calls != 1 {
		t.Fatalf("f invoked %d times, want 1", calls)
	}
}

func TestWrapSlowFuncFirstCallBlocks(t *testing.T) {
	var (
		started = make(chan struct{})
		release = make(chan struct{})
		f       = func() (string, error) {
			close(started)

			<-release

			return "ok", nil
		}
		wrapped = WrapSlowFunc(f)
		done    = make(chan struct{})
	)

	go func() {
		defer close(done)

		res, err := wrapped()
		if err != nil || res != "ok" {
			t.Errorf("got (%v, %v), want (ok, nil)", res, err)
		}
	}()

	<-started

	select {
	case <-done:
		t.Fatal("first call returned before f completed")
	case <-time.After(10 * time.Millisecond):
	}

	close(release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("first call did not return after f completed")
	}
}

func TestWrapSlowFuncConcurrent(t *testing.T) {
	var (
		calls int
		mu    sync.Mutex
		f     = func() (int, error) {
			mu.Lock()
			calls++
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)

			return 7, nil
		}
		wrapped    = WrapSlowFunc(f)
		goroutines = 16
		wg         sync.WaitGroup
	)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			res, err := wrapped()
			if err != nil || res != 7 {
				t.Errorf("got (%v, %v), want (7, nil)", res, err)
			}
		}()
	}

	wg.Wait()

	if calls != 1 {
		t.Fatalf("f invoked %d times, want 1", calls)
	}
}

func TestWrapSlowFuncCachesError(t *testing.T) {
	var (
		boom    = errors.New("boom")
		wrapped = WrapSlowFunc(func() (int, error) {
			return 0, boom
		})
	)

	for i := 0; i < 3; i++ {
		_, err := wrapped()
		if !errors.Is(err, boom) {
			t.Fatalf("call %d: expected boom, got %v", i+1, err)
		}
	}
}

func TestWrapSlowFuncStartsImmediately(t *testing.T) {
	var (
		started = make(chan struct{})
		f       = func() (int, error) {
			close(started)

			return 1, nil
		}
	)

	_ = WrapSlowFunc(f)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("f was not started immediately")
	}
}

func TestWrapSlowFuncContextCancellation(t *testing.T) {
	var (
		started = make(chan struct{})
		release = make(chan struct{})
		f       = func() (int, error) {
			close(started)

			<-release

			return 42, nil
		}
		wrapped     = WrapSlowFuncContext(f)
		ctx, cancel = context.WithCancel(context.Background())
		done        = make(chan struct{})
	)

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

func TestWrapSlowFuncContextResultCachedAfterCancellation(t *testing.T) {
	// A cancelled caller does not block.
	var (
		started = make(chan struct{})
		release = make(chan struct{})
		f       = func() (int, error) {
			close(started)

			<-release

			return 42, nil
		}
		wrapped     = WrapSlowFuncContext(f)
		ctx, cancel = context.WithCancel(context.Background())
	)

	cancel()

	if _, err := wrapped(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}

	<-started
	close(release)

	// A later caller gets the cached result.
	res, err := wrapped(context.Background())
	if err != nil || res != 42 {
		t.Fatalf("got (%v, %v), want (42, nil)", res, err)
	}
}
