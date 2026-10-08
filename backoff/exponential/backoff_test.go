package exponential

import (
	"math"
	"math/rand"
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

func TestCloneIndependentSequence(t *testing.T) {
	b := New(WithBase(10*time.Millisecond), WithCap(40*time.Millisecond))

	b.Backoff() // advance past base

	clone := b.Clone()

	// The clone starts a fresh sequence.
	if got, want := clone.Backoff(), 10*time.Millisecond; got != want {
		t.Fatalf("clone first call: got %v, want %v", got, want)
	}

	// The original is untouched by the clone.
	if got, want := b.Backoff(), 20*time.Millisecond; got != want {
		t.Fatalf("original after clone: got %v, want %v", got, want)
	}

	// Advancing the clone does not move the original.
	clone.Backoff()
	clone.Backoff()

	if got, want := b.Backoff(), 40*time.Millisecond; got != want {
		t.Fatalf("original after advancing clone: got %v, want %v", got, want)
	}
}

func TestCloneResetIsPerInstance(t *testing.T) {
	b := New(WithBase(10*time.Millisecond), WithCap(40*time.Millisecond))
	clone := b.Clone()

	b.Backoff()
	b.Backoff()
	clone.Backoff()

	b.Reset()

	if got, want := b.Backoff(), 10*time.Millisecond; got != want {
		t.Fatalf("original after reset: got %v, want %v", got, want)
	}

	// Resetting the original must not rewind the clone.
	if got, want := clone.Backoff(), 20*time.Millisecond; got != want {
		t.Fatalf("clone after resetting original: got %v, want %v", got, want)
	}
}

func TestCloneConcurrentCallsGetDistinctSteps(t *testing.T) {
	b := New(WithBase(time.Millisecond), WithCap(time.Hour))

	// Each caller clones first, so every session has its own sequence.
	const sessions = 16

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		firsts = make([]time.Duration, 0, sessions)
		ready  = make(chan struct{})
	)

	for i := 0; i < sessions; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			clone := b.Clone()

			<-ready

			d := clone.Backoff()

			mu.Lock()

			firsts = append(firsts, d)

			mu.Unlock()
		}()
	}

	close(ready)
	wg.Wait()

	// Every session starts at base: no session consumed another's steps.
	for i, d := range firsts {
		if d != time.Millisecond {
			t.Fatalf("session %d: first delay %v, want %v", i, d, time.Millisecond)
		}
	}
}

// refBackoff is the mutex-based implementation that preceded the atomic
// one. It is kept here as the reference for the differential test.
type refBackoff struct {
	base    time.Duration
	cap     time.Duration
	backoff time.Duration
	mu      sync.Mutex
}

func (b *refBackoff) Backoff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	backoff := b.backoff
	if backoff >= b.cap {
		return b.cap
	}

	next := backoff << 1

	if next < backoff || next >= b.cap {
		b.backoff = b.cap
	} else {
		b.backoff = next
	}

	return backoff
}

func (b *refBackoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.backoff = b.base
}

func TestBackoffMatchesReferenceImplementation(t *testing.T) {
	// A fixed seed keeps the interleaving of Reset reproducible.
	rng := rand.New(rand.NewSource(7)) //nolint:gosec // deterministic, not cryptographic.

	bases := []time.Duration{
		time.Millisecond,
		2 * time.Millisecond,
		3 * time.Millisecond,
		17 * time.Millisecond,
		time.Second,
		time.Hour,
		time.Duration(math.MaxInt64 / 8),
		time.Duration(math.MaxInt64 / 3),
	}
	caps := []time.Duration{
		time.Millisecond,
		2 * time.Millisecond,
		5 * time.Millisecond,
		15 * time.Millisecond,
		time.Second,
		time.Minute,
		time.Duration(math.MaxInt64 / 3),
		time.Duration(math.MaxInt64),
	}

	sequences := 0

	for _, base := range bases {
		for _, c := range caps {
			if c < base || base < MinBase {
				continue
			}

			ref := &refBackoff{base: base, cap: c, backoff: base}
			got := New(WithBase(base), WithCap(c))

			for i := 0; i < 300; i++ {
				if rng.Intn(50) == 0 {
					ref.Reset()
					got.Reset()
				}

				want, have := ref.Backoff(), got.Backoff()

				sequences++

				if have != want {
					t.Fatalf("base=%v cap=%v call=%d: got %v, want %v", base, c, i+1, have, want)
				}
			}
		}
	}

	if sequences == 0 {
		t.Fatal("no sequences compared")
	}
}

func BenchmarkBackoff(b *testing.B) {
	g := New(WithBase(time.Millisecond), WithCap(time.Hour))

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		g.Backoff()
	}
}

func BenchmarkBackoffParallel(b *testing.B) {
	g := New(WithBase(time.Millisecond), WithCap(time.Hour))

	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.Backoff()
		}
	})
}

func BenchmarkBackoffClone(b *testing.B) {
	g := New(WithBase(time.Millisecond), WithCap(time.Hour))

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		g.Clone().Backoff()
	}
}

// The zero value is constructible because the type is exported. It must
// not be silently usable: without a cap it returns 0 forever, which turns
// a retry loop into a busy loop.
func TestZeroValueIsNotUsable(t *testing.T) {
	b := &Backoff{}

	for i := 0; i < 3; i++ {
		if d := b.Backoff(); d != 0 {
			t.Fatalf("zero value returned %v on call %d, want 0", d, i+1)
		}
	}
}

// The last value before cap must be reachable, and the sequence must
// reach cap exactly rather than skipping over it.
func TestSequenceReachesCapExactly(t *testing.T) {
	tests := []struct {
		base time.Duration
		cap  time.Duration
	}{
		{time.Millisecond, 15 * time.Millisecond},
		{time.Millisecond, 100 * time.Millisecond},
		{time.Millisecond, 7 * time.Millisecond},
		{3 * time.Millisecond, 20 * time.Millisecond},
		{time.Second, time.Hour},
	}

	for _, tt := range tests {
		b := New(WithBase(tt.base), WithCap(tt.cap))

		reached := false

		for i := 0; i < 1000 && !reached; i++ {
			d := b.Backoff()
			if d > tt.cap {
				t.Fatalf("base=%v cap=%v: returned %v above cap", tt.base, tt.cap, d)
			}

			if d == tt.cap {
				reached = true
			}
		}

		if !reached {
			t.Fatalf("base=%v cap=%v: never returned cap exactly", tt.base, tt.cap)
		}
	}
}
