# wrapper

[![Go Reference](https://pkg.go.dev/badge/github.com/tandem97/wrapper.svg)](https://pkg.go.dev/github.com/tandem97/wrapper)
[![Go Report Card](https://goreportcard.com/badge/github.com/tandem97/wrapper)](https://goreportcard.com/report/github.com/tandem97/wrapper)

Resilience wrappers for your functions.

`wrapper` is a small, dependency-free Go library (1.20+, generics) that wraps
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
- **Fail-fast sentinel errors** — `ErrServiceUnreachable`, `ErrDebounce`,
  `ErrTooManyCalls`.
- **Context-aware variants** — every wrapper has a `*Context` version that
  passes your `context.Context` through and respects cancellation.
- **Deterministic backoff** — `exponentialjitter.WithSeed` makes jittered
  sequences reproducible.

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
`backoff.Backoff()` between failed attempts. The backoff is reset after every
call, so each retry session starts from a clean state.

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

Context errors (`context.Canceled`, `context.DeadlineExceeded`) returned by the
circuit are passed through to the caller but are **not** counted as failures —
caller cancellations cannot open the breaker.

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
  circuit immediately; calls within `d` return the cached result.
- **`DebounceLast`** (trailing edge): the circuit runs only after a quiet
  period of `d`; calls superseded by newer ones receive `ErrDebounce`.

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
`circuitbreaker`:

```go
type Backoff interface {
	Backoff() time.Duration // delay until the next attempt / probe
	Reset()                 // restart the sequence at its initial value
}
```

**`exponential`** — deterministic `base → ×2 → cap`:

```go
bo := exponential.New(
	exponential.WithBase(100*time.Millisecond),
	exponential.WithCap(10*time.Second),
)

bo.Backoff() // 100ms, 200ms, 400ms, … capped at 10s
bo.Reset()   // back to 100ms
```

**`exponentialjitter`** — configurable multiplier and random spread in
`[1-jitter, 1+jitter)` around the curve, so a fleet of clients does not retry
in sync:

```go
bo := exponentialjitter.New(
	exponentialjitter.WithBase(100*time.Millisecond),
	exponentialjitter.WithCap(10*time.Second),
	exponentialjitter.WithMultiplier(2),
	exponentialjitter.WithJitter(0.2),
	exponentialjitter.WithSeed(42), // optional: deterministic sequence
)
```

Both are concurrent-safe. Invalid configuration panics at construction.

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
| `debounce.ErrDebounce` | `DebounceLast` | The call was superseded by a newer call before it could run |
| `throttle.ErrTooManyCalls` | throttle | The token bucket was empty |

## Guarantees and conventions

- **Invalid configuration panics at construction** (e.g. non-positive durations
  or zero bounds), so mistakes surface at startup, not in production traffic.
- **Context-aware wrappers expect the wrapped function to respect the context**:
  retries, debounce and the breaker rely on the wrapped call observing
  cancellation in a timely manner.
- **A call with an already cancelled context returns `ctx.Err()` immediately**
  without invoking the wrapped function (`retry`, `debounce`, `timeout`,
  `future`).
- **Context errors are not circuit failures**: `circuitbreaker` passes
  `context.Canceled`/`context.DeadlineExceeded` through without counting them.
- **Panics inside a wrapped function crash the process** — wrappers never
  swallow them.
- Wrapped functions may be reused across calls and are safe for concurrent
  use. Wrappers keep their own state internally; the only external piece is
  the backoff you provide — use the bundled generators, which are
  concurrent-safe.

## Testing

Most packages ship with unit tests, and every wrapper has a runnable
`go test` example (`Example*`); `effector` only defines types:

```sh
go test ./...
```

## License

[MIT](LICENSE)