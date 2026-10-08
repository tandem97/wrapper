# wrapper

[![Go Reference](https://pkg.go.dev/badge/github.com/tandem97/wrapper.svg)](https://pkg.go.dev/github.com/tandem97/wrapper)

Resilience wrappers for your functions.

`wrapper` is a small, dependency-free Go library (1.22+, generics) that wraps
any function with retries, circuit breaking, debounce, throttling, timeouts and
more — without ever making you rewrite that function.

Every wrapper takes a function and returns a function of the **same shape**, so
wrappers compose freely: stack them in any order and the call chain still looks
like one plain call. The one exception is the entry point: `timeout.Timeout`
lifts a context-free function into a context-aware one.

```go
res, err := myFunc()              // before
myFunc = wrapper.X(myFunc)        // after
res, err = myFunc()               // same call, resilient now
```

## Features

- **Zero dependencies** — only the Go standard library.
- **Generics** — works with any `(T, error)` function signature.
- **Composable** — wrappers have identical signatures and stack in any order.
- **Concurrent-safe** — every wrapper keeps its own state internally and may be
  reused across goroutines.
- **Per-session backoff** — retry clones the generator on every call, so
  concurrent retry sessions escalate independently instead of sharing one
  sequence position.
- **Fail-fast sentinel errors** — `ErrServiceUnreachable`, `ErrDebounce`,
  `ErrTooManyCalls`.
- **Context-aware variants** — every wrapper has a `*Context` version that
  passes your `context.Context` through and respects cancellation.
- **Deterministic backoff** — `exponentialjitter.WithSeed` makes jittered
  sequences reproducible; `Clone` reproduces a session on demand.

## Installation

```sh
go get github.com/tandem97/wrapper
```

## Quick start

```go
package main

import (
	"fmt"

	"github.com/tandem97/wrapper/backoff/exponential"
	"github.com/tandem97/wrapper/retry"
)

func main() {
	attempts := 0

	fetch := func() (string, error) {
		attempts++

		if attempts < 3 {
			return "", fmt.Errorf("transient failure")
		}

		return "ok", nil
	}

	// Wrap fetch with retries: up to 5 attempts, exponential backoff.
	fetch = retry.Retry(fetch, 5, exponential.New())

	res, err := fetch()
	fmt.Println(res, err) // ok <nil>
	fmt.Println(attempts) // 3
}
```

## Core idea: effector

All wrappers operate on a few tiny types defined in [`effector`](effector):

```go
type ValueErrorContext[T any] func(context.Context) (T, error) // context-aware
type ValueError[T any]        func() (T, error)                // plain
type Void                     func()                           // side-effect only
```

Plain functions adapt to the context-aware form automatically via
`.ValueErrorContext()`, so wrapping and composing is always one line:

```go
backoff := exponential.New(exponential.WithBase(100 * time.Millisecond))
wrapped := retry.RetryContext(circuit.ValueErrorContext(), 4, backoff)
```

## Packages

| Package | What it does |
| --- | --- |
| [`effector`](effector) | The foundation types every wrapper builds on |
| [`backoff/exponential`](backoff/exponential) | Plain exponential backoff generator (1s, 2s, 4s, …) |
| [`backoff/exponentialjitter`](backoff/exponentialjitter) | Exponential backoff with randomized jitter to avoid retry storms |
| [`circuitbreaker`](circuitbreaker) | Fails fast after N consecutive failures, probes recovery with one request |
| [`debounce`](debounce) | Leading- and trailing-edge debounce — coalesce bursts of calls |
| [`future`](future) | Start a slow function in the background, cache its result |
| [`retry`](retry) | Retry a call with a backoff between attempts, stop on context cancel |
| [`throttle`](throttle) | Token-bucket throttling with a fail-fast `ErrTooManyCalls` |
| [`timeout`](timeout) | Bound a context-free function by a deadline |

## Package guide

### retry

Retries a call until it succeeds or the attempts are exhausted, waiting
`backoff.Backoff()` between failed attempts. Once a call has waited at least
once, the backoff is reset before the wrapper returns, so it is reusable. A call
that never waits leaves it untouched.

The generator is cloned lazily, on the first failed attempt — a call that
succeeds immediately costs nothing. Concurrent calls therefore escalate
independently. A backoff without `Clone` is shared instead of copied, so give it
to one retry wrapper at a time.

```go
import (
	"time"

	"github.com/tandem97/wrapper/backoff/exponential"
	"github.com/tandem97/wrapper/retry"
)

backoff := exponential.New(
	exponential.WithBase(100*time.Millisecond),
	exponential.WithCap(2*time.Second),
)

wrapped := retry.Retry(fetch, 4, backoff) // up to 4 attempts
res, err := wrapped()

// Context-aware: interrupts the backoff wait when ctx is done.
wrappedCtx := retry.RetryContext(fetchCtx, 4, backoff)
res, err = wrappedCtx(ctx)
```

### circuitbreaker

Opens after `threshold` consecutive failures: every following call fails fast
with `ErrServiceUnreachable` without invoking the circuit. After the backoff
delay elapses, a single probe request is allowed through (half-open state). A
successful probe resets the breaker; a failed one re-opens it.

A `threshold` of `0` opens the breaker after the first failure. Until something
has failed the breaker is closed and every call passes through, so concurrent
calls are not serialised and never see a spurious `ErrServiceUnreachable`.

Context errors (`context.Canceled`, `context.DeadlineExceeded`) returned by the
circuit are passed through to the caller but are **not** counted as failures —
caller cancellations cannot open the breaker. A cancelled probe re-arms the
open window, since it yielded no information about the circuit.

`threshold` bounds how many consecutive completed failures it takes to open the
breaker — it is not a limit on calls in flight. A burst of concurrent calls
reaches the circuit in full while none of them has completed, so the breaker is
not an admission controller. Two further consequences of that design: a success
returned by a call that started before the breaker opened re-closes it, and a
probe that never returns leaves every call failing fast until it does.

```go
import (
	"github.com/tandem97/wrapper/backoff/exponential"
	"github.com/tandem97/wrapper/circuitbreaker"
)

breaker := circuitbreaker.Breaker(fetch, 5, exponential.New())
res, err := breaker()

if err == circuitbreaker.ErrServiceUnreachable {
	// The circuit is open; fetch was not invoked.
}
```

### debounce

Two flavours of debounce:

- **`DebounceFirst`** (leading edge): the first call in a window executes the
  circuit immediately; calls within `d` return the cached result. There is one
  run per window, under the context of the caller that opened it, so that
  context is the only way to cancel it. Callers that arrive later wait for
  that run and inherit its outcome — including `context.Canceled` if the
  initiating caller was cancelled, even though their own context is live.
- **`DebounceLast`** (trailing edge): the circuit runs only after a quiet
  period of `d`; calls superseded by newer ones receive `ErrDebounce`. A
  circuit that had already started is not superseded — it runs to completion
  and its caller gets the result, so the wrapper can hold the circuit in
  flight for two callers at once. Make the circuit safe for concurrent use.

```go
import (
	"time"

	"github.com/tandem97/wrapper/debounce"
)

// Run immediately, cache the result for 100ms.
debounced := debounce.DebounceFirst(fetch, 100*time.Millisecond)
res, err := debounced()

// Run only after 100ms of quiet.
debounced = debounce.DebounceLast(fetch, 100*time.Millisecond)
res, err = debounced()
```

### throttle

Token-bucket throttling. The bucket starts full with `max` tokens; every call
consumes one token; calls that find the bucket empty fail fast with
`ErrTooManyCalls`. A background goroutine refills the bucket by `refill` tokens
every `d` (up to `max`) and stops when `refillCtx` is cancelled.

Cancelling `refillCtx` does not pause the refill, it ends it: the bucket never
fills again, so once the initial `max` tokens are spent every call returns
`ErrTooManyCalls`. Keep `refillCtx` alive for as long as the throttler is in
use — a `defer stop()` in a setup function will switch the throttler off when
that function returns. A throttler that is never called starts no goroutine.

```go
import (
	"context"
	"time"

	"github.com/tandem97/wrapper/throttle"
)

refillCtx, stop := context.WithCancel(context.Background())
defer stop() // stop the refill goroutine

// Allow bursts of up to 10 calls, refilling 10 tokens per second.
throttled := throttle.Throttle(refillCtx, fetch, 10, 10, time.Second)
res, err := throttled()

if err == throttle.ErrTooManyCalls {
	// The bucket was empty; fetch was not invoked.
}
```

### timeout

Bounds a context-free function by a context deadline or cancellation. The
function runs in a background goroutine and cannot be cancelled: once the
timeout fires, the caller receives the context error while the function keeps
running and its result is discarded.

When the context cannot be cancelled (`ctx.Done() == nil`, e.g.
`context.Background()`), the function is invoked synchronously in the caller's
goroutine — no goroutine overhead.

```go
import (
	"context"
	"time"

	"github.com/tandem97/wrapper/timeout"
)

wrapped := timeout.Timeout(fetch) // lifts a plain function to context-aware

ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

res, err := wrapped(ctx)
```

### future

Starts a slow function immediately in a background goroutine and caches its
result. The first call blocks until the function returns; subsequent calls
return the cached result instantly.

```go
import (
	"github.com/tandem97/wrapper/future"
)

wrapped := future.WrapSlowFunc(fetch) // fetch starts right away
res, err := wrapped()                 // first call blocks, later calls are instant

// Bound the wait with a context: on cancellation ctx.Err() is returned and
// the result is still cached for later callers.
wrappedCtx := future.WrapSlowFuncContext(fetch)
res, err = wrappedCtx(ctx)
```

### backoff

Both generators satisfy the tiny `Backoff` interface consumed by `retry` and
`circuitbreaker`. It is declared once, in [`effector`](effector), and aliased by
both wrappers:

```go
type Backoff interface {
	Backoff() time.Duration // delay until the next attempt / probe
	Reset()                 // restart the sequence at its initial value
}
```

A generator carries the position in its sequence, so a single instance
represents a single sequence and must not be shared between concurrent retry
sessions. Bundled generators also satisfy `effector.Cloneable`:

```go
type Cloneable interface {
	Backoff
	Clone() Backoff // independent copy, starting from the base again
}
```

`retry` clones the generator on every call, so you can pass one instance and
call the wrapper from as many goroutines as you like. To drive a generator
yourself, clone per session.

**`exponential`** — deterministic `base → ×2 → cap`:

```go
bo := exponential.New(
	exponential.WithBase(100*time.Millisecond),
	exponential.WithCap(10*time.Second),
)

bo.Backoff() // 100ms, 200ms, 400ms, … capped at 10s
bo.Reset()   // back to 100ms
```

`cap` is a hard bound: `Backoff()` never returns more than it, and the sequence
reaches `cap` exactly rather than skipping over it. Build generators with `New`:
the zero value is not usable, as both types are exported.

**`exponentialjitter`** — configurable multiplier and random spread in
`[1-jitter, 1+jitter)` around the curve, so a fleet of clients does not retry
in sync:

```go
bo := exponentialjitter.New(
	exponentialjitter.WithBase(100*time.Millisecond),
	exponentialjitter.WithCeiling(10*time.Second),
	exponentialjitter.WithMultiplier(2),
	exponentialjitter.WithJitter(0.2),
	exponentialjitter.WithSeed(42, 1), // optional: deterministic sequence
)
```

Keep jitter small (`0.1`–`0.3`). As it approaches `1` the spread becomes
one-sided and delays degenerate towards `Uniform[0, 2·level)`, so callers retry
almost immediately and the storms jitter is meant to prevent come back.
`jitter` must be within `[0, 1)`.

Two differences from `exponential` are worth keeping in mind:

| | `exponential` | `exponentialjitter` |
| --- | --- | --- |
| Bound | `cap` is a hard maximum, never exceeded | `ceiling` bounds the un-jittered curve; the returned delay may reach `ceiling*(1+jitter)` |
| First delay | always exactly `base` | jittered, so it may be below `base` |
| Unusable zero value | returns `0` forever | panics |

`WithSeed(seed1, seed2)` makes every new generator with the same configuration
produce the same delay sequence, which is what tests need. The two values mirror
`rand.NewPCG`: pass two distinct ones for the full 128 bits of state, so
independently seeded generators do not walk the same sequence. It does not make
`Reset` reproducible: `Reset` rewinds the curve but continues the random stream,
so a reset generator diverges from a newly created one. Use `Clone` to get a
reproducible session. The generator draws from a PCG source, whose output is not
guaranteed across Go releases, so pin the toolchain when relying on exact
delays.

A `multiplier` of exactly `1` gives a flat sequence that never reaches
`ceiling`, so `ceiling` has no effect. Values just above `1` grow slowly: near
the precision limit a multiplier of `1.0000001` adds less than one nanosecond
per step, so the curve emits runs of identical delays before it visibly moves.
The sequence stays monotonic either way.

Both are concurrent-safe. Invalid configuration panics at construction; in
`exponentialjitter` that includes a `ceiling` above `MaxCeiling` (~104 days),
above which the nanosecond level would lose precision.

## Composing wrappers

Because pieces have identical signatures, building a resilient pipeline is just
a sequence of assignments. Here a fragile `fetch` becomes throttled,
circuit-broken, retried and deadline-bounded:

```go
import (
	"context"
	"time"

	"github.com/tandem97/wrapper/backoff/exponential"
	"github.com/tandem97/wrapper/circuitbreaker"
	"github.com/tandem97/wrapper/retry"
	"github.com/tandem97/wrapper/throttle"
	"github.com/tandem97/wrapper/timeout"
)

plain := func() ([]byte, error) { /* ... */ }

// Never wait forever for a slow call; Timeout turns the plain function
// into a context-aware one.
fetch := timeout.Timeout(plain)

// Backoffs for the retry and the breaker are independent instances.
retryBO := exponential.New(exponential.WithBase(100*time.Millisecond), exponential.WithCap(2*time.Second))
breakerBO := exponential.New(exponential.WithBase(1*time.Second), exponential.WithCap(30*time.Second))

// Break the circuit after 5 consecutive failures and probe recovery.
fetch = circuitbreaker.BreakerContext(fetch, 5, breakerBO)

// Retry transient failures up to a total of 4 attempts.
fetch = retry.RetryContext(fetch, 4, retryBO)

// Allow at most 10 calls per second.
refillCtx, stopRefill := context.WithCancel(context.Background())
defer stopRefill()
fetch = throttle.ThrottleContext(refillCtx, fetch, 10, 10, time.Second)

ctx := context.Background()
res, err := fetch(ctx)
```

The wrappers run in reverse assignment order: the throttle gate fires first,
then retry, then the circuit breaker, then the timeout. Each layer only sees
the one below it, so the semantics stay predictable.

## Sentinel errors

| Error | Raised by | Meaning |
| --- | --- | --- |
| `circuitbreaker.ErrServiceUnreachable` | circuit breaker | The circuit is open; the call was not invoked |
| `debounce.ErrDebounce` | `DebounceLast` | The call was superseded by a newer call before its circuit could run |
| `throttle.ErrTooManyCalls` | throttle | The token bucket was empty |

## Guarantees and conventions

- **Invalid configuration panics at construction** (e.g. non-positive durations
  or zero bounds), so mistakes surface at startup, not in production traffic.
- **A backoff generator holds one sequence position.** `retry` resets it once a
  call has waited on it, so it is reusable; a call that never waits leaves it
  alone. `retry` also clones the generator on the first failed attempt, so
  concurrent sessions do not share a position. A generator without `Clone` is
  shared and belongs to one wrapper at a time.
- **`MinBase` bounds the configured base, not the returned delay.** With
  `jitter > 0` a generator built with `base == MinBase` returns delays below
  `MinBase`.
- **Context-aware wrappers expect the wrapped function to respect the context**:
  retries, debounce and the breaker rely on the wrapped call observing
  cancellation in a timely manner.
- **A call with an already cancelled context returns `ctx.Err()` immediately**
  without invoking the wrapped function (`retry`, `debounce`, `timeout`,
  `throttle`) and, for `throttle`, without spending a token.
  `future` is the exception: a result that is already cached is returned even
  then, because there is nothing left to wait for, and the context bounds the
  wait rather than the work.
- **Context errors are not circuit failures**: `circuitbreaker` passes
  `context.Canceled`/`context.DeadlineExceeded` through without counting them.
- **Panics inside a wrapped function crash the process** — wrappers never
  swallow them.
- Wrapped functions may be reused across calls and are safe for concurrent
  use. Wrappers keep their own state internally; the only external piece is
  the backoff you provide — use the bundled generators, which are
  concurrent-safe and clonable.

## Testing

Every package ships with unit tests, and every wrapper has a runnable
`go test` example (`Example*`):

```sh
go test ./...
```

The backoff generators also carry benchmarks, which is where the atomic
sequence in `exponential` shows up: `Backoff` costs ~1.7 ns and ~0.4 ns on eight
cores, against ~26 ns and ~240 ns for a mutex-protected sequence.

## License

[MIT](LICENSE)