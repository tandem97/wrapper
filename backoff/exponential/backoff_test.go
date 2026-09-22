package exponential

import (
	"math"
	"sync"
	"testing"
	"time"
)

func TestBackoffSequence(t *testing.T) {
	b := New(WithBase(10*time.Millisecond), WithCap(40*time.Millisecond))

	want := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
		40 * time.Millisecond,
		40 * time.Millisecond,
	}

	for i, w := range want {
		if got := b.Backoff(); got != w {
			t.Fatalf("call %d: got %v, want %v", i+1, got, w)
		}
	}
}

func TestBackoffDefaults(t *testing.T) {
	b := New()

	want := []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}

	for i, w := range want {
		if got := b.Backoff(); got != w {
			t.Fatalf("call %d: got %v, want %v", i+1, got, w)
		}
	}
}

func TestBackoffReset(t *testing.T) {
	b := New(WithBase(10*time.Millisecond), WithCap(40*time.Millisecond))

	b.Backoff()
	b.Backoff()
	b.Reset()

	if got, want := b.Backoff(), 10*time.Millisecond; got != want {
		t.Fatalf("after reset: got %v, want %v", got, want)
	}
}

func TestBackoffSaturatesAtCap(t *testing.T) {
	var (
		max = time.Duration(math.MaxInt64)
		b   = New(WithBase(max/4), WithCap(max))
	)

	// (max/4)<<1 and (max/4)<<2 fit in int64; the next doubling would
	// overflow, so the sequence pins at cap after returning the last
	// representable value.
	want := []time.Duration{
		max / 4,
		(max / 4) << 1,
		(max / 4) << 2,
		max,
		max,
	}

	for i, w := range want {
		if got := b.Backoff(); got != w {
			t.Fatalf("call %d: got %v, want %v", i+1, got, w)
		}
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
		{"negative cap", []Opt{WithCap(-time.Second)}},
		{"cap below base", []Opt{WithBase(2 * time.Second), WithCap(time.Second)}},
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
	var (
		b          = New(WithBase(time.Millisecond), WithCap(time.Second))
		goroutines = 32
		calls      = 1000
		wg         sync.WaitGroup
	)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for i := 0; i < calls; i++ {
				if d := b.Backoff(); d <= 0 || d > time.Second {
					t.Errorf("Backoff returned %v out of range", d)
				}
			}
		}()
	}

	wg.Wait()

	b.Reset()

	if got, want := b.Backoff(), time.Millisecond; got != want {
		t.Fatalf("after concurrent use and reset: got %v, want %v", got, want)
	}
}

func TestBackoffBaseEqualsCap(t *testing.T) {
	b := New(WithBase(10*time.Millisecond), WithCap(10*time.Millisecond))

	for i := 0; i < 3; i++ {
		if got := b.Backoff(); got != 10*time.Millisecond {
			t.Fatalf("call %d: got %v, want 10ms", i+1, got)
		}
	}
}
