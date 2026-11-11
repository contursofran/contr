package questions

import (
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
)

// The outline of the chat (D270): the chat's ticks in place of the pins, in the room of the list.

// chatTicks are four ticks of a chat: two old, a new one and an old one that waits.
func chatTicks() []OutlineTick {
	return []OutlineTick{
		{ID: "p1", Kind: "You", Headline: "start builders for 609", Pinned: outlineClockStart.Add(-time.Hour), Width: 8},
		{ID: "s1/0", Kind: "Decision", Headline: "Ship the notch now or after the release", Pinned: outlineClockStart.Add(-30 * time.Minute), Width: 9, Blocked: true},
		{ID: "s2/0", Kind: "Warning", Headline: "CI is slow on the hc tests", Pinned: outlineClockStart.Add(-20 * time.Minute), Width: 8},
		{ID: "s3/0", Kind: "Done", Headline: "PR 633 is merged", Pinned: outlineClockStart.Add(10 * time.Minute), Width: 8},
	}
}

// newChatOutline is newOutline with the chat of the orchestrator shown: its ticks, the list's
// room top and bottom, and the ids of the jumps.
func newChatOutline(t *testing.T, ticks []OutlineTick, top, bottom float32) (*outlineFixture, *[]string) {
	t.Helper()
	f := newOutline(t, threePins()...)
	var jumps []string
	f.o.Chat = func() (ChatTicks, bool) {
		return ChatTicks{Addr: apptest.OrchAddr, Ticks: ticks, Top: top, Bottom: bottom,
			Jump: func(id string) { jumps = append(jumps, id) }}, true
	}
	return f, &jumps
}

func TestOutlineShowsTheChatTicksInPlaceOfThePins(t *testing.T) {
	f, _ := newChatOutline(t, chatTicks(), 44, 120)
	f.tt.Frame()
	for _, l := range []string{"You: start builders for 609", "Decision: Ship the notch now or after the release",
		"Warning: CI is slow on the hc tests", "Done: PR 633 is merged, new"} {
		f.rect(t, l)
	}
	if _, ok := f.tt.Find("Decide the release name"); ok {
		t.Error("a pin of the terminal shows in Chat mode")
	}
	// Terminal mode: the pins again.
	f.o.Chat = func() (ChatTicks, bool) { return ChatTicks{}, false }
	f.tt.Frame()
	f.rect(t, "Decide the release name")
	if _, ok := f.tt.Find("You: start builders for 609"); ok {
		t.Error("a tick of the chat shows in Terminal mode")
	}
}

func TestOutlineChatWithNoTickHasNoColumn(t *testing.T) {
	f, _ := newChatOutline(t, nil, 44, 120)
	f.tt.Frame()
	if _, ok := f.tt.Find(LabelOutline); ok {
		t.Error("a column for a chat with no tick")
	}
	if _, ok := f.o.Top(); ok {
		t.Error("a top for a chat with no tick")
	}
}

func TestOutlineChatColumnKeepsToTheListRoom(t *testing.T) {
	f, _ := newChatOutline(t, chatTicks(), 44, 120)
	f.tt.Frame()
	f.tt.Frame()
	col := f.rect(t, LabelOutline)
	if col.Y != 44 || col.Y+col.H != 600-120 {
		t.Errorf("column %v, want from 44 to %d", col, 600-120)
	}
	if f.o.height != col.H {
		t.Errorf("height %v, want the column's %v", f.o.height, col.H)
	}
}

func TestOutlineChatCountsTheNewTicks(t *testing.T) {
	f, _ := newChatOutline(t, chatTicks(), 44, 120)
	_, v, ok := f.o.view(ChatTicks{Addr: apptest.OrchAddr, Ticks: chatTicks()}, true, 0)
	if !ok || v.Count != 1 || !v.New[3] || v.New[1] {
		t.Errorf("view %+v, want the last tick new", v)
	}
	f.tt.Frame()
	if _, ok := f.tt.Find(LabelOutlineCount); !ok {
		t.Errorf("no count; texts %q", f.tt.Texts())
	}
}

// A tick that waits is blocked, new or not; a new one is ink-100; the others are `tick`.
func TestOutlineTickInks(t *testing.T) {
	for _, p := range []*theme.Palette{theme.Dark, theme.Light} {
		waits := OutlineTick{Blocked: true}
		cases := []struct {
			t     OutlineTick
			isNew bool
			want  ui.Color
		}{
			{waits, false, p.Blocked},
			{waits, true, p.Blocked},
			{OutlineTick{}, true, p.Ink100},
			{OutlineTick{}, false, p.Tick},
		}
		for i, c := range cases {
			if got := tickInk(p, c.t, c.isNew); got != c.want {
				t.Errorf("case %d: %v, want %v", i, got, c.want)
			}
		}
	}
}

