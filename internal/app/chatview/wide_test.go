package chatview

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
)

var (
	longCell = strings.Repeat("a long cell text ", 8)
	wideText = "A short line of prose.\n\n" +
		"| one | two | three | four |\n|---|---|---|---|\n| " + strings.Repeat(longCell+" | ", 4) + "\n\n" +
		"More prose.\n\n```go\nx := 1\n```\n"
)

func TestParts(t *testing.T) {
	got := Parts(wideText)
	if len(got) != 4 || got[0].Wide || !got[1].Wide || got[2].Wide || !got[3].Wide {
		t.Fatalf("parts %+v", got)
	}
	if !strings.HasPrefix(got[1].Text, "| one") || !strings.HasPrefix(got[3].Text, "```go") {
		t.Errorf("cuts %+v", got)
	}
	if p := Parts("just prose"); len(p) != 1 || p[0].Wide {
		t.Errorf("prose only: %+v", p)
	}
	if p := Parts("    indented code\n"); len(p) != 1 || !p[0].Wide || p[0].Text != "    indented code\n" {
		t.Errorf("indented code keeps its line: %+v", p)
	}
}

// wideRig shows one text of Claude in a panel w wide and returns the boxes of the prose, the
// table and the code block.
func wideRig(t *testing.T, w int) (prose, table, code ui.Rect) {
	t.Helper()
	ch := New(view.Env{}, t.TempDir(), nil)
	ch.md.Theme = nil
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Bind(&ch.panel).Padding(0, 20).Children(func() { ch.said(c, "r1", wideText) })
	}, w, 700)
	tt.Frame()
	p, ok := tt.Find("A short line of prose.")
	if !ok {
		t.Fatalf("no prose in %v", tt.Texts())
	}
	tb, ok1 := tt.AccessByRole(ui.RoleTable, "")
	cb, ok2 := tt.AccessByRole(ui.RoleGroup, "Code, go")
	if !ok1 || !ok2 {
		t.Fatalf("table %v, code %v", ok1, ok2)
	}
	return p, tb.Bounds, cb.Bounds
}

func TestWideBlocks(t *testing.T) {
	prose, table, code := wideRig(t, 1600)
	left := float32(1600-920) / 2
	if prose.X < left-1 || prose.X > left+1 {
		t.Errorf("prose starts at %v, want the column's left %v", prose.X, left)
	}
	if table.W <= 920 || table.X >= left {
		t.Errorf("a wide table stays in the column: %+v", table)
	}
	if code.X < left-1 || code.X > left+1 || code.W > 920+1 {
		t.Errorf("a code block that fits leaves the column: %+v", code)
	}
	if table.X < 20-1 || table.X+table.W > 1600-20+1 {
		t.Errorf("a wide table leaves the content width: %+v", table)
	}
	// A panel no wider than the column and its insets: wide blocks keep the column's left edge.
	prose, table, _ = wideRig(t, 920+40)
	if table.X < prose.X-1 || table.X > prose.X+1 {
		t.Errorf("narrow panel: table at %v, prose at %v", table.X, prose.X)
	}
}

// An open work group is as wide as its lines need, up to the content width; short lines keep it
// at the column's left edge.
func TestOpenGroupIsWide(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		wide bool
	}{{"go test ./...", false}, {strings.Repeat("echo a long command line ", 12), true}} {
		r := newRig(t)
		r.tt.SetSize(1600, 900)
		r.savePath(term)
		r.selectTerm(term)
		r.chat()
		r.write(lPrompt, `{"type":"assistant","uuid":"g1","message":{"content":[{"type":"tool_use","id":"g1","name":"Bash","input":{"command":"`+tc.cmd+`"}}]}}`)
		if err := r.tt.Click(LabelGroup + " 1 tool call"); err != nil {
			t.Fatal(err)
		}
		r.tt.Advance(time.Second)
		prompt, _ := r.tt.Find(LabelGroup + " 1 tool call")
		line, ok := r.tt.Find(LabelCall + " Bash")
		if !ok {
			t.Fatalf("no call line in %v", r.tt.Texts())
		}
		left := float32(1600-920) / 2
		if prompt.X < left-1 || prompt.X > left+1 {
			t.Errorf("%q: the fold row is at %v, want %v", tc.cmd, prompt.X, left)
		}
		if gotWide := line.W > 920; gotWide != tc.wide {
			t.Errorf("%q: line %+v, wide %v", tc.cmd, line, gotWide)
		}
		if !tc.wide && (line.X < left || line.X > left+theme.Space12+2) {
			t.Errorf("%q: a short card leaves the column's left edge: %+v", tc.cmd, line)
		}
	}
}
