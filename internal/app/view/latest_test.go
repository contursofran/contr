package view_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/view"
)

func TestLatestDropsTheOldResultAndAppliesTheNewest(t *testing.T) {
	var h apptest.HeldEnv
	var l view.Latest
	var got []string
	l.Do(h.Env(), func() func() { return func() { got = append(got, "old") } })
	l.Reset()
	l.Do(h.Env(), func() func() { return func() { got = append(got, "new") } })
	h.RunAll()
	if want := []string{"new"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLatestAppliesAllReadsOfOneGeneration(t *testing.T) {
	var h apptest.HeldEnv
	var l view.Latest
	var got []string
	l.Do(h.Env(), func() func() { return func() { got = append(got, "a") } })
	l.Do(h.Env(), func() func() { return func() { got = append(got, "b") } })
	if !l.Busy() {
		t.Fatal("not busy with two reads in flight")
	}
	h.RunAll()
	if want := []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if l.Busy() {
		t.Fatal("busy after the results")
	}
}

func TestLatestDropsAllResultsAfterAReset(t *testing.T) {
	var h apptest.HeldEnv
	var l view.Latest
	ran := 0
	l.Do(h.Env(), func() func() { return func() { ran++ } })
	l.Do(h.Env(), func() func() { return func() { ran++ } })
	l.Reset()
	if l.Busy() {
		t.Fatal("busy after a reset")
	}
	h.RunAll()
	if ran != 0 {
		t.Fatalf("%d results applied after a reset", ran)
	}
}

func TestLatestResetEndsTheContext(t *testing.T) {
	var h apptest.HeldEnv
	var l view.Latest
	var before, after error
	l.Read(h.Env(), time.Hour, func(c context.Context) func() {
		before = c.Err()
		l.Reset() // a newer read starts while this one runs
		after = c.Err()
		return func() {}
	})
	h.RunAll()
	if before != nil {
		t.Fatalf("context ended before a reset: %v", before)
	}
	if !errors.Is(after, context.Canceled) {
		t.Fatalf("context after a reset: %v, want canceled", after)
	}
}

func TestLatestReadEndsAtTheTimeLimit(t *testing.T) {
	var l view.Latest
	var err error
	l.Read(view.Env{}, time.Millisecond, func(c context.Context) func() {
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
	var h apptest.HeldEnv
	var l view.Latest
	var ctxs []context.Context
	read := func(c context.Context) func() { ctxs = append(ctxs, c); return func() {} }
	l.Read(h.Env(), 0, read)
	l.Read(h.Env(), 0, read)
	h.Work[0]()
	h.Work[1]()
	h.Done[0]()
	if ctxs[0].Err() != nil {
		t.Fatal("context ended while a read of its generation is in flight")
	}
	h.Done[1]()
	if ctxs[1].Err() == nil {
		t.Fatal("context still on after the last result")
	}
}

func TestLatestBindRunsOnlyInItsGeneration(t *testing.T) {
	var l view.Latest
	ran := 0
	fn := l.Bind(func() { ran++ })
	fn()
	l.Reset()
	fn()
	if ran != 1 {
		t.Fatalf("ran %d times, want 1", ran)
	}
}
