package app

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
)

func workRow(t *testing.T, i int) board.WorkRow {
	t.Helper()
	return cardFixture(t).Rows[i]
}

// workTester draws the work section in a 440 wide panel, 600 tall.
func workTester(l *WorkList, clearRows bool, onClear ActionClick) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(theme.Color(theme.BgWindow)).Font(theme.FontFamily).
			FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).
			TextColor(theme.Color(theme.Text)).Children(func() {
			l.View(c, clearRows, onClear)
		})
	}, theme.PanelWidth, 600)
}

func workList(t *testing.T, rows []board.WorkRow) *WorkList {
	l := NewWorkList(nil)
	l.Sync(rows)
	return l
}

func findRect(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	tt.Frame()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("missing %q in %v", label, tt.Texts())
	}
	return r
}

func TestTheAccessibleLabelReadsTheStagesInOrder(t *testing.T) {
	want := "#640 sort bags, plan done, build working, review not started, CI not started, you not started, b-1151 5m"
	if got := WorkRowLabel(workRow(t, 0)); got != want {
		t.Errorf("got %q", got)
	}
}

func TestARowWithoutWhoHasNoTrailingComma(t *testing.T) {
	row := workRow(t, 0)
	row.Who.Text = ""
	if got := WorkRowLabel(row); got[len(got)-len("you not started"):] != "you not started" {
		t.Errorf("got %q", got)
	}
}

func TestAJoinedRowReadsItsJoin(t *testing.T) {
	if got := WorkRowLabel(workRow(t, 2)); got != "#642 joined #641" {
		t.Errorf("got %q", got)
	}
}

func TestAFinishedTitleIsDimAndALiveOneIsNot(t *testing.T) {
	if TitleColour(workRow(t, 2)) != theme.TextDim || TitleColour(workRow(t, 0)) != theme.Text {
		t.Error("wrong title colours")
	}
}

func TestWhoColourFollowsTheKind(t *testing.T) {
	if WhoColour(board.WhoDim) != theme.TextDim2 || WhoColour(board.WhoText) != theme.Text2 ||
		WhoColour(board.WhoOther) != theme.Text2 {
		t.Error("wrong who colours")
	}
}

func TestTheFiveStageHeadsFillTheRight220Px(t *testing.T) {
	tt := workTester(workList(t, cardFixture(t).Rows), true, nil)
	tt.Frame()
	right := float32(theme.PanelWidth - theme.PanelPadX)
	for i, head := range board.StageHeads {
		r, ok := tt.Find(head)
		if !ok {
			t.Fatalf("missing head %q", head)
		}
		col := right - theme.StageColWidth*float32(5-i)
		if r.X < col || r.X+r.W > col+theme.StageColWidth {
			t.Errorf("%s at %v is not in its column starting at %v", head, r, col)
		}
	}
}

func TestEachWorkRowIs42PxTall(t *testing.T) {
	rows := cardFixture(t).Rows
	tt := workTester(workList(t, rows), true, nil)
	for _, row := range rows {
		if r := findRect(t, tt, WorkRowLabel(row)); r.H != theme.WorkRowHeight || r.W != theme.PanelWidth {
			t.Errorf("%s is %vx%v", row.Label, r.W, r.H)
		}
	}
}

func TestAWorkingRowShowsDoneAndWorkingInTheirColumns(t *testing.T) {
	tt := workTester(workList(t, cardFixture(t).Rows[:1]), true, nil)
	tt.Frame()
	right := float32(theme.PanelWidth - theme.PanelPadX)
	inColumn := func(icon string, column int) {
		t.Helper()
		r, ok := tt.Find(icon)
		if !ok {
			t.Fatalf("missing icon %q", icon)
		}
		left := right - theme.StageColWidth*float32(5-column)
		if r.X < left || r.X >= left+theme.StageColWidth {
			t.Errorf("%s at %v is not in column %d", icon, r, column)
		}
	}
	inColumn("done", 0)
	inColumn("working", 1)
}

func TestTheYouRowShowsItsChip(t *testing.T) {
	tt := workTester(workList(t, cardFixture(t).Rows), true, nil)
	r := findRect(t, tt, "your test")
	row := findRect(t, tt, WorkRowLabel(workRow(t, 1)))
	if r.Y < row.Y || r.Y+r.H > row.Y+row.H {
		t.Errorf("the chip %v is outside its row %v", r, row)
	}
	if _, ok := tt.Find("b-1151 5m"); !ok {
		t.Error("missing the plain who text")
	}
}

func TestAJoinedRowHasNoStageIcons(t *testing.T) {
	tt := workTester(workList(t, cardFixture(t).Rows[2:]), true, nil)
	tt.Frame()
	if _, ok := tt.Find("joined #641"); !ok {
		t.Error("missing the join")
	}
	if _, ok := tt.Find("not started"); ok {
		t.Error("a joined row has stage icons")
	}
}

