package exponentialjitter

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/tandem97/wrapper/backoff/exponential"
	"github.com/tandem97/wrapper/effector"
)

func TestBackoffSequenceWithoutJitter(t *testing.T) {
	b := New(
		WithBase(100*time.Millisecond),
		WithCeiling(250*time.Millisecond),
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
	// 0.9 keeps the factor inside [0.1, 1.9), so delays land in
	// [20ms, 380ms). jitter is now capped below 1.
	const (
		jitter   = 0.9
		base     = 200 * time.Millisecond
		minDelay = 20 * time.Millisecond  // base * (1 - jitter)
		maxDelay = 380 * time.Millisecond // base * (1 + jitter)
	)

	b := New(
		WithBase(base),
		WithCeiling(base),
		WithMultiplier(1),
		WithJitter(jitter),
	)

	for i := 0; i < 1000; i++ {
		d := b.Backoff()
		if d < minDelay || d > maxDelay {
			t.Fatalf("delay %v out of [%v, %v]", d, minDelay, maxDelay)
		}
	}
}

func TestBackoffLargeValues(t *testing.T) {
	// MaxCeiling is the largest delay a generator can be configured
	// with, and the largest it represents exactly in nanoseconds.
	b := New(
		WithBase(MaxCeiling/2),
		WithCeiling(MaxCeiling),
		WithMultiplier(2),
		WithJitter(0),
	)

	want := []time.Duration{MaxCeiling / 2, MaxCeiling, MaxCeiling}

	for i, w := range want {
		if got := b.Backoff(); got != w {
			t.Fatalf("call %d: got %v, want %v", i+1, got, w)
		}
	}
}

func TestBackoffConfiguredDelaysAreExact(t *testing.T) {
	// Below MaxCeiling the level is an exact integer number of
	// nanoseconds, so a generator without jitter returns exactly the
	// configured delay. Above MaxCeiling the float64 curve would round
	// it, which is why New rejects those ceilings.
	base := MaxCeiling / 4

	b := New(
		WithBase(base),
		WithCeiling(MaxCeiling),
		WithMultiplier(1),
		WithJitter(0),
	)

	if got := b.Backoff(); got != base {
		t.Fatalf("got %v, want the configured base %v", got, base)
	}
}

func TestBackoffReset(t *testing.T) {
	b := New(
		WithBase(time.Second),
		WithCeiling(10*time.Second),
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
		{"non-positive ceiling", []Opt{WithCeiling(0)}},
		{"negative ceiling", []Opt{WithCeiling(-time.Second)}},
		{"ceiling below base", []Opt{WithBase(2 * time.Second), WithCeiling(time.Second)}},
		{"ceiling above MaxCeiling", []Opt{WithCeiling(MaxCeiling + time.Nanosecond)}},
		{"ceiling at MaxInt64", []Opt{WithBase(MaxCeiling), WithCeiling(time.Duration(math.MaxInt64))}},
		{"multiplier below 1", []Opt{WithMultiplier(0.5)}},
		{"multiplier NaN", []Opt{WithMultiplier(math.NaN())}},
		{"multiplier +Inf", []Opt{WithMultiplier(math.Inf(1))}},
		{"multiplier -Inf", []Opt{WithMultiplier(math.Inf(-1))}},
		{"jitter negative", []Opt{WithJitter(-0.1)}},
		{"jitter at 1", []Opt{WithJitter(1)}},
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
	var (
		b = New(
			WithBase(time.Millisecond),
			WithCeiling(10*time.Second),
			WithMultiplier(2),
			WithJitter(0.5),
		)
		goroutines = 32
		calls      = 1000
		wg         sync.WaitGroup
	)

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
		WithCeiling(200*time.Millisecond),
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
		WithCeiling(150*time.Millisecond),
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
			WithCeiling(2*time.Second),
			WithMultiplier(2),
			WithJitter(0.5),
			WithSeed(42, 1),
		)
	}

	a, b := newBackoff(), newBackoff()

	for i := 0; i < 100; i++ {
		if da, db := a.Backoff(), b.Backoff(); da != db {
			t.Fatalf("call %d: got %v and %v, want equal", i+1, da, db)
		}
	}
}

