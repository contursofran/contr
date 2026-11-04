package state

import "testing"

func TestSelectingTheSameValueNotifiesOnce(t *testing.T) {
	n := 0
	s := NewSelection(func() { n++ })
	s.Select(Selected{KindWorktree, "a"})
	s.Select(Selected{KindWorktree, "a"})
	if n != 1 {
		t.Fatalf("notified %d times, want 1", n)
	}
	got, ok := s.Current()
	if !ok || got != (Selected{KindWorktree, "a"}) {
		t.Fatalf("current = %v, %v", got, ok)
	}
}

func TestWorkerReplacesWorktree(t *testing.T) {
	n := 0
	s := NewSelection(func() { n++ })
	s.Select(Selected{KindWorktree, "a"})
	s.Select(Selected{KindWorker, "w1:p1"})
	if n != 2 {
		t.Fatalf("notified %d times, want 2", n)
	}
	got, _ := s.Current()
	if got != (Selected{KindWorker, "w1:p1"}) {
		t.Fatalf("current = %v", got)
	}
}

func TestClearingNotifiesOnlyWhenSomethingWasSelected(t *testing.T) {
	n := 0
	s := NewSelection(func() { n++ })
	s.Clear()
	if n != 0 {
		t.Fatalf("notified %d times, want 0", n)
	}
	s.Select(Selected{KindWorktree, "a"})
	s.Clear()
	s.Clear()
	if n != 2 {
		t.Fatalf("notified %d times, want 2", n)
	}
	if _, ok := s.Current(); ok {
		t.Fatal("want no selection")
	}
}

func TestTerminalKindIsNewAndKeepsTheOthers(t *testing.T) {
	if KindTerminal != KindWorker+1 {
		t.Fatalf("KindTerminal = %d, want after KindWorker (%d)", KindTerminal, KindWorker)
	}
	n := 0
	s := NewSelection(func() { n++ })
	s.Select(Selected{KindWorker, "t1"})
	s.Select(Selected{KindTerminal, "t1"})
	if n != 2 {
		t.Fatalf("a worker and a terminal with one address are one selection (notified %d times, want 2)", n)
	}
}
