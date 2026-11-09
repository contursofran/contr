package view

import (
	"slices"
	"testing"
)

func TestEnvZeroRunsAtOnce(t *testing.T) {
	var got []string
	var e Env
	e.Do(func() func() {
		got = append(got, "work")
		return func() { got = append(got, "done") }
	})
	e.Do(func() func() { got = append(got, "nil done"); return nil })
	e.Post(func() { got = append(got, "post") })
	e.Redraw()
	e.Show("message")
	e.Open("https://example.com")
	if want := []string{"work", "done", "nil done", "post"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEnvDoUsesRun(t *testing.T) {
	var gone, posted []func()
	e := Env{Run: Runner{
		Go:   func(f func()) { gone = append(gone, f) },
		Post: func(f func()) { posted = append(posted, f) },
	}}
	ran := 0
	e.Do(func() func() { return func() { ran++ } })
	if len(gone) != 1 || ran != 0 {
		t.Fatalf("Do ran the work on the caller: gone %d, ran %d", len(gone), ran)
	}
	gone[0]()
	if len(posted) != 1 || ran != 0 {
		t.Fatalf("the result did not go through Post: posted %d, ran %d", len(posted), ran)
	}
	posted[0]()
	e.Post(func() { ran++ })
	if len(posted) != 2 || ran != 1 {
		t.Fatalf("Post did not go through Run.Post: posted %d, ran %d", len(posted), ran)
	}
}

func TestEnvCallsItsFunctions(t *testing.T) {
	var redraws int
	var shown, opened []string
	e := Env{
		Invalidate: func() { redraws++ },
		Status:     func(s string) { shown = append(shown, s) },
		OpenURL:    func(u string) { opened = append(opened, u) },
	}
	e.Redraw()
	e.Show("saved")
	e.Open("https://example.com")
	if redraws != 1 || !slices.Equal(shown, []string{"saved"}) || !slices.Equal(opened, []string{"https://example.com"}) {
		t.Fatalf("redraws %d, shown %v, opened %v", redraws, shown, opened)
	}
}