func TestBackoffResetIsNotReproducible(t *testing.T) {
	mk := func() *Backoff {
		return New(
			WithBase(100*time.Millisecond),
			WithCeiling(2*time.Second),
			WithMultiplier(2),
			WithJitter(0.5),
			WithSeed(42, 1),
		)
	}

	drain := func(b *Backoff) []time.Duration {
		out := make([]time.Duration, 0, 4)
		for i := 0; i < 4; i++ {
			out = append(out, b.Backoff())
		}

		return out
	}

	// Two fresh instances with the same seed agree.
	freshA, freshB := drain(mk()), drain(mk())
	if fmt.Sprint(freshA) != fmt.Sprint(freshB) {
		t.Fatalf("fresh instances diverge: %v vs %v", freshA, freshB)
	}

	// Reset rewinds the curve but not the random stream, so a reset
	// instance diverges from a fresh one. This is the documented
	// behaviour: use Clone for a reproducible session.
	reused := mk()
	drain(reused)
	reused.Reset()
	afterReset := drain(reused)

	if fmt.Sprint(afterReset) == fmt.Sprint(freshA) {
		t.Fatal("reset unexpectedly rewound the random stream")
	}
}

func TestCloneReproducesSeededSequence(t *testing.T) {
	opts := []Opt{
		WithBase(100 * time.Millisecond),
		WithCeiling(2 * time.Second),
		WithMultiplier(2),
		WithJitter(0.5),
		WithSeed(7, 9),
	}

	drain := func(b *Backoff) []time.Duration {
		out := make([]time.Duration, 0, 8)
		for i := 0; i < 8; i++ {
			out = append(out, b.Backoff())
		}

		return out
	}

	original := New(opts...)
	clone := mustClone(t, original)

	// Both start at base, and a seeded clone draws from the same random
	// source, so the sequences must agree step for step.
	fromClone, fromOriginal := drain(clone), drain(original)

	if got, want := fmt.Sprint(fromClone), fmt.Sprint(fromOriginal); got != want {
		t.Fatalf("seeded clone diverges:\n got %s\nwant %s", got, want)
	}

	// The instances are independent: drain consumed exactly len(fromOriginal)
	// steps of each seeded stream, so the next value must equal the one a
	// generator built from the same options produces at the same position.
	// A clone sharing state would have advanced the original by twice that.
	reference := New(opts...)
	for i := 0; i < len(fromOriginal); i++ {
		reference.Backoff()
	}

	if got, want := original.Backoff(), reference.Backoff(); got != want {
		t.Fatalf("original was disturbed by the clone: got %v, want %v", got, want)
	}
}

func TestCloneUnseededDrawsFreshRandom(t *testing.T) {
	original := New(
		WithBase(100*time.Millisecond),
		WithCeiling(2*time.Second),
		WithMultiplier(2),
		WithJitter(0.5),
	)

	original.Backoff()

	clone := mustClone(t, original)

	// The clone starts at base, not at the original's advanced position.
	if clone.current != float64(100*time.Millisecond) {
		t.Fatalf("clone level %v, want base %v", clone.current, 100*time.Millisecond)
	}

	// An unseeded clone draws a fresh random source, so its delays are
	// independent of the original's: draining both repeatedly produces
	// two different sequences. Only the seeded case is reproducible.
	cloneSeq, originalSeq := make([]time.Duration, 0, 64), make([]time.Duration, 0, 64)

	for i := 0; i < 64; i++ {
		cloneSeq = append(cloneSeq, clone.Backoff())
		originalSeq = append(originalSeq, original.Backoff())
	}

	if fmt.Sprint(cloneSeq) == fmt.Sprint(originalSeq) {
		t.Fatal("unseeded clone reproduced the original's sequence")
	}

	// Both stay within the ceiling widened by the jitter amplitude:
	// base=100ms, ceiling=2s, jitter=0.5 → at most 3s.
	for _, d := range append(cloneSeq, originalSeq...) {
		if d < 0 || d > 3*time.Second {
			t.Fatalf("delay %v out of range", d)
		}
	}
}

