package sidebar

import (
	"slices"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
)

func TestStepAgentSkipsProjectRowsAndStopsAtTheEnds(t *testing.T) {
	h := newRows(t)
	h.list.StepAgent(1) // no selection: the first agent row
	if got := selID(h); got != "lead" {
		t.Fatalf("⌘⌥↓ with no selection: %q, want lead", got)
	}
	h.list.StepAgent(-1) // the project row above is not an agent row
	if got := selID(h); got != "lead" {
		t.Fatalf("⌘⌥↑ from the first agent: %q, want lead", got)
	}
	for _, want := range []string{"b-241", "b-242"} {
		h.list.StepAgent(1)
		if got := selID(h); got != want {
			t.Fatalf("⌘⌥↓: %q, want %q", got, want)
		}
	}
	h.list.StepAgent(1) // the project row of beta below is not an agent row
	if sel, _ := h.sel.Current(); sel.Kind == state.KindWorktree {
		t.Fatalf("⌘⌥↓ from b-242 selected the project row %+v", sel)
	}
	h.sel.Clear()
	h.list.StepAgent(-1) // no selection: the last agent row
	h.tt.Frame()
	last := selID(h)
	h.list.StepAgent(1)
	if got := selID(h); got != last || last == "" {
		t.Errorf("⌘⌥↓ from the last agent %q: %q", last, got)
	}
}

func TestTheRowsAboveAndBelowShowTheirChipsWhileCmdIsHeld(t *testing.T) {
	h := newRows(t)
	h.sel.Select(state.Selected{Kind: state.KindWorker, ID: "b-241"})
	h.tt.Frame()
	if slices.ContainsFunc(h.tt.Texts(), func(s string) bool { return strings.HasPrefix(s, kit.LabelKeyHint) }) {
		t.Fatal("chips without Hints")
	}
	h.list.Hints = true
	h.tt.Frame()
	texts := h.tt.Texts()
	for _, key := range []string{"⌥↑", "⌥↓", ","} {
		if !slices.Contains(texts, key) {
			t.Errorf("no chip %q in %q", key, texts)
		}
	}
}

func TestTheIconColumnButtonsShowTheirChips(t *testing.T) {
	h := compactRows(t)
	h.list.Hints = true
	h.tt.Frame()
	for _, key := range []string{"O", "N", ","} {
		if !slices.Contains(h.tt.Texts(), key) {
			t.Errorf("no chip %q in %q", key, h.tt.Texts())
		}
	}
}
