package diagram

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

const branch = "issue -> plan: owner picks\nplan -> build\nbuild -> review: PR\n" +
	"review -> merge: approved\nreview -> build: blockers\nmerge -> release\nmerge [done]\nreview [waiting]\n"

func TestParse(t *testing.T) {
	f, err := Parse(branch)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Nodes) != 6 || len(f.Edges) != 6 {
		t.Fatalf("nodes %d, edges %d", len(f.Nodes), len(f.Edges))
	}
	if e := f.Edges[0]; f.Nodes[e.From].Label != "issue" || f.Nodes[e.To].Label != "plan" || e.Label != "owner picks" {
		t.Errorf("first edge %+v", e)
	}
	if f.Edges[1].Label != "" {
		t.Errorf("an edge without a label has %q", f.Edges[1].Label)
	}
	if f.Nodes[4].Label != "merge" || f.Nodes[4].State != "done" || f.Nodes[3].State != "waiting" || f.Nodes[0].State != "" {
		t.Errorf("states %+v", f.Nodes)
	}
	if f, err := Parse("a -> b: x: y"); err != nil || f.Edges[0].Label != "x: y" {
		t.Errorf("a label with a colon: %+v %v", f, err)
	}
	if f, err := Parse("a [running]\n\n  a -> b  \n"); err != nil || f.Nodes[0].State != "running" || len(f.Nodes) != 2 {
		t.Errorf("a state before the arrow, blank lines: %+v %v", f, err)
	}
}

func TestParseErrors(t *testing.T) {
	for src, want := range map[string]error{
		"a -> b -> c": ErrChain,
		"":            ErrEmpty,
		"a [done]\n":  ErrEmpty,
	} {
		if _, err := Parse(src); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", src, err, want)
		}
	}
	for _, src := range []string{"just words", "a -> b\nno arrow", "a [busy]"} {
		if _, err := Parse(src); err == nil || !strings.Contains(err.Error(), "line") {
			t.Errorf("%q: %v, want a line error", src, err)
		}
	}
}

func TestLimits(t *testing.T) {
	var b strings.Builder
	for i := range MaxNodes - 1 {
		b.WriteString(string(rune('a'+i)) + " -> " + string(rune('a'+i+1)) + "\n")
	}
	twelve, _ := Parse(b.String())
	if ok, _ := twelve.Fits(); !ok {
		t.Error("12 nodes do not fit")
	}
	if ok, _ := Layout(twelve).Fits(); ok {
		t.Error("12 layers fit")
	}
	thirteen, _ := Parse(b.String() + "l -> m\n")
	if ok, _ := thirteen.Fits(); ok {
		t.Error("13 nodes fit")
	}
	long, _ := Parse("a -> " + strings.Repeat("x", MaxLabel+1))
	if ok, _ := long.Fits(); ok {
		t.Error("a long label fits")
	}
	fan, _ := Parse("a -> b\na -> c\na -> d\na -> e\na -> f\n")
	if ok, _ := Layout(fan).Fits(); ok {
		t.Error("5 nodes in a layer fit")
	}
	l := Layout(mustParse(t, branch))
	back := 0
	for _, x := range l.Back {
		if x {
			back++
		}
	}
	if back != 1 || len(l.Layers) != 6 {
		t.Errorf("back edges %d, layers %d", back, len(l.Layers))
	}
}

func mustParse(t *testing.T, s string) Flow {
	t.Helper()
	f, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// View draws a flow that fits, and builds nothing for one wider than maxW.
func TestView(t *testing.T) {
	f := mustParse(t, branch)
	for _, tc := range []struct {
		maxW float32
		want bool
	}{{2000, true}, {200, false}} {
		var got bool
		tt := ui.NewTester(func(c *ui.Context) { got = View(c, f, tc.maxW, DefaultStyle) }, 1200, 400)
		tt.Frame()
		_, drawn := tt.Access(LabelDiagram)
		if got != tc.want || drawn != tc.want {
			t.Errorf("maxW %v: View %v, element %v; want %v", tc.maxW, got, drawn, tc.want)
		}
	}
}
