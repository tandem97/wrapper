// Package exponential provides an exponential backoff generator.
//
// The generator produces an increasing sequence of delays, starting at
// base and doubling on every step until the value reaches cap, after
// which cap is returned for every subsequent call. It is typically used
// to space out retries of failed operations.
package exponential

import (
	"sync"
	"time"
)

const (
	// MinBase is the smallest delay allowed for base.
	MinBase = time.Millisecond

	// DefaultBase is the initial delay used when New is called without
	// WithBase.
	DefaultBase = time.Second

	// DefaultCap is the maximum delay used when New is called without
	// WithCap.
	DefaultCap = 30 * time.Second
)

// Backoff is a concurrent-safe exponential backoff generator.
type Backoff struct {
	base    time.Duration
	cap     time.Duration
	backoff time.Duration
	mu      sync.Mutex
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

// WithCap sets the maximum delay that Backoff can return.
// cap must be positive and greater than or equal to base, otherwise New
// panics.
func WithCap(cap time.Duration) Opt {
	return func(b *Backoff) {
		b.cap = cap
	}
}

// New creates a Backoff generator from the given options.
//
// It panics if the resulting configuration is invalid: base must be at
// least MinBase, cap must be positive, and cap must be greater than or
// equal to base.
func New(opts ...Opt) *Backoff {
	backoff := &Backoff{
		base: DefaultBase,
		cap:  DefaultCap,
	}

	for _, opt := range opts {
		opt(backoff)
	}

	if backoff.base < MinBase {
		panic("exponential: base must be at least " + MinBase.String())
	}

	if backoff.cap <= 0 {
		panic("exponential: cap must be positive")
	}

	if backoff.cap < backoff.base {
		panic("exponential: cap must be greater than or equal to base")
	}

	backoff.backoff = backoff.base

	return backoff
}

// Backoff returns the next delay in the sequence and advances the
// generator. With the default options the sequence is 1s, 2s, 4s, 8s,
// 16s, followed by cap (30s) forever.
//
// Backoff is safe for concurrent use.
func (b *Backoff) Backoff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	backoff := b.backoff
	if backoff >= b.cap {
		return b.cap
	}

	next := backoff << 1

	// If the doubling overflows int64 (next < backoff for positive
	// values) or overshoots cap, the sequence has saturated: pin the
	// state at cap and still return the last representable value
	// instead of cap.
	if next < backoff || next >= b.cap {
		b.backoff = b.cap
	} else {
		b.backoff = next
	}

	return backoff
}

// Reset restarts the sequence so that the next call to Backoff returns
// base again.
func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.backoff = b.base
}
