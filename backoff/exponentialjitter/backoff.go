// Package exponentialjitter provides an exponential backoff generator
// with randomized jitter.
//
// Delays grow by a constant multiplier on every retry until they reach
// ceiling, and are then spread by a random factor around the exponential
// curve: each delay is multiplied by a value in [1-jitter, 1+jitter).
// The randomness helps a fleet of clients avoid synchronized retry
// storms.
//
// ceiling bounds the un-jittered curve, so the returned delay may exceed
// it by up to ceiling*jitter. A multiplier of exactly 1 yields a flat
// sequence that never reaches ceiling.
//
// A generator carries the position in its sequence, so concurrent retry
// sessions must not share one instance. Use Clone to obtain an
// independent generator for each session.
package exponentialjitter

import (
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/tandem97/wrapper/effector"
)

const (
	// MinBase is the smallest delay allowed for base.
	//
	// MinBase bounds the configured base, not the returned delay: a
	// generator created with base == MinBase and jitter > 0 returns
	// delays below MinBase.
	MinBase = time.Millisecond

	// DefaultBase is the initial delay used when New is called without
	// WithBase.
	DefaultBase = 1 * time.Second

	// DefaultCeiling is the maximum delay of the un-jittered curve used
	// when New is called without WithCeiling.
	//
	// Because jitter is applied on top of the capped value, the actually
	// returned delay may reach up to DefaultCeiling*(1+jitter).
	DefaultCeiling = 30 * time.Second

	// DefaultMultiplier is the growth factor used when New is called
	// without WithMultiplier.
	DefaultMultiplier = 1.6

	// DefaultJitter is the jitter amplitude used when New is called
	// without WithJitter.
	DefaultJitter = 0.2

	// MaxCeiling is the largest ceiling allowed, set to the largest
	// value a float64 represents exactly as an integer number of
	// nanoseconds. Beyond it the generator loses nanosecond precision.
	MaxCeiling = time.Duration(1 << 53)
)

// Backoff is a concurrent-safe exponential backoff generator with jitter.
//
// A Backoff must be built with New. The zero value is not usable: it
// carries no random source, so Backoff panics with a nil dereference.
type Backoff struct {
	base       time.Duration
	ceiling    time.Duration
	jitter     float64
	multiplier float64
	seed1      uint64
	seed2      uint64
	current    float64 // current un-jittered delay level, in nanoseconds
	reachedCap bool
	seeded     bool
	rand       *rand.Rand
	mu         sync.Mutex
}

// Opt configures a Backoff created by New.
type Opt func(b *Backoff)

// WithBase sets the initial delay of the sequence.
// base must be at least MinBase, otherwise New panics.
func WithBase(base time.Duration) Opt {
	return func(b *Backoff) {
		b.base = base
	}
}

// WithCeiling sets the maximum delay of the un-jittered curve, before
// jitter is applied. Because jitter is applied on top of the capped
// value, the actually returned delay may reach up to
// ceiling*(1+jitter): ceiling is not a bound on the returned delay.
// ceiling must be positive, greater than or equal to base, and at most
// MaxCeiling, otherwise New panics.
func WithCeiling(ceiling time.Duration) Opt {
	return func(b *Backoff) {
		b.ceiling = ceiling
	}
}

// WithMultiplier sets the factor by which the delay grows on every call.
// multiplier must be finite and greater than or equal to 1, otherwise
// New panics.
//
// A multiplier of exactly 1 produces a flat sequence that never reaches
// ceiling, so ceiling has no effect. Values just above 1 grow very
// slowly: near MaxCeiling a multiplier of 1.0000001 adds less than one
// nanosecond per step, so the curve emits runs of identical delays
// before it visibly moves. The sequence stays monotonic either way.
func WithMultiplier(multiplier float64) Opt {
	return func(b *Backoff) {
		b.multiplier = multiplier
	}
}

// WithJitter sets the amplitude of the random spread applied to each
// delay. With jitter j, the returned delay is multiplied by a random
// factor in [1-j, 1+j).
//
// Small amplitudes (0.1-0.3) spread callers around the exponential
// curve, which is what prevents synchronized retry storms. As jitter
// approaches 1 the spread becomes one-sided and the delay degenerates
// towards Uniform[0, 2*level): most draws land far below the curve, so
// callers retry almost immediately and the storms return. jitter must be
// within [0, 1).
func WithJitter(jitter float64) Opt {
	return func(b *Backoff) {
		b.jitter = jitter
	}
}

// WithSeed sets the two seeds of the internal random source, making the
// generated delays deterministic for a given configuration. By default the
// generator is seeded from the runtime.
//
// The two seeds mirror rand.NewPCG: pass two distinct values for the full
// 128 bits of state, which is what keeps independently seeded generators
// from walking the same sequence. Passing the same value twice is valid and
// yields a distinct generator per value, just with half the state entropy.
//
// The seeds make every new instance of the same configuration produce the
// same sequence of delays, which is what tests and reproducible runs need.
// They do not make Reset reproducible: Reset starts a fresh curve but
// continues the random stream, so a reset instance diverges from a newly
// created one. Use Clone to get a reproducible session.
//
// The generator uses a PCG source, whose output is not reproducible across
// Go releases, so pin the toolchain when relying on exact delays.
func WithSeed(seed1, seed2 uint64) Opt {
	return func(b *Backoff) {
		b.seed1, b.seed2, b.seeded = seed1, seed2, true
		b.rand = rand.New(rand.NewPCG(seed1, seed2)) // #nosec G404
	}
}

