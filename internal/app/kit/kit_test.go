package kit

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestRowShowsItsPartsAndClicks(t *testing.T) {
	clicks := 0
	long := "plugin/" + strings.Repeat("very-long-folder/", 20) + "pin-panel.ts"
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(240).Children(func() {
			Row(c, RowOpts{Label: "file pin", Mark: "M", Text: long, Counts: true, Added: 5, Removed: 1, Selected: true, OnClick: func() { clicks++ }})
			Row(c, RowOpts{Text: "fix-213", Sub: "3 commits"})
		})
	}, 400, 200)
	for _, want := range []string{"M", "+5", "−1", "fix-213", "3 commits"} {
		if !tt.HasText(want) {
			t.Errorf("no %q; texts %q", want, tt.Texts())
		}
	}
	if _, ok := tt.Find(CountsText(5, 1)); !ok {
		t.Error("the counts have no label")
	}
	if _, ok := tt.Find("fix-213"); !ok {
		t.Error("a row without a label is not found by its text")
	}
	r, ok := tt.Find("file pin")
	if !ok {
		t.Fatal("row not found by label")
	}
	txt, _ := tt.Find("file pin text")
	cnt, _ := tt.Find(CountsText(5, 1))
	if txt.X+txt.W > cnt.X {
		t.Errorf("the text (ends at %v) runs under the counts (start at %v)", txt.X+txt.W, cnt.X)
	}
	if r.W > 240 {
		t.Errorf("a long text widened the row to %v; it must clip in 240", r.W)
	}
	if r.H != RowHeight {
		t.Errorf("one-line row height = %v, want %d", r.H, RowHeight)
	}
	if two, _ := tt.Find("fix-213"); two.H <= RowHeight {
		t.Errorf("a row with Sub is %v high, want two lines", two.H)
	}
	if err := tt.Click("file pin"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 {
		t.Errorf("clicks = %d, want 1", clicks)
	}
}

func TestSwitchChangesTheChoice(t *testing.T) {
	choice := 0
	tt := ui.NewTester(func(c *ui.Context) {
		Switch(c, "mode", &choice, "Whole branch", "By commit")
	}, 400, 60)
	if err := tt.Click("By commit"); err != nil {
		t.Fatal(err)
	}
	if choice != 1 {
		t.Errorf("choice = %d, want 1", choice)
	}
}

// before has 40 lines; after changes line 2 and adds a line at the end, so
// 30 unchanged lines lie between the changes.
var before, after = func() (string, string) {
	var b, a strings.Builder
	for i := 1; i <= 40; i++ {
		line := "line " + strings.Repeat("x", i%7) + "\n"
		b.WriteString(line)
		if i == 2 {
			line = "changed\n"
		}
		a.WriteString(line)
	}
	a.WriteString("added\n")
	return b.String(), a.String()
}()

func TestNewCodeShowsTheWholeFile(t *testing.T) {
	d := NewCode("x.txt", before, after)
	if d.Layout != editor.DiffUnified || d.Indicators != editor.DiffClassic {
		t.Errorf("layout %v, indicators %v", d.Layout, d.Indicators)
	}
	if d.Font.Family != "JetBrains Mono" || d.LineHeight != 1.5 {
		t.Errorf("font %+v, line height %v", d.Font, d.LineHeight)
	}
	if add, del := d.Stats(); add != 2 || del != 1 {
		t.Errorf("stats = +%d -%d, want +2 -1", add, del)
	}
	tt := ui.NewTester(func(c *ui.Context) { CodeView(c, d).Fill() }, 400, 600)
	tt.Frame()
	collapsed := func() bool {
		for _, txt := range tt.Texts() {
			if strings.Contains(txt, "unmodified line") {
				return true
			}
		}
		return false
	}
	// Whole file: no unchanged lines fold away.
	if collapsed() {
		t.Errorf("whole file shows a fold; texts %q", tt.Texts())
	}
	if d.Theme == nil || d.Theme.Added == d.Theme.Deleted {
		t.Error("the code view has no contr theme")
	}
	SetWhole(d, false)
	tt.Frame()
	tt.Frame()
	if !collapsed() {
		t.Errorf("only changes: the lines far from a change do not fold; texts %q", tt.Texts())
	}
	SetWhole(d, true)
	tt.Frame()
	tt.Frame()
	if collapsed() {
		t.Error("whole file again: still folded")
	}
}

func TestCodeViewFollowsLightAndDark(t *testing.T) {
	d := NewCode("x.go", "package x\n", "package y\n")
	tt := ui.NewTester(func(c *ui.Context) { CodeView(c, d).Fill() }, 300, 100)
	tt.SetDark(false)
	tt.Frame()
	light := *d.Theme
	tt.SetDark(true)
	tt.Frame()
	if *d.Theme == light {
		t.Error("the code theme did not change with dark mode")
	}
	if d.Theme.Added.R == 0 && d.Theme.Added.G == 0 {
		t.Error("no added color")
	}
}
