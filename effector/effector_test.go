package effector

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestValueErrorAdaptsToContextForm(t *testing.T) {
	var calls int

	adapted := ValueError[int](func() (int, error) {
		calls++

		return 42, errors.New("boom")
	}).ValueErrorContext()

	res, err := adapted(context.Background())
	if res != 42 || err == nil || calls != 1 {
		t.Fatalf("got (%v, %v) after %d calls, want (42, error) after 1", res, err, calls)
	}
}

func TestVoidAdaptsToContextForm(t *testing.T) {
	var calls int

	adapted := Void(func() { calls++ }).ValueErrorContext()

	res, err := adapted(context.Background())
	if res != (struct{}{}) || err != nil || calls != 1 {
		t.Fatalf("got (%v, %v) after %d calls, want (struct{}{}, nil) after 1", res, err, calls)
	}
}

// plainBackoff implements only Backoff, as a custom generator may.
type plainBackoff struct {
	delay time.Duration
}

func (b *plainBackoff) Backoff() time.Duration { return b.delay }
func (b *plainBackoff) Reset()                 {}

// cloningBackoff additionally implements Cloneable, as the bundled
// generators do.
type cloningBackoff struct {
	delay  time.Duration
	cloned int
}

func (b *cloningBackoff) Backoff() time.Duration { return b.delay }
func (b *cloningBackoff) Reset()                 {}
func (b *cloningBackoff) Clone() Backoff {
	b.cloned++

	return &cloningBackoff{delay: b.delay}
}

func TestBackoffSatisfiedByPlainGenerator(t *testing.T) {
	// A generator without Clone satisfies Backoff and is used as-is.
	var backoff Backoff = &plainBackoff{delay: time.Millisecond}

	if got := backoff.Backoff(); got != time.Millisecond {
		t.Fatalf("got %v, want %v", got, time.Millisecond)
	}

	if _, ok := backoff.(Cloneable); ok {
		t.Fatal("generator without Clone must not satisfy Cloneable")
	}
}

func TestCloneableSatisfiedByClonableGenerator(t *testing.T) {
	generator := &cloningBackoff{delay: time.Millisecond}

	var backoff Backoff = generator

	cloneable, ok := backoff.(Cloneable)
	if !ok {
		t.Fatal("generator with Clone must satisfy Cloneable")
	}

	if got := cloneable.Clone().Backoff(); got != time.Millisecond {
		t.Fatalf("clone returned %v, want the configured %v", got, time.Millisecond)
	}

	if generator.cloned != 1 {
		t.Fatalf("Clone called %d times, want 1", generator.cloned)
	}
}