func TestBackoffNeverExceedsCeilingTimesOnePlusJitter(t *testing.T) {
	const jitter = 0.2

	ceiling := 10 * time.Second
	bound := time.Duration(float64(ceiling) * (1 + jitter))

	b := New(
		WithBase(time.Millisecond),
		WithCeiling(ceiling),
		WithMultiplier(2),
		WithJitter(jitter),
	)

	worst := time.Duration(0)

	for i := 0; i < 200000; i++ {
		if d := b.Backoff(); d > worst {
			worst = d
		}
	}

	if worst > bound {
		t.Fatalf("worst delay %v exceeds ceiling*(1+jitter) = %v", worst, bound)
	}
}

func TestBackoffJitteredDelaysStayAboveCurve(t *testing.T) {
	const (
		base   = 100 * time.Millisecond
		jitter = 0.5
	)

	lowest := time.Duration(math.MaxInt64)

	b := New(
		WithBase(base),
		WithCeiling(base),
		WithMultiplier(1),
		WithJitter(jitter),
	)

	for i := 0; i < 200000; i++ {
		if d := b.Backoff(); d < lowest {
			lowest = d
		}
	}

	// The factor is drawn from [1-jitter, 1+jitter), so the delay never
	// drops below base*(1-jitter).
	if floor := time.Duration(float64(base) * (1 - jitter)); lowest < floor {
		t.Fatalf("lowest delay %v below base*(1-jitter) = %v", lowest, floor)
	}
}

func TestCloneConcurrentCallsGetIndependentCurves(t *testing.T) {
	b := New(
		WithBase(time.Millisecond),
		WithCeiling(time.Hour),
		WithMultiplier(2),
		WithJitter(0),
	)

	const sessions = 16

	var (
		wg     sync.WaitGroup
		firsts = make([]time.Duration, 0, sessions)
		start  = make(chan struct{})
	)

	for i := 0; i < sessions; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			clone := mustClone(t, b)

			<-start

			firsts = append(firsts, clone.Backoff())
		}()
	}

	close(start)
	wg.Wait()

	for i, d := range firsts {
		if d != time.Millisecond {
			t.Fatalf("session %d: first delay %v, want %v", i, d, time.Millisecond)
		}
	}
}

// mustClone clones b and asserts the result is a *Backoff, which is what
// this generator returns.
func mustClone(t *testing.T, b effector.Cloneable) *Backoff {
	t.Helper()

	clone, ok := b.Clone().(*Backoff)
	if !ok {
		t.Fatal("Clone did not return a *Backoff")
	}

	return clone
}

