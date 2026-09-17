package exponential

import (
	"math"
	"sync"
	"time"
)

const (
	MinBase     = time.Millisecond
	DefaultBase = time.Second
	DefaultCap  = 30 * time.Second
)

type Backoff struct {
	base    time.Duration
	cap     time.Duration
	backoff time.Duration
	mu      sync.Mutex
}

type Opt func(b *Backoff)

func WithBase(base time.Duration) Opt {
	return func(b *Backoff) {
		b.base = base
	}
}

func WithCap(cap time.Duration) Opt {
	return func(b *Backoff) {
		b.cap = cap
	}
}

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

func (b *Backoff) Backoff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.backoff >= b.cap {
		return b.cap
	}

	if b.backoff > math.MaxInt64/2 {
		return b.cap
	}

	backoff := b.backoff

	b.backoff <<= 1

	return backoff
}

func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.backoff = b.base
}
