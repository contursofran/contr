package chatview

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/github"
)

// written is each token as its source has it, with its kind and name.
func written(src string, toks []Token) []string {
	var out []string
	for _, t := range toks {
		out = append(out, fmt.Sprintf("%d %s %s", t.Kind, t.Name, src[t.Start:t.End]))
	}
	return out
}

func TestTokens(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want []string
	}{
		{"PR #667 and #656.", []string{"1 PR PR", "0 667 #667", "1 and and", "0 656 #656"}},
		{"b-649 waits; see internal/app/kit/agentrow.go:42, ChatView.md.",
			[]string{"1 b-649 b-649", "1 waits waits", "1 see see", "2 internal/app/kit/agentrow.go internal/app/kit/agentrow.go:42", "2 ChatView.md ChatView.md"}},
		{"**b-649** and _#7_", []string{"1 b-649 b-649", "1 and and", "0 7 #7"}},
		{"my_file.go", []string{"2 my_file.go my_file.go"}},
		// Code, links, images and HTML are not touched.
		{"`#3` and `b-649`", []string{"1 and and"}},
		{"```\nb-649 #3 x.go\n```", nil},
		{"    b-649 indented code", nil},
		{"[b-649](https://example.com) ![x.go](x.png) <b-649>", nil},
		// No number: glued to a word, letters after the digits, no digits.
		{"PR#5 #5a # #", []string{"1 PR PR"}},
		// No name: a pair with a colon, digits alone, a URL.
		{"a:b 42 1.5", nil},
		// A table cell and a heading are text too.
		{"# b-1\n\n| #2 |\n|---|\n| x.go |", []string{"1 b-1 b-1", "0 2 #2", "2 x.go x.go"}},
		// An escape ends a run: the name after it is still found.
		{`\#4 b-2`, []string{"1 b-2 b-2"}},
	} {
		got := written(tc.src, Tokens(tc.src))
		if !slices.Equal(got, tc.want) {
			t.Errorf("Tokens(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// fakeChips knows some names, and records what is looked up and opened.
type fakeChips struct {
	known  map[string]Chip
	looked []string
	opened []string
}

func (f *fakeChips) Look(addr string, t Token) (Chip, bool) {
	f.looked = append(f.looked, t.Name)
	c, ok := f.known[t.Name]
	return c, ok
}

func (f *fakeChips) Open(addr string, c Chip, name string) {
	f.opened = append(f.opened, fmt.Sprintf("%s %d %s", addr, c.Kind, name))
}

func knownChips() *fakeChips {
	return &fakeChips{known: map[string]Chip{
		"667":        {Kind: ChipPR, State: StatePassed},
		"b-649":      {Kind: ChipWorker, State: StateNeedsYou},
		"my_file.go": {Kind: ChipFile},
	}}
}

func TestChipTextRewritesOnlyKnownNames(t *testing.T) {
	ch := New(view.Env{}, t.TempDir(), nil)
	ch.Chips = knownChips()
	src := "PR #667 and #3 by b-649 in my_file.go:4 and nope.go, `b-649`."
	got := ch.chipText("a1", 0, src)
	want := `PR [#667](contr-chip:pr/passed/667 "PR #667 · checks passed") and #3 by ` +
		`[b-649](contr-chip:worker/needs-you/b-649 "Worker b-649 · needs you") in ` +
		`[my\_file.go:4](contr-chip:file//my_file.go "Open my_file.go") and nope.go, ` + "`b-649`."
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	// The chip follows the state of now: the source is made again when a state changes, and
	// kept while none does.
	fc := ch.Chips.(*fakeChips)
	fc.known["667"] = Chip{Kind: ChipPR, State: StateMerged}
	if got := ch.chipText("a1", 0, src); !strings.Contains(got, "contr-chip:pr/merged/667") {
		t.Errorf("the state did not follow: %s", got)
	}
	again := ch.chipText("a1", 0, src)
	if again != ch.chips["a1/0"].out {
		t.Error("the source is not kept")
	}
	// Without a source, the text stays as it is.
	ch.Chips = nil
	if got := ch.chipText("a1", 0, src); got != src {
		t.Errorf("no source: %s", got)
	}
}

func TestChipDestRoundTrip(t *testing.T) {
	for k := ChipPR; k <= ChipFile; k++ {
		for s := StateNone; s <= StateWaiting; s++ {
			c := Chip{Kind: k, State: s}
			got, name, ok := parseChipDest(chipDest(c, "a/b.go"))
			if !ok || got != c || name != "a/b.go" {
				t.Errorf("%v: got %v %q %v", c, got, name, ok)
			}
		}
	}
	for _, bad := range []string{"https://x", "contr-chip:", "contr-chip:pr/open/", "contr-chip:nope/open/1",
		"contr-chip:pr/nope/1", "contr-chip:pr/open"} {
		if _, _, ok := parseChipDest(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestChipStates(t *testing.T) {
	pr := func(state string, pass, fail, pending int) github.Ref {
		return github.Ref{Number: 1, PR: true, State: state, Checks: github.CheckSummary{Pass: pass, Fail: fail, Pending: pending}}
	}
	for _, tc := range []struct {
		ref  github.Ref
		want Chip
	}{
		{pr("OPEN", 0, 0, 0), Chip{ChipPR, StateOpen}},
		{pr("OPEN", 2, 0, 1), Chip{ChipPR, StateRunning}},
		{pr("OPEN", 2, 1, 1), Chip{ChipPR, StateFailed}},
		{pr("OPEN", 3, 0, 0), Chip{ChipPR, StatePassed}},
		{pr("MERGED", 0, 1, 0), Chip{ChipPR, StateMerged}},
		{pr("CLOSED", 3, 0, 0), Chip{ChipPR, StateClosed}},
		{github.Ref{Number: 1, State: "OPEN"}, Chip{ChipIssue, StateNone}},
	} {
		if got := RefChip(tc.ref); got != tc.want {
			t.Errorf("RefChip(%+v) = %v, want %v", tc.ref, got, tc.want)
		}
	}
	for _, tc := range []struct {
		w    agents.Worker
		want ChipState
	}{
		{agents.Worker{Live: true, Status: "blocked"}, StateNeedsYou},
		{agents.Worker{Live: true, Status: "working"}, StateWorking},
		{agents.Worker{Live: true, Status: "idle"}, StateWaiting},
		{agents.Worker{Live: true}, StateWaiting},
		{agents.Worker{Status: "working"}, StateDone},
	} {
		if got := WorkerChip(tc.w); got != (Chip{ChipWorker, tc.want}) {
			t.Errorf("WorkerChip(%+v) = %v", tc.w, got)
		}
	}
	for _, p := range []*theme.Palette{theme.Dark, theme.Light} {
		for s, want := range map[ChipState]any{
			StateOpen: p.ContentInfo, StateRunning: p.ContentRunning, StatePassed: p.ContentDone,
			StateFailed: p.ContentFailed, StateMerged: p.ContentDone, StateClosed: p.ContentInfo,
			StateNeedsYou: p.ContentFailed, StateWorking: p.ContentRunning, StateDone: p.ContentDone,
			StateWaiting: p.ContentWaiting,
		} {
			if got, ok := ChipColor(p, s); !ok || got != want {
				t.Errorf("state %d: %v %v", s, got, ok)
			}
		}
		if _, ok := ChipColor(p, StateNone); ok {
			t.Error("no state has a dot")
		}
	}
}

func TestChipIcon(t *testing.T) {
	if _, ok := chipIcon("https://example.com", 14); ok {
		t.Error("a web link has an icon")
	}
	if _, ok := chipIcon(chipDest(Chip{Kind: ChipIssue}, "4"), 14); ok {
		t.Error("an issue has a dot")
	}
	o, ok := chipIcon(chipDest(Chip{ChipPR, StatePassed}, "4"), 14)
	if !ok || o.Width != chipDot+chipDotGap || o.Draw == nil {
		t.Errorf("PR dot: %+v %v", o, ok)
	}
	if o, ok := chipIcon(chipDest(Chip{Kind: ChipFile}, "a.go"), 14); !ok || o.Draw == nil || o.Width <= 0 {
		t.Errorf("file icon: %+v %v", o, ok)
	}
}

// iconMark starts the text of a link with an icon: the icon's object and a word joiner.
const iconMark = "\uFFFC\u2060"

// said is a text of Claude as a transcript line.
func said(id, text string) string {
	return `{"type":"assistant","uuid":"` + id + `","message":{"content":[{"type":"text","text":` + strconv.Quote(text) + `}]}}`
}

func TestChipClicksOpenTheirSource(t *testing.T) {
	s := newSession(t, "idle")
	fc := knownChips()
	s.ch.Chips = fc
	s.write(lPrompt, said("c1", "PR #667 by b-649 in my_file.go, and #3."))
	for _, name := range []string{"#667", "b-649", "my_file.go"} {
		if err := s.tt.Click(iconMark + name); err != nil {
			t.Fatalf("%s: %v in %v", name, err, s.tt.Texts())
		}
		s.tt.Frame()
	}
	want := []string{term + " 1 667", term + " 3 b-649", term + " 4 my_file.go"}
	if !slices.Equal(fc.opened, want) {
		t.Errorf("opened %q, want %q", fc.opened, want)
	}
	if err := s.tt.Click(iconMark + "#3"); err == nil {
		t.Error("an unknown number is a link")
	}
}

func TestOpenLinkScrollsToAHeadingAndIgnoresOtherSchemes(t *testing.T) {
	ch := New(view.Env{}, t.TempDir(), nil)
	ch.openLink("#install")
	if ch.md.Anchor != "#install" || ch.md.AnchorVersion != 1 {
		t.Errorf("anchor %q v%d", ch.md.Anchor, ch.md.AnchorVersion)
	}
	// No source, no window: nothing happens, nothing panics.
	ch.openLink(chipDest(Chip{ChipPR, StateOpen}, "1"))
	ch.openLink("javascript:alert(1)")
	ch.openLink("https://example.com")
}

// Only the texts on screen are looked up: a long transcript asks for none of its old names.
func TestNoLookupForTextsOffScreen(t *testing.T) {
	s := newSession(t, "idle")
	fc := knownChips()
	s.ch.Chips = fc
	lines := []string{lPrompt, said("old", "the old #1")}
	for i := range 60 {
		lines = append(lines, said("s"+strconv.Itoa(i), "line "+strconv.Itoa(i)+"\n\nmore text\n\nand more"))
	}
	s.write(lines...)
	s.tt.Frame()
	if slices.Contains(fc.looked, "1") {
		t.Error("a name off screen was looked up")
	}
	if !slices.Contains(fc.looked, "more") {
		t.Errorf("no name on screen was looked up: %q", fc.looked)
	}
}

// A chat at rest with chips asks for no frame.
func TestChipsAskForNoRedrawAtRest(t *testing.T) {
	s := newSession(t, "idle")
	s.ch.Chips = knownChips()
	s.write(lPrompt, said("c1", "PR #667 by b-649."))
	s.tt.Frame()
	s.tt.Advance(2 * theme.MotionSelect) // past the switch's fade (D256)
	if s.tt.FramePending() {
		t.Error("a redraw is asked at rest")
	}
}
