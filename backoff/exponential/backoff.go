// Package exponential provides an exponential backoff generator.
//
// The generator produces an increasing sequence of delays, starting at
// base and doubling on every step until the value reaches cap, after
// which cap is returned for every subsequent call. It is typically used
// to space out retries of failed operations.
//
// A generator carries the position in its sequence, so concurrent retry
// sessions must not share one instance. Use Clone to obtain an
// independent generator for each session.
package exponential

import (
	"sync/atomic"
	"time"

	"github.com/tandem97/wrapper/effector"
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

// Backoff is a concurrent-safe exponential backoff generator. The
// sequence position is advanced atomically, so Backoff needs no lock.
//
// A Backoff must be built with New. The zero value is not usable: it has
// no cap, so Backoff returns 0 on every call, which turns a retry loop
// into a busy loop rather than reporting the mistake.
type Backoff struct {
	base time.Duration
	cap  time.Duration
	cur  atomic.Int64
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

	backoff.cur.Store(int64(backoff.base))

	return backoff
}

// Backoff returns the next delay in the sequence and advances the
// generator. With the default options the sequence is 1s, 2s, 4s, 8s,
// 16s, followed by cap (30s) forever.
//
// Backoff is safe for concurrent use: the sequence position is advanced
// with a compare-and-swap, so concurrent callers each receive a
// distinct step of the sequence. Sharing one generator between
// concurrent retry sessions is not meaningful regardless, as each
// session advances the position and Reset rewinds it for all of them;
// use Clone for per-session isolation.
func (b *Backoff) Backoff() time.Duration {
	cap := int64(b.cap)

	for {
		cur := b.cur.Load()

		if cur >= cap {
			return b.cap
		}

		next := cur << 1

		// If the doubling overflows int64 (next < cur for positive
		// values) or overshoots cap, the sequence has saturated: pin the
		// state at cap and still return the last representable value
		// instead of cap. The condition is evaluated before the
		// assignment, so it still sees the doubled value.
		if next < cur || next >= cap {
			next = cap
		}

		if b.cur.CompareAndSwap(cur, next) {
			return time.Duration(cur)
		}
	}
}

// Reset restarts the sequence so that the next call to Backoff returns
// base again.
func (b *Backoff) Reset() { b.cur.Store(int64(b.base)) }

// Clone returns an independent copy of the generator with the same
// configuration, positioned at the start of a fresh sequence. The
// original is left untouched.
func (b *Backoff) Clone() effector.Backoff {
	return New(WithBase(b.base), WithCap(b.cap))
}
