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
	tries      int
	rand       *rand.Rand
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
		rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for _, opt := range opts {
		opt(backoff)
	}

	if backoff.base <= 0 {
		panic("exponentialwithjitter: base must be positive")
	}

	if backoff.cap <= 0 {
		panic("exponentialwithjitter: cap must be positive")
	}

	if backoff.multiplier < 1 {
		panic("exponentialwithjitter: multiplier must be greater than or equal to 1")
	}

	if backoff.jitter < 0 || backoff.jitter > 1 {
		panic("exponentialwithjitter: jitter must be within [0, 1]")
	}

	if backoff.cap < backoff.base {
		panic("exponentialwithjitter: cap must be greater than or equal to base")
	}

	return backoff
}

func (b *Backoff) Backoff() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.tries

	if b.tries != math.MaxInt {
		b.tries++
	}

	backoff, max := float64(b.base), float64(b.cap)
	for backoff < max && r > 0 {
		backoff *= b.multiplier
		r--
	}

	backoff = math.Min(backoff, max) * (1 + b.jitter*(b.rand.Float64()*2-1))

	return time.Duration(backoff)
}

func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.tries = 0
}
