package exponentialjitter

import (
	"math"
	"sync"
	"testing"
	"time"
)

func TestBackoffSequenceWithoutJitter(t *testing.T) {
	b := New(
		WithBase(100*time.Millisecond),
		WithCap(250*time.Millisecond),
		WithMultiplier(2),
		WithJitter(0),
	)

	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		250 * time.Millisecond,
		250 * time.Millisecond,
	}

	for i, w := range want {
		if got := b.Backoff(); got != w {
			t.Fatalf("call %d: got %v, want %v", i+1, got, w)
		}
	}
}

func TestBackoffJitterBounds(t *testing.T) {
	b := New(
		WithBase(100*time.Millisecond),
		WithCap(200*time.Millisecond),
		WithMultiplier(1),
		WithJitter(1),
	)

	const maxDelay = 200 * time.Millisecond

	for i := 0; i < 1000; i++ {
		d := b.Backoff()
		if d < 0 || d > maxDelay {
			t.Fatalf("delay %v out of [0, 2*base]", d)
		}
	}
}

func TestBackoffLargeValues(t *testing.T) {
	b := New(
		WithBase(time.Duration(math.MaxInt64/2)),
		WithCap(time.Duration(math.MaxInt64)),
		WithMultiplier(2),
		WithJitter(0),
	)

	for i := 0; i < 5; i++ {
		if d := b.Backoff(); d <= 0 {
			t.Fatalf("call %d: non-positive delay %v", i+1, d)
		}
	}
}

func TestBackoffReset(t *testing.T) {
	b := New(
		WithBase(time.Second),
		WithCap(10*time.Second),
		WithMultiplier(2),
		WithJitter(0),
	)

	b.Backoff()
	b.Backoff()
	b.Reset()

	if got, want := b.Backoff(), time.Second; got != want {
		t.Fatalf("after reset: got %v, want %v", got, want)
	}
}

func TestNewPanics(t *testing.T) {
	tests := []struct {
		name string
		opts []Opt
	}{
		{"base below MinBase", []Opt{WithBase(time.Nanosecond)}},
		{"zero base", []Opt{WithBase(0)}},
		{"non-positive cap", []Opt{WithCap(0)}},
		{"cap below base", []Opt{WithBase(2 * time.Second), WithCap(time.Second)}},
		{"multiplier below 1", []Opt{WithMultiplier(0.5)}},
		{"multiplier NaN", []Opt{WithMultiplier(math.NaN())}},
		{"multiplier +Inf", []Opt{WithMultiplier(math.Inf(1))}},
		{"multiplier -Inf", []Opt{WithMultiplier(math.Inf(-1))}},
		{"jitter negative", []Opt{WithJitter(-0.1)}},
		{"jitter above 1", []Opt{WithJitter(1.1)}},
		{"jitter NaN", []Opt{WithJitter(math.NaN())}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("New did not panic")
				}
			}()

			New(tt.opts...)
		})
	}
}

func TestBackoffConcurrent(t *testing.T) {
	b := New(
		WithBase(time.Millisecond),
		WithCap(10*time.Second),
		WithMultiplier(2),
		WithJitter(0.5),
	)

	goroutines := 32
	calls := 1000

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for i := 0; i < calls; i++ {
				if d := b.Backoff(); d < 0 {
					t.Errorf("Backoff returned negative delay %v", d)
				}
			}
		}()
	}

	wg.Wait()
}

func TestBackoffJitterBoundsStrict(t *testing.T) {
	b := New(
		WithBase(100*time.Millisecond),
		WithCap(200*time.Millisecond),
		WithMultiplier(1),
		WithJitter(0.2),
	)

	const (
		minDelay = 80 * time.Millisecond  // base * (1 - jitter)
		maxDelay = 120 * time.Millisecond // base * (1 + jitter)
	)

	for i := 0; i < 1000; i++ {
		d := b.Backoff()
		if d < minDelay || d > maxDelay {
			t.Fatalf("delay %v out of [%v, %v]", d, minDelay, maxDelay)
		}
	}
}

func TestBackoffResetAfterCap(t *testing.T) {
	b := New(
		WithBase(100*time.Millisecond),
		WithCap(150*time.Millisecond),
		WithMultiplier(2),
		WithJitter(0),
	)

	// 100ms, then capped at 150ms forever.
	b.Backoff()
	b.Backoff()
	b.Backoff()

	b.Reset()

	if got, want := b.Backoff(), 100*time.Millisecond; got != want {
		t.Fatalf("after reset: got %v, want %v", got, want)
	}
}

func TestBackoffDeterministicWithSeed(t *testing.T) {
	newBackoff := func() *Backoff {
		return New(
			WithBase(100*time.Millisecond),
			WithCap(2*time.Second),
			WithMultiplier(2),
			WithJitter(0.5),
			WithSeed(42),
		)
	}

	a, b := newBackoff(), newBackoff()

	for i := 0; i < 100; i++ {
		if da, db := a.Backoff(), b.Backoff(); da != db {
			t.Fatalf("call %d: got %v and %v, want equal", i+1, da, db)
		}
	}
}

func TestBackoffDifferentSeeds(t *testing.T) {
	newBackoff := func(seed int64) *Backoff {
		return New(
			WithBase(100*time.Millisecond),
			WithCap(2*time.Second),
			WithMultiplier(2),
			WithJitter(0.5),
			WithSeed(seed),
		)
	}

	a, b := newBackoff(1), newBackoff(2)

	different := false

	for i := 0; i < 100; i++ {
		if a.Backoff() != b.Backoff() {
			different = true

			break
		}
	}

	if !different {
		t.Fatal("sequences with different seeds are identical")
	}
}
