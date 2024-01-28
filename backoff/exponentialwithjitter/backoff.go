package exponentialwithjitter

import (
	"math/rand"
	"sync"
	"time"
)

const (
	DefaultBase             = time.Second
	DefaultCap              = 30 * time.Second
	DefaultJitterMultiplier = 2
)

type Backoff struct {
	base             time.Duration
	cap              time.Duration
	jitterMultiplier int64
	backoff          time.Duration
	mu               sync.Mutex
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

func WithJitterMultiplier(multiplier int64) opt {
	return func(b *Backoff) {
		b.jitterMultiplier = multiplier
	}
}

func New(opts ...opt) *Backoff {
	backoff := &Backoff{
		base:             DefaultBase,
		cap:              DefaultCap,
		jitterMultiplier: DefaultJitterMultiplier,
	}

	for _, opt := range opts {
		opt(backoff)
	}

	backoff.backoff = backoff.base

	if backoff.jitterMultiplier == 0 {
		backoff.jitterMultiplier = 1
	}

	return backoff
}

func (b *Backoff) Backoff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.backoff > b.cap {
		b.backoff = b.cap
	}

	jitter := rand.Int63n(int64(b.backoff) * b.jitterMultiplier)
	b.backoff <<= 1

	return b.base + time.Duration(jitter)
}

func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.backoff = b.base
}