func TestOutlineChatClickJumpsAndMarksTheOnesBeforeRead(t *testing.T) {
	ticks := chatTicks()
	ticks = append(ticks, OutlineTick{ID: "s4/0", Kind: "Done", Headline: "PR 634 is merged", Pinned: outlineClockStart.Add(20 * time.Minute), Width: 8})
	f, jumps := newChatOutline(t, ticks, 44, 120)
	f.tt.Frame()
	if err := f.tt.Click("Done: PR 633 is merged, new"); err != nil {
		t.Fatal(err)
	}
	if len(*jumps) != 1 || (*jumps)[0] != "s3/0" {
		t.Errorf("jumps %q, want s3/0", *jumps)
	}
	_, v, _ := f.o.view(ChatTicks{Addr: apptest.OrchAddr, Ticks: ticks}, true, 0)
	if v.Count != 1 || v.New[3] || !v.New[4] {
		t.Errorf("view %+v, want only the last new", v)
	}
}

func TestOutlineChatCardShowsTheKindAndTheFirstWords(t *testing.T) {
	f, _ := newChatOutline(t, chatTicks(), 44, 120)
	f.tt.Frame()
	f.hover(t, "Warning: CI is slow on the hc tests")
	for _, s := range []string{"Warning", "CI is slow on the hc tests", "13:40"} {
		if !f.tt.HasText(s) {
			t.Errorf("card has no %q; texts %q", s, f.tt.Texts())
		}
	}
	// A sent prompt has no time: the card shows none.
	if got := outlineCardText(OutlineTick{Kind: "You", Headline: "go"}); got != "go. You" {
		t.Errorf("card text %q, want %q", got, "go. You")
	}
}

func TestOutlineFitReducesTheGapThenShowsTheNewest(t *testing.T) {
	for _, c := range []struct {
		n         int
		h         float32
		shown     int
		pitch     float32
		situation string
	}{
		{3, 0, 3, outlinePitch, "not laid out"},
		{10, 200, 10, outlinePitch, "fits at 11px"},
		{40, 200, 40, 5, "a smaller gap"},
		{100, 200, 50, outlineMinPitch, "the newest that fit at 4px"},
		{3, 2, 1, outlineMinPitch, "one at least"},
	} {
		shown, pitch := outlineFit(c.n, c.h)
		if shown != c.shown || pitch != c.pitch {
			t.Errorf("%s: outlineFit(%d, %v) = %d, %v; want %d, %v", c.situation, c.n, c.h, shown, pitch, c.shown, c.pitch)
		}
	}
}

// A long thread: the column keeps inside the list's room, the newest tick shows.
func TestOutlineChatManyTicksStayInTheRoom(t *testing.T) {
	var ticks []OutlineTick
	for i := range 300 {
		ticks = append(ticks, OutlineTick{ID: "r" + strconv.Itoa(i), Kind: "You", Headline: "Prompt " + strconv.Itoa(i),
			Pinned: outlineClockStart.Add(-time.Duration(300-i) * time.Minute), Width: 8})
	}
	f, _ := newChatOutline(t, ticks, 44, 120)
	f.tt.Frame()
	f.tt.Frame()
	col := f.rect(t, LabelOutline)
	if newest := f.rect(t, "You: Prompt 299"); newest.Y+newest.H > col.Y+col.H {
		t.Errorf("the newest tick %v is out of the column %v", newest, col)
	}
	_, v, _ := f.o.view(ChatTicks{Addr: apptest.OrchAddr, Ticks: ticks}, true, f.o.height)
	if len(v.Ticks) != int(col.H/outlineMinPitch) || v.Pitch != outlineMinPitch {
		t.Errorf("%d ticks at %v in %v, want the newest that fit at %d", len(v.Ticks), v.Pitch, col.H, outlineMinPitch)
	}
	top := f.rect(t, "You: Prompt "+strconv.Itoa(300-len(v.Ticks)))
	if top.Y < col.Y {
		t.Errorf("the oldest tick shown %v is above the column %v", top, col)
	}
}

// The chat's outline asks for no frame at rest (D55): after the fit settles, nothing redraws.
func TestOutlineChatAsksForNoFrameAtRest(t *testing.T) {
	var ticks []OutlineTick
	for i := range 120 {
		ticks = append(ticks, OutlineTick{ID: "r" + strconv.Itoa(i), Kind: "You", Headline: "Prompt",
			Pinned: outlineClockStart.Add(time.Duration(i) * time.Second), Width: 8})
	}
	f, _ := newChatOutline(t, ticks, 44, 120)
	redraws := 0
	f.o.Env.Invalidate = func() { redraws++ }
	f.tt.Frame()
	f.tt.Frame()
	redraws = 0
	for range 3 {
		f.tt.Frame()
		f.tt.Advance(time.Second)
	}
	if redraws != 0 || f.tt.FramePending() {
		t.Errorf("%d redraws, frame pending %v at rest; want none", redraws, f.tt.FramePending())
	}
}
