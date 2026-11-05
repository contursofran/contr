package state

import (
	"slices"
	"sync"
	"testing"
)

func TestToastsStartEmpty(t *testing.T) {
	q := NewToasts(nil)
	if got := q.Take(); len(got) != 0 {
		t.Fatalf("Take = %q, want none", got)
	}
}

func TestToastsKeepTheOrderAndEmptyOnTake(t *testing.T) {
	q := NewToasts(nil)
	q.Show("first")
	q.Show("second")
	if got := q.Pending(); !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("Pending = %q", got)
	}
	if got := q.Take(); !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("Take = %q", got)
	}
	if got := q.Pending(); len(got) != 0 {
		t.Fatalf("Pending after Take = %q, want none", got)
	}
}

func TestToastsIgnoreEmptyText(t *testing.T) {
	n := 0
	q := NewToasts(func() { n++ })
	q.Show("")
	if n != 0 || len(q.Pending()) != 0 {
		t.Fatalf("an empty text queued or redrew (%d redraws)", n)
	}
}

func TestToastsAskForARedrawOnEachShow(t *testing.T) {
	n := 0
	q := NewToasts(func() { n++ })
	q.Show("a")
	q.Show("b")
	if n != 2 {
		t.Fatalf("%d redraws, want 2", n)
	}
}

func TestToastsAreSafeFromManyGoroutines(t *testing.T) {
	q := NewToasts(nil)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); q.Show("x") }()
	}
	wg.Wait()
	if got := q.Take(); len(got) != 20 {
		t.Fatalf("%d messages, want 20", len(got))
	}
}