// TestSaturatingDuration covers the guard directly. Backoff cannot reach
// the clamp, since the level is capped at MaxCeiling and the factor is
// below 2, so this keeps the guard honest rather than leaving it
// uncovered.
func TestSaturatingDuration(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want time.Duration
	}{
		{"below MaxInt64", 1500, 1500},
		{"exact MaxInt64", math.MaxInt64, time.Duration(math.MaxInt64)},
		{"above MaxInt64", math.MaxInt64 * 2, time.Duration(math.MaxInt64)},
		{"above MaxInt64 by float", math.Inf(1), time.Duration(math.MaxInt64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := saturatingDuration(tt.in); got != tt.want {
				t.Fatalf("saturatingDuration(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestBackoffDifferentSeeds(t *testing.T) {
	newBackoff := func(seed1, seed2 uint64) *Backoff {
		return New(
			WithBase(100*time.Millisecond),
			WithCeiling(2*time.Second),
			WithMultiplier(2),
			WithJitter(0.5),
			WithSeed(seed1, seed2),
		)
	}

	// The two halves must both matter: generators differing only in the
	// first or only in the second seed must not share a sequence.
	pairs := [][2]uint64{
		{1, 2}, // differs in both halves
		{1, 9}, // differs in the second half only
		{8, 2}, // differs in the first half only
	}

	for _, pair := range pairs {
		a, b := newBackoff(pair[0], pair[1]), newBackoff(2, 2)

		different := false

		for i := 0; i < 100; i++ {
			if a.Backoff() != b.Backoff() {
				different = true

				break
			}
		}

		if !different {
			t.Fatalf("seeds (%d, %d) and (2, 2) produce identical sequences", pair[0], pair[1])
		}
	}
}

func TestBackoffSameSeedBothHalves(t *testing.T) {
	// Passing one value twice is valid: it yields a distinct generator,
	// just seeded from less state.
	opts := func(seed uint64) []Opt {
		return []Opt{
			WithBase(100 * time.Millisecond),
			WithCeiling(2 * time.Second),
			WithMultiplier(2),
			WithJitter(0.5),
			WithSeed(seed, seed),
		}
	}

	a, b := New(opts(1)...), New(opts(1)...)

	same := true

	for i := 0; i < 100; i++ {
		if a.Backoff() != b.Backoff() {
			same = false

			break
		}
	}

	if !same {
		t.Fatal("identical seeds must produce identical sequences")
	}

	c := New(opts(2)...)

	different := false

	for i := 0; i < 100; i++ {
		if c.Backoff() != a.Backoff() {
			different = true

			break
		}
	}

	if !different {
		t.Fatal("different seeds must produce different sequences")
	}
}

func BenchmarkBackoff(b *testing.B) {
	g := New(
		WithBase(time.Millisecond),
		WithCeiling(time.Hour),
		WithMultiplier(1.6),
	)

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		g.Backoff()
	}
}

func BenchmarkBackoffParallel(b *testing.B) {
	g := New(
		WithBase(time.Millisecond),
		WithCeiling(time.Hour),
		WithMultiplier(1.6),
	)

	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			g.Backoff()
		}
	})
}

func BenchmarkBackoffClone(b *testing.B) {
	g := New(
		WithBase(time.Millisecond),
		WithCeiling(time.Hour),
		WithMultiplier(1.6),
	)

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		g.Clone().Backoff()
	}
}

// The zero value is constructible because the type is exported. It must
// fail loudly rather than return a plausible-looking delay.
func TestZeroValuePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("zero value did not panic")
		}
	}()

	b := &Backoff{}
	b.Backoff()
}

// The two packages use different words for the same knob and guarantee
// different things with it. This pins that asymmetry: exponential never
// exceeds cap, exponentialjitter reaches ceiling*(1+jitter) — and does so
// about half the time once the curve has saturated.
func TestCeilingExceedsCapAsymmetry(t *testing.T) {
	const (
		bound  = time.Second
		jitter = 0.2
	)

	// exponential never returns more than cap.
	plain := exponential.New(
		exponential.WithBase(bound),
		exponential.WithCap(bound),
	)

	for i := 0; i < 100000; i++ {
		if d := plain.Backoff(); d > bound {
			t.Fatalf("exponential returned %v above cap %v", d, bound)
		}
	}

	// exponentialjitter returns more than ceiling roughly half the time.
	spread := New(
		WithBase(bound),
		WithCeiling(bound),
		WithMultiplier(1),
		WithJitter(jitter),
	)

	const draws = 200000

	above := 0

	var worst time.Duration

	for i := 0; i < draws; i++ {
		d := spread.Backoff()
		if d > worst {
			worst = d
		}

		if d > bound {
			above++
		}
	}

	if above == 0 {
		t.Fatal("expected jittered delays to exceed the ceiling")
	}

	if limit := time.Duration(float64(bound) * (1 + jitter)); worst > limit {
		t.Fatalf("worst delay %v exceeds ceiling*(1+jitter) = %v", worst, limit)
	}
}
