package exponential

import (
	"sync"
	"time"
)

const (
	DefaultBase = time.Second
	DefaultCap  = 30 * time.Second
)

type Backoff struct {
	base    time.Duration
	cap     time.Duration
	backoff time.Duration
	mu      sync.Mutex
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

func New(opts ...opt) *Backoff {
	backoff := &Backoff{
		base: DefaultBase,
		cap:  DefaultCap,
	}

	for _, opt := range opts {
		opt(backoff)
	}

	backoff.backoff = backoff.base

	return backoff
}

func (b *Backoff) Backoff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	backoff := b.backoff

	if b.backoff > b.cap {
		b.backoff = b.cap
	}

	b.backoff <<= 1

	return backoff
}

func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.backoff = b.base
}
