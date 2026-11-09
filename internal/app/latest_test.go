package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// heldEnv holds the work and the results until the test runs them.
type heldEnv struct {
	work, done []func()
}

func (h *heldEnv) env() Env {
	return Env{Run: Runner{
		Go:   func(f func()) { h.work = append(h.work, f) },
		Post: func(f func()) { h.done = append(h.done, f) },
	}}
}

// runAll runs the held work, then the held results, in order.
func (h *heldEnv) runAll() {
	for _, f := range h.work {
		f()
	}
	h.work = nil
	for _, f := range h.done {
		f()
	}
	h.done = nil
}

func TestLatestDropsTheOldResultAndAppliesTheNewest(t *testing.T) {
	var h heldEnv
	var l Latest
	var got []string
	l.Do(h.env(), func() func() { return func() { got = append(got, "old") } })
	l.Reset()
	l.Do(h.env(), func() func() { return func() { got = append(got, "new") } })
	h.runAll()
	if want := []string{"new"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLatestAppliesAllReadsOfOneGeneration(t *testing.T) {
	var h heldEnv
	var l Latest
	var got []string
	l.Do(h.env(), func() func() { return func() { got = append(got, "a") } })
	l.Do(h.env(), func() func() { return func() { got = append(got, "b") } })
	if !l.Busy() {
		t.Fatal("not busy with two reads in flight")
	}
	h.runAll()
	if want := []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if l.Busy() {
		t.Fatal("busy after the results")
	}
}

func TestLatestDropsAllResultsAfterAReset(t *testing.T) {
	var h heldEnv
	var l Latest
	ran := 0
	l.Do(h.env(), func() func() { return func() { ran++ } })
	l.Do(h.env(), func() func() { return func() { ran++ } })
	l.Reset()
	if l.Busy() {
		t.Fatal("busy after a reset")
	}
	h.runAll()
	if ran != 0 {
		t.Fatalf("%d results applied after a reset", ran)
	}
}

func TestLatestResetEndsTheContext(t *testing.T) {
	var h heldEnv
	var l Latest
	var before, after error
	l.Read(h.env(), time.Hour, func(c context.Context) func() {
		before = c.Err()
		l.Reset() // a newer read starts while this one runs
		after = c.Err()
		return func() {}
	})
	h.runAll()
	if before != nil {
		t.Fatalf("context ended before a reset: %v", before)
	}
	if !errors.Is(after, context.Canceled) {
		t.Fatalf("context after a reset: %v, want canceled", after)
	}
}

func TestLatestReadEndsAtTheTimeLimit(t *testing.T) {
	var l Latest
	var err error
	l.Read(Env{}, time.Millisecond, func(c context.Context) func() {
		<-c.Done()
		err = c.Err()
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v, want the time limit", err)
	}
	if l.Busy() {
		t.Fatal("busy after a result with no apply")
	}
}

func TestLatestEndsTheContextAfterTheLastResult(t *testing.T) {
	var h heldEnv
	var l Latest
	var ctxs []context.Context
	read := func(c context.Context) func() { ctxs = append(ctxs, c); return func() {} }
	l.Read(h.env(), 0, read)
	l.Read(h.env(), 0, read)
	h.work[0]()
	h.work[1]()
	h.done[0]()
	if ctxs[0].Err() != nil {
		t.Fatal("context ended while a read of its generation is in flight")
	}
	h.done[1]()
	if ctxs[1].Err() == nil {
		t.Fatal("context still on after the last result")
	}
}

func TestLatestBindRunsOnlyInItsGeneration(t *testing.T) {
	var l Latest
	ran := 0
	fn := l.Bind(func() { ran++ })
	fn()
	l.Reset()
	fn()
	if ran != 1 {
		t.Fatalf("ran %d times, want 1", ran)
	}
}