func TestClearDoneShowsOnlyWhenRowsCanBeCleared(t *testing.T) {
	l := workList(t, cardFixture(t).Rows)
	findRect(t, workTester(l, true, nil), LabelClearWork)
	tt := workTester(l, false, nil)
	tt.Frame()
	if _, ok := tt.Find(LabelClearWork); ok {
		t.Error("clear done shows with nothing to clear")
	}
}

func TestClearDoneIsInertWithoutAClickAndSendsWithOne(t *testing.T) {
	l := workList(t, cardFixture(t).Rows)
	tt := workTester(l, true, nil)
	findRect(t, tt, LabelClearWork)
	if err := tt.Click(LabelClearWork); err != nil {
		t.Logf("inert control: %v", err)
	}
	n := 0
	tt = workTester(l, true, func() { n++ })
	findRect(t, tt, LabelClearWork)
	if err := tt.Click(LabelClearWork); err != nil || n != 1 {
		t.Errorf("click: err %v, calls %d", err, n)
	}
}

func TestRowsKeepTheCoordinatorsOrder(t *testing.T) {
	a, b := workRow(t, 0), workRow(t, 1)
	a.Key, a.Label, b.Key, b.Label = "b", "#2", "a", "#1"
	l := workList(t, []board.WorkRow{a, b})
	tt := workTester(l, true, nil)
	first := findRect(t, tt, WorkRowLabel(a))
	second := findRect(t, tt, WorkRowLabel(b))
	if first.Y >= second.Y {
		t.Error("row 0 is not above row 1")
	}
	if got := []string{l.Rows()[0].Key, l.Rows()[1].Key}; !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("keys %v", got)
	}
}

func TestAChangedStageRedrawsOnlyItsRow(t *testing.T) {
	l := workList(t, cardFixture(t).Rows)
	next := cardFixture(t).Rows
	next[1].Stages[4] = board.StageDone
	got := l.Sync(next)
	if got.Rebuilt || !reflect.DeepEqual(got.Rows, []int{1}) || !got.Any() {
		t.Errorf("change %+v", got)
	}
}

func TestANewPickRedrawsOnlyTheRowsThatChange(t *testing.T) {
	l := workList(t, cardFixture(t).Rows)
	if !l.SetPicked("owner/name#640") {
		t.Error("a new pick is not a change")
	}
	if l.SetPicked("owner/name#640") {
		t.Error("the same pick is a change")
	}
	if got := l.Sync(cardFixture(t).Rows); got.Any() {
		t.Errorf("a pick changes no row data: %+v", got)
	}
}

func TestTheSameSnapshotAgainRedrawsNoRow(t *testing.T) {
	l := workList(t, cardFixture(t).Rows)
	if got := l.Sync(cardFixture(t).Rows); got.Any() {
		t.Errorf("change %+v", got)
	}
}

func TestNewKeysRebuildTheRows(t *testing.T) {
	l := workList(t, cardFixture(t).Rows)
	if got := l.Sync(cardFixture(t).Rows[:1]); !got.Rebuilt {
		t.Errorf("change %+v", got)
	}
	other := cardFixture(t).Rows[:1]
	other[0].Key = "x"
	if got := l.Sync(other); !got.Rebuilt {
		t.Errorf("change %+v", got)
	}
}

func TestNoRowsShowsTheEmptyLine(t *testing.T) {
	tt := workTester(workList(t, nil), true, nil)
	findRect(t, tt, LabelWorkEmpty)
	if _, ok := tt.Find(LabelWorkRows); ok {
		t.Error("the rows list shows with no rows")
	}
}

func TestAClickOnARowPicksItAndItsCardFillsTheRow(t *testing.T) {
	var picks []string
	l := NewWorkList(func(k string) { picks = append(picks, k) })
	l.Sync(cardFixture(t).Rows)
	tt := workTester(l, true, nil)
	label := WorkRowLabel(workRow(t, 0))
	before := findRect(t, tt, label)
	if err := tt.Click(label); err != nil || !reflect.DeepEqual(picks, []string{"owner/name#640"}) {
		t.Fatalf("click: err %v, picks %v", err, picks)
	}
	l.SetPicked("owner/name#640")
	tt.Frame()
	if after := findRect(t, tt, label); after != before {
		t.Errorf("the picked row moved: %v to %v", before, after)
	}
	if err := tt.Click(label); err != nil || len(picks) != 2 {
		t.Errorf("a second click keeps it: err %v, picks %v", err, picks)
	}
}

func TestALongListScrollsInsideHalfThePanel(t *testing.T) {
	base := workRow(t, 0)
	rows := make([]board.WorkRow, 20)
	for i := range rows {
		rows[i] = base
		rows[i].Key = "k" + string(rune('a'+i))
		rows[i].Label = "#" + string(rune('a'+i))
	}
	tt := workTester(workList(t, rows), true, nil)
	section := findRect(t, tt, LabelWorkSection)
	list := findRect(t, tt, LabelWorkRows)
	if section.H > 300 {
		t.Errorf("the section is %v tall in a 600 panel", section.H)
	}
	if list.Y+list.H != section.Y+section.H {
		t.Errorf("the rows %v do not fill the section %v", list, section)
	}
}
