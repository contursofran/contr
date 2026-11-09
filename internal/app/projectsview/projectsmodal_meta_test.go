package projectsview

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/theme"
)

// gapRig has one open row with a path, a live state and an orchestrator dot.
func gapRig(t *testing.T, branch string) *modalRig {
	r := newModalRig(t)
	idle := "idle"
	r.m.Orchestrated = func(g spaces.RepoGroup) bool { return g.Name == "a" }
	r.rows = func(g *spaces.RepoGroup) {
		if g.Name == "a" {
			g.Path, g.Branch = "/p", branch
			g.Rows = []spaces.WorktreeRow{{IsMain: true, Root: "/p", Hold: &idle}}
		}
	}
	r.open()
	return r
}

func (r *modalRig) rect(t *testing.T, s string) ui.Rect {
	t.Helper()
	b, ok := r.tt.Find(s)
	if !ok {
		t.Fatalf("no element %q in %v", s, r.tt.Texts())
	}
	return b
}

func within(a, b float32) bool { return math.Abs(float64(a-b)) < 0.6 }

// The parts of an open row's meta have the same gap: the space token. The
// dot sits the same distance from the live state.
func TestProjectsModalOpenMetaPartsHaveEvenGaps(t *testing.T) {
	want := float32(theme.Space6)
	r := gapRig(t, "")
	path, dot, live := r.rect(t, "/p"), r.rect(t, LabelProjectsDot), r.rect(t, "idle")
	sep := r.rect(t, "·")
	if g := sep.X - (path.X + path.W); !within(g, want) {
		t.Errorf("path to dot: %v, want %v", g, want)
	}
	if g := dot.X - (sep.X + sep.W); !within(g, want) {
		t.Errorf("separator to claude dot: %v, want %v", g, want)
	}
	if g := live.X - (dot.X + dot.W); !within(g, want) {
		t.Errorf("claude dot to live state: %v, want %v", g, want)
	}

	r = gapRig(t, "main")
	path, sep, branch := r.rect(t, "/p"), r.rect(t, "·"), r.rect(t, "main")
	if g := sep.X - (path.X + path.W); !within(g, want) {
		t.Errorf("path to separator: %v, want %v", g, want)
	}
	if g := branch.X - (sep.X + sep.W); !within(g, want) {
		t.Errorf("separator to branch: %v, want %v", g, want)
	}
}
