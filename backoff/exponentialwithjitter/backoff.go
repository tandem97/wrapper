package exponentialwithjitter

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

const (
	DefaultBase       = 1 * time.Second
	DefaultCap        = 30 * time.Second
	DefaultMultiplier = 1.6
	DefaultJitter     = 0.2
)

type Backoff struct {
	base       time.Duration
	cap        time.Duration
	jitter     float64
	multiplier float64
	retries    int
	mu         sync.Mutex
}

type opt func(b *Backoff)

func WithBase(base time.Duration) opt {
	return func(b *Backoff) {
		b.base = base
	}
}

func WithCap(cap time.Duration) opt {
	return func(b *Backoff) {
		b.cap = cap
	}
}

func WithMultiplier(multiplier float64) opt {
	return func(b *Backoff) {
		b.multiplier = multiplier
	}
}

func WithJitter(jitter float64) opt {
	return func(b *Backoff) {
		b.jitter = jitter
	}
}

func New(opts ...opt) *Backoff {
	backoff := &Backoff{
		base:       DefaultBase,
		cap:        DefaultCap,
		multiplier: DefaultMultiplier,
		jitter:     DefaultJitter,
	}

	for _, opt := range opts {
		opt(backoff)
	}

	return backoff
}

func (b *Backoff) Backoff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.retries

	if b.retries != math.MaxInt {
		b.retries++
	}

	if r == 0 {
		return b.base
	}

	backoff, max := float64(b.base), float64(b.cap)
	for backoff < max && r > 0 {
		backoff *= b.multiplier
		r--
	}

	if backoff > max {
		backoff = max
	}

	backoff *= 1 + b.jitter*(rand.Float64()*2-1)
	if backoff < 0 {
		return 0
	}

	return time.Duration(backoff)
}

func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.retries = 0
}
