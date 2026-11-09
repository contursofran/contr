package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/app/spaces"
)

func TestProjectsModalOpenMetaPartsHaveEvenGaps(t *testing.T) {
	r := newModalRig(t)
	r.m.Orchestrated = func(spaces.RepoGroup) bool { return true }
	r.rows = func(g *spaces.RepoGroup) { g.Path, g.Branch = "/p", "main" }
	r.open()
	texts := r.tt.Texts()
	// Each part is its own text and the gaps are space tokens, so no text
	// holds a space next to the dot.
	for _, s := range texts {
		if strings.HasPrefix(s, " · ") || strings.HasSuffix(s, " ·") {
			t.Errorf("text %q holds its own gap", s)
		}
	}
	if !slices.Contains(texts, "·") || !slices.Contains(texts, "main") {
		t.Errorf("parts missing in %v", texts)
	}
}
