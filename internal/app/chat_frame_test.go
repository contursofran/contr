package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/chatview"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/owner"
	"github.com/contursofran/contr/internal/session"
	"github.com/contursofran/contr/internal/statedir"
)

// pick selects the agent at addr and draws, after the reads of the selection land.
func (r *countRig) pick(t *testing.T, kind state.SelectedKind, addr string) {
	t.Helper()
	r.f.Selection.Select(state.Selected{Kind: kind, ID: addr})
	r.w.flush()
	r.tt.Frame()
}

// The switch shows for a Claude Code session: an orchestrator, or a worker whose transcript path
// a hook saved. A worker without a path and a plain terminal get none (D246).
func TestFrameChatSwitchShowsForAClaudeSession(t *testing.T) {
	r := attachCounting(t)
	r.src.Snap.Terminals = []agents.Worker{{Name: "zsh", Address: "ta", Project: "a", Role: agents.RoleTerminal, Live: true}}
	r.f.syncAgents()
	r.pick(t, state.KindOrchestrator, "oa")
	if !r.tt.HasText(chatview.TextChat) {
		t.Fatalf("orchestrator: no switch in %v", r.tt.Texts())
	}
	r.pick(t, state.KindTerminal, "ta")
	if r.tt.HasText(chatview.TextChat) {
		t.Error("a plain terminal shows the switch")
	}
	r.pick(t, state.KindWorker, "wa")
	if r.tt.HasText(chatview.TextChat) {
		t.Error("a worker without a transcript path shows the switch")
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := session.Update(statedir.Dir(), "wa", func(s *session.Session) (bool, error) {
		return s.SetTranscript("s", path), nil
	}); err != nil {
		t.Fatal(err)
	}
	r.pick(t, state.KindTerminal, "ta")
	r.pick(t, state.KindWorker, "wa")
	waitUntil(t, func() bool { r.w.flush(); r.tt.Frame(); return r.tt.HasText(chatview.TextChat) })
}

// ⌘E switches the selected session to Chat, where the chat takes the grid's place and its
// composer takes the focus, and back.
func TestFrameChatShortcutAndComposer(t *testing.T) {
	r := attachCounting(t)
	r.pick(t, state.KindOrchestrator, "oa")
	r.tt.Key(ui.Cmd, ui.KeyE)
	r.w.flush()
	r.tt.Frame()
	composer := func() bool { _, ok := r.tt.Find(chatview.LabelComposer); return ok }
	if !r.f.Chat.On("oa") || !r.tt.HasText(chatview.TextEmpty) || !composer() {
		t.Fatalf("⌘E: on %v, texts %v", r.f.Chat.On("oa"), r.tt.Texts())
	}
	if !r.tt.Focused(chatview.LabelComposer) {
		t.Error("the composer does not take the focus on ⌘E")
	}
	r.pick(t, state.KindOrchestrator, "ob")
	if composer() {
		t.Error("Chat mode moved to another session")
	}
	r.pick(t, state.KindOrchestrator, "oa")
	if !composer() {
		t.Error("Chat mode of a session is not kept")
	}
	// ⌘E works with the focus in the composer.
	r.tt.Key(ui.Cmd, ui.KeyE)
	r.tt.Frame()
	if r.f.Chat.On("oa") || composer() {
		t.Error("⌘E in the composer does not switch back")
	}
	// ⌘E does nothing for a terminal that is not a Claude session.
	r.pick(t, state.KindWorker, "wa")
	r.tt.Key(ui.Cmd, ui.KeyE)
	r.tt.Frame()
	if r.f.Chat.On("wa") {
		t.Error("⌘E switched a terminal without a transcript")
	}
}

// The tests of this package use a state folder of their own (TestMain).
func TestFrameTestsUseAScratchStateDir(t *testing.T) {
	home, _ := os.UserHomeDir()
	if dir := statedir.Dir(); dir == "" || (home != "" && strings.HasPrefix(dir, home)) {
		t.Errorf("state dir %q is not a scratch folder", dir)
	}
}

// Chat mode shows no hands drawing, and the empty terminal shows no chat (D246, D247).
func TestFrameChatAndTheEmptyTerminalExcludeEachOther(t *testing.T) {
	r := attachCounting(t)
	r.pick(t, state.KindOrchestrator, "oa")
	r.f.Chat.SetOn("oa", true)
	r.tt.Frame()
	if !!apptest.Absent(r.tt, chatview.LabelComposer) || !apptest.Absent(r.tt, LabelTerminalArt) {
		t.Errorf("chat mode: dock %v, art absent %v", !apptest.Absent(r.tt, chatview.LabelComposer), apptest.Absent(r.tt, LabelTerminalArt))
	}
	r.f.Selection.Clear()
	r.w.flush()
	r.tt.Frame()
	if !apptest.Absent(r.tt, chatview.LabelComposer) || r.tt.HasText(chatview.TextChat) || apptest.Absent(r.tt, LabelTerminalArt) {
		t.Errorf("empty terminal: texts %v", r.tt.Texts())
	}
}

// In Chat mode the outline shows the ticks of the chat in place of the pins (D270): a pin and a
// question take their text from the owner store, and a click scrolls the chat to the row. Back
// in Terminal mode, the pins show again.
func TestFrameChatOutline(t *testing.T) {
	r := attachCounting(t)
	r.src.Snap.Todos = []owner.Todo{{ID: "T1", Project: "a", Kind: "do", Text: "Test the build", Pinned: time.Now().Add(-time.Hour)}}
	r.src.Snap.Questions = []owner.Question{{ID: "D1", Project: "a", Question: "Which name for the release?"}}
	r.f.syncAgents()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-11-10T10:00:00Z","message":{"content":"start builders for 609"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-10T10:01:00Z","message":{"content":[{"type":"text","text":"> [!DECISION]\n> Ship the notch now?"}]}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-11-10T10:02:00Z","message":{"content":[{"type":"tool_use","id":"p1","name":"mcp__plugin_contr_contr__pin_for_owner","input":{}}]}}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-11-10T10:02:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"p1","content":"T1"}]}}`,
		`{"type":"assistant","uuid":"a3","timestamp":"2026-11-10T10:03:00Z","message":{"content":[{"type":"tool_use","id":"q1","name":"mcp__plugin_contr_contr__ask_owner","input":{}}]}}`,
		`{"type":"user","uuid":"u3","timestamp":"2026-11-10T10:03:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"q1","content":"D1"}]}}`,
		`{"type":"assistant","uuid":"a4","timestamp":"2026-11-10T10:04:00Z","message":{"content":[{"type":"tool_use","id":"q2","name":"mcp__plugin_contr_contr__ask_owner","input":{}}]}}`,
		`{"type":"user","uuid":"u4","timestamp":"2026-11-10T10:04:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"q2","content":"D0"}]}}`,
	}
	// A long tail, so that a click on the first tick scrolls the chat up.
	long := strings.Repeat("The builder reads the issue, the code and the spec, then writes its steps. ", 12)
	for i := range 12 {
		lines = append(lines, `{"type":"assistant","uuid":"t`+strconv.Itoa(i)+`","message":{"content":[{"type":"text","text":"`+long+`"}]}}`)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.Update(statedir.Dir(), "oa", func(s *session.Session) (bool, error) {
		return s.SetTranscript("s", path), nil
	}); err != nil {
		t.Fatal(err)
	}
	// The notch shows the text of the pin too: the pin's tick is a second one.
	pinTicks := func() int {
		n := 0
		for _, s := range r.tt.Texts() {
			if s == "Test the build" {
				n++
			}
		}
		return n - 1
	}
	r.pick(t, state.KindOrchestrator, "oa")
	if pinTicks() != 1 {
		t.Fatalf("Terminal mode: no pin tick in %v", r.tt.Texts())
	}
	r.f.Chat.SetOn("oa", true)
	ticks := []string{"You: start builders for 609", "Decision: Ship the notch now?", "Pinned: Test the build",
		"Question: Which name for the release?", "Question: Question D0"}
	waitUntil(t, func() bool {
		r.w.flush()
		r.tt.Frame()
		_, ok := r.tt.Find(ticks[len(ticks)-1])
		return ok
	})
	for _, l := range ticks {
		if _, ok := r.tt.Find(l); !ok {
			t.Errorf("Chat mode: no tick %q in %v", l, r.tt.Texts())
		}
	}
	if pinTicks() != 0 {
		t.Errorf("Chat mode: the pin of Terminal mode shows too: %q", r.tt.Texts())
	}
	ct, ok := r.f.chatOutline()
	if !ok || len(ct.Ticks) != len(ticks) || ct.Addr != "oa" {
		t.Fatalf("chatOutline %+v, %v", ct, ok)
	}
	// The open question and the decision that waits are blocked; the decided question is not.
	for i, want := range []bool{false, true, false, true, false} {
		if ct.Ticks[i].Blocked != want {
			t.Errorf("tick %q blocked %v, want %v", ct.Ticks[i].Headline, ct.Ticks[i].Blocked, want)
		}
	}
	if top, bottom, _ := r.f.Chat.Area(); ct.Top != top || ct.Bottom != bottom || top <= 0 || bottom <= 0 {
		t.Errorf("the column's room %v %v, want the list's %v %v", ct.Top, ct.Bottom, top, bottom)
	}
	if r.tt.HasText(chatview.TextJump) {
		t.Fatal("the chat does not start at its end")
	}
	if err := r.tt.Click("You: start builders for 609"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		r.tt.Frame()
		r.tt.Advance(time.Second)
	}
	if !r.tt.HasText(chatview.TextJump) {
		t.Errorf("a click on the first tick does not scroll the chat up; texts %q", r.tt.Texts())
	}
	r.f.Chat.SetOn("oa", false)
	r.tt.Frame()
	if pinTicks() != 1 {
		t.Errorf("back in Terminal mode: no pin tick in %v", r.tt.Texts())
	}
}
