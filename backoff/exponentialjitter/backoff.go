// Package exponentialjitter provides an exponential backoff generator
// with randomized jitter.
//
// Delays grow by a constant multiplier on every retry until they reach
// cap, and are then spread by a random factor around the exponential
// curve: each delay is multiplied by a value in [1-jitter, 1+jitter).
// The randomness helps a fleet of clients avoid synchronized retry
// storms.
package exponentialjitter

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

const (
	// MinBase is the smallest delay allowed for base.
	MinBase = time.Millisecond

	// DefaultBase is the initial delay used when New is called without
	// WithBase.
	DefaultBase = 1 * time.Second

	// DefaultCap is the maximum delay used when New is called without
	// WithCap.
	DefaultCap = 30 * time.Second

	// DefaultMultiplier is the growth factor used when New is called
	// without WithMultiplier.
	DefaultMultiplier = 1.6

	// DefaultJitter is the jitter amplitude used when New is called
	// without WithJitter.
	DefaultJitter = 0.2
)

// Backoff is a concurrent-safe exponential backoff generator with jitter.
type Backoff struct {
	base       time.Duration
	cap        time.Duration
	jitter     float64
	multiplier float64
	current    float64 // current un-jittered delay level, in nanoseconds
	reachedCap bool
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

// WithCap sets the maximum delay that Backoff can return, before jitter
// is applied. Because jitter is applied on top of the capped value, the
// actually returned delay may reach up to cap*(1+jitter).
// cap must be positive and greater than or equal to base, otherwise New
// panics.
func WithCap(cap time.Duration) Opt {
	return func(b *Backoff) {
		b.cap = cap
	}
}

// WithMultiplier sets the factor by which the delay grows on every call.
// multiplier must be finite and greater than or equal to 1, otherwise
// New panics.
func WithMultiplier(multiplier float64) Opt {
	return func(b *Backoff) {
		b.multiplier = multiplier
	}
}

// WithJitter sets the amplitude of the random spread applied to each
// delay. With jitter j, the returned delay is multiplied by a random
// factor in [1-j, 1+j). With jitter 1 the factor can be 0, so the
// returned delay may be 0. jitter must be within [0, 1], otherwise New
// panics.
func WithJitter(jitter float64) Opt {
	return func(b *Backoff) {
		b.jitter = jitter
	}
}

// New creates a Backoff generator from the given options.
//
// It panics if the resulting configuration is invalid: base must be at
// least MinBase, cap must be positive and greater than or equal to base,
// multiplier must be finite and greater than or equal to 1, and jitter
// must be within [0, 1].
func New(opts ...Opt) *Backoff {
	backoff := &Backoff{
		base:       DefaultBase,
		cap:        DefaultCap,
		multiplier: DefaultMultiplier,
		jitter:     DefaultJitter,
		rand:       rand.New(rand.NewSource(rand.Int63())),
	}

	for _, opt := range opts {
		opt(backoff)
	}

	if backoff.base < MinBase {
		panic("exponentialjitter: base must be at least " + MinBase.String())
	}

	if backoff.cap <= 0 {
		panic("exponentialjitter: cap must be positive")
	}

	if math.IsNaN(backoff.multiplier) || math.IsInf(backoff.multiplier, 0) || backoff.multiplier < 1 {
		panic("exponentialjitter: multiplier must be finite and greater than or equal to 1")
	}

	if math.IsNaN(backoff.jitter) || backoff.jitter < 0 || backoff.jitter > 1 {
		panic("exponentialjitter: jitter must be within [0, 1]")
	}

	if backoff.cap < backoff.base {
		panic("exponentialjitter: cap must be greater than or equal to base")
	}

	backoff.current = float64(backoff.base)

	return backoff
}

// Backoff returns the next delay. The base is multiplied by multiplier
// on every call, capped at cap, and then randomized by a random factor
// in [1-jitter, 1+jitter). Jitter is applied to the first delay too.
//
// Backoff is safe for concurrent use.
func (b *Backoff) Backoff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	level := b.current

	if !b.reachedCap {
		next := level * b.multiplier
		if next >= float64(b.cap) {
			b.current = float64(b.cap)
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

// saturatingDuration converts f nanoseconds to a time.Duration, clamping
// the result to [0, math.MaxInt64].
func saturatingDuration(f float64) time.Duration {
	switch {
	case math.IsNaN(f) || f <= 0:
		return 0
	case f >= math.MaxInt64:
		return time.Duration(math.MaxInt64)
	default:
		return time.Duration(f)
	}
}
