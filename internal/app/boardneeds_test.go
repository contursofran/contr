package app

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
)

type needsHost struct {
	items      []board.NeedItem
	count      uint32
	clearItems bool
	picked     string
	onPick     func(string)
	onClear    ActionClick
}

func (h *needsHost) tester() *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(theme.Color(theme.BgWindow)).Font(theme.FontFamily).
			FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).
			TextColor(theme.Color(theme.Text)).Children(func() {
			NeedsSection(c, h.items, h.count, h.clearItems, h.picked, h.onPick, h.onClear)
		})
	}, theme.PanelWidth, 900)
}

func needItem(t *testing.T, json string) board.NeedItem {
	t.Helper()
	return cardItems(t, "["+json+"]")[0]
}

func TestOpenAsksAndToDosReadInTheTextColour(t *testing.T) {
	cases := []struct {
		json string
		want uint32
	}{
		{`{"key":"D7","kind":"ask"}`, theme.Text},
		{`{"key":"T12","kind":"do"}`, theme.Text},
		{`{"key":"N4","kind":"know"}`, theme.TextDim},
		{`{"key":"N5","kind":"news"}`, theme.TextDim},
		{`{"key":"D6","kind":"ask","open":false}`, theme.TextDim},
	}
	for _, c := range cases {
		if got := NeedText(needItem(t, c.json)); got != c.want {
			t.Errorf("%s: got %#x", c.json, got)
		}
	}
}

func TestTheAccessibleNameIsKeyTitleAndKind(t *testing.T) {
	if got := NeedLabel(needItem(t, `{"key":"D7","kind":"ask","title":"Row layout"}`)); got != "D7 Row layout, decide" {
		t.Errorf("got %q", got)
	}
	if got := NeedLabel(needItem(t, `{"key":"D6","kind":"ask","title":"Colours","open":false}`)); got != "D6 Colours, done" {
		t.Errorf("got %q", got)
	}
}

func TestTheFixtureRowsAre28TallAndHoldTheirIcons(t *testing.T) {
	h := &needsHost{items: cardFixture(t).Items, count: 3, clearItems: true}
	tt := h.tester()
	findRect(t, tt, NeedsCountLabel(3))
	for i, item := range h.items {
		row := findRect(t, tt, NeedLabel(item))
		if row.H != theme.BoardLineHeight {
			t.Errorf("row %d is %v tall", i, row.H)
		}
	}
	for _, c := range []struct{ item, icon string }{
		{"T12", "do"}, {"D7", "decide"}, {"N4", "fyi"}, {"D6", "done"},
	} {
		var item board.NeedItem
		for _, it := range h.items {
			if it.Key == c.item {
				item = it
			}
		}
		row := findRect(t, tt, NeedLabel(item))
		icon, ok := tt.Find(c.icon)
		if !ok {
			t.Errorf("missing icon %q", c.icon)
			continue
		}
		if icon.Y < row.Y || icon.Y+icon.H > row.Y+row.H {
			t.Errorf("%s %v is not in %v", c.icon, icon, row)
		}
	}
}

func TestClearDoneShowsWithClearItems(t *testing.T) {
	h := &needsHost{items: cardFixture(t).Items, count: 3, clearItems: true}
	findRect(t, h.tester(), LabelClearNeed)
}

func TestClearDoneIsHiddenWithoutResolvedItems(t *testing.T) {
	h := &needsHost{items: cardFixture(t).Items, count: 3}
	tt := h.tester()
	tt.Frame()
	if _, ok := tt.Find(LabelClearNeed); ok {
		t.Error("clear done shows")
	}
}

func TestClearDoneSendsWhenLit(t *testing.T) {
	n := 0
	h := &needsHost{items: cardFixture(t).Items, count: 3, clearItems: true, onClear: func() { n++ }}
	tt := h.tester()
	tt.Frame()
	if err := tt.Click(LabelClearNeed); err != nil || n != 1 {
		t.Errorf("err %v, calls %d", err, n)
	}
}

func TestItemsKeepTheCoordinatorsOrder(t *testing.T) {
	items := cardItems(t, `[{"key":"T3","kind":"do","title":"Test"},{"key":"D1","kind":"ask","title":"Pick"}]`)
	h := &needsHost{items: items, count: 2}
	tt := h.tester()
	first := findRect(t, tt, NeedLabel(items[0]))
	second := findRect(t, tt, NeedLabel(items[1]))
	if first.Y >= second.Y {
		t.Error("row 0 is not above row 1")
	}
}

func TestNoItemsShowsTheEmptyLine(t *testing.T) {
	h := &needsHost{}
	tt := h.tester()
	findRect(t, tt, LabelNeedsEmpty)
	findRect(t, tt, NeedsCountLabel(0))
}

func TestAClickOnARowPicksItsKey(t *testing.T) {
	var picks []string
	h := &needsHost{items: cardFixture(t).Items, count: 3, onPick: func(k string) { picks = append(picks, k) }}
	tt := h.tester()
	findRect(t, tt, LabelNeedsRows)
	if err := tt.Click(NeedLabel(h.items[2])); err != nil || !reflect.DeepEqual(picks, []string{"N4"}) {
		t.Errorf("err %v, picks %v", err, picks)
	}
}

func TestTheSectionEndsAtTheBottomAndTheRowsClip(t *testing.T) {
	items := make([]board.NeedItem, 40)
	for i := range items {
		items[i] = needItem(t, `{"key":"D`+strconv.Itoa(i)+`","kind":"ask","title":"Item `+strconv.Itoa(i)+`"}`)
	}
	h := &needsHost{items: items, count: 40}
	tt := h.tester()
	section := findRect(t, tt, LabelNeedsSection)
	rows := findRect(t, tt, LabelNeedsRows)
	if section.Y+section.H != 900 || rows.Y+rows.H != 900 {
		t.Errorf("section %v, rows %v: not at the bottom", section, rows)
	}
}

func TestAPickedItemKeepsItsRowSize(t *testing.T) {
	h := &needsHost{items: cardFixture(t).Items, count: 3, picked: "D7"}
	tt := h.tester()
	row := findRect(t, tt, NeedLabel(h.items[1]))
	if row.H != theme.BoardLineHeight {
		t.Errorf("picked row is %v tall", row.H)
	}
}

func TestOnlyTheResolvedRowHasTheResolvedMarker(t *testing.T) {
	h := &needsHost{items: cardFixture(t).Items, count: 3}
	tt := h.tester()
	img := tt.Image()
	for _, item := range h.items {
		title := findRect(t, tt, item.Title)
		run := strikeRun(img, title)
		// A struck title has a line across its whole text, longer than
		// any run in plain letters. The 5 DIPs per letter hold for the
		// pinned font at scale 1.
		if struck := run >= 5*len(item.Title); struck != !item.Open {
			t.Errorf("%s: open %v, longest run %d of %v", item.Key, item.Open, run, title.W)
		}
	}
}

func TestThePickedItemDrawsAsTheSelectedCard(t *testing.T) {
	h := &needsHost{items: cardFixture(t).Items, count: 3, picked: "D7"}
	tt := h.tester()
	for _, item := range h.items {
		want := uint32(theme.BgWindow)
		if item.Key == "D7" {
			want = theme.BgCard
		}
		if got := rowFill(t, tt, NeedLabel(item)); got != want {
			t.Errorf("%s: fill %#x, want %#x", item.Key, got, want)
		}
	}
}