// New creates a Backoff generator from the given options.
//
// It panics if the resulting configuration is invalid: base must be at
// least MinBase, ceiling must be positive and greater than or equal to
// base, ceiling must be at most MaxCeiling, multiplier must be finite
// and greater than or equal to 1, and jitter must be within [0, 1).
func New(opts ...Opt) *Backoff {
	backoff := &Backoff{
		base:       DefaultBase,
		ceiling:    DefaultCeiling,
		multiplier: DefaultMultiplier,
		jitter:     DefaultJitter,
		rand:       rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), //#nosec G404
	}

	for _, opt := range opts {
		opt(backoff)
	}

	if backoff.base < MinBase {
		panic("exponentialjitter: base must be at least " + MinBase.String())
	}

	if backoff.ceiling <= 0 {
		panic("exponentialjitter: ceiling must be positive")
	}

	if math.IsNaN(backoff.multiplier) || math.IsInf(backoff.multiplier, 0) || backoff.multiplier < 1 {
		panic("exponentialjitter: multiplier must be finite and greater than or equal to 1")
	}

	if math.IsNaN(backoff.jitter) || backoff.jitter < 0 || backoff.jitter >= 1 {
		panic("exponentialjitter: jitter must be within [0, 1)")
	}

	if backoff.ceiling < backoff.base {
		panic("exponentialjitter: ceiling must be greater than or equal to base")
	}

	// The curve is tracked as a float64 number of nanoseconds, which
	// only represents integers exactly up to MaxCeiling. A larger
	// ceiling would silently round the configured delay, so reject it
	// instead of returning values that differ from what was asked for.
	if backoff.ceiling > MaxCeiling {
		panic("exponentialjitter: ceiling must be at most " + MaxCeiling.String())
	}

	backoff.current = float64(backoff.base)

	return backoff
}

// Backoff returns the next delay. The base is multiplied by multiplier
// on every call until the ceiling is reached, and the result is then
// randomized by a random factor in [1-jitter, 1+jitter). Once the
// ceiling is reached the jitter is applied to the ceiling, so delays may
// reach up to ceiling*(1+jitter). Jitter is applied to the first delay
// too.
//
// Backoff is safe for concurrent use: the generator is mutex-protected.
// Sharing one generator between concurrent retry sessions is not
// meaningful regardless, as each session advances the position and Reset
// rewinds it for all of them; use Clone for per-session isolation.
func (b *Backoff) Backoff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	level := b.current

	if !b.reachedCap {
		next := level * b.multiplier

		if next >= float64(b.ceiling) {
			b.current = float64(b.ceiling)
			b.reachedCap = true
		} else {
			b.current = next
		}
	}

	backoff := level * (1 + b.jitter*(b.rand.Float64()*2-1))

	return saturatingDuration(backoff)
}

// Reset restarts the sequence so that the next call to Backoff returns
// a delay around base again.
func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.current = float64(b.base)
	b.reachedCap = false
}

// Clone returns an independent copy of the generator with the same
// configuration, positioned at the start of a fresh sequence. The
// original is left untouched.
//
// A seeded clone reproduces the seeded sequence exactly. An unseeded
// clone draws a fresh random source, so its delays differ from the
// original's.
func (b *Backoff) Clone() effector.Backoff {
	// Built directly rather than through New: the source is allocated
	// here and assigned over the one New would allocate, keeping Clone
	// to a single allocation. The configuration is known to be valid
	// because it came from a generator New already accepted.
	clone := &Backoff{
		base:       b.base,
		ceiling:    b.ceiling,
		jitter:     b.jitter,
		multiplier: b.multiplier,
		seed1:      b.seed1,
		seed2:      b.seed2,
		seeded:     b.seeded,
		current:    float64(b.base),
	}

	if b.seeded {
		clone.rand = rand.New(rand.NewPCG(b.seed1, b.seed2)) // #nosec G404
	} else {
		clone.rand = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) // #nosec G404
	}

	return clone
}

// saturatingDuration converts f nanoseconds to a time.Duration, clamping
// the result to math.MaxInt64. Callers guarantee f is positive and
// finite: the level is at least MinBase, the multiplier is finite and
// greater than or equal to 1, and the jitter factor is within [0, 2).
//
// The clamp is unreachable through Backoff: the level never exceeds
// MaxCeiling and the factor is below 2, so f stays at least 512 times
// below MaxInt64. It is kept as a guard on that argument, and the
// in-package tests cover it directly.
func saturatingDuration(f float64) time.Duration {
	if f >= math.MaxInt64 {
		return time.Duration(math.MaxInt64)
	}

	return time.Duration(f)
}
