package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/chatview"
	"github.com/contursofran/contr/internal/app/state"
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

// TestFrameChatPressesKeysThroughTheSource: the composer presses its keys (Ctrl+V of a pasted image,
// D269) through the agent source, as it sends and interrupts.
func TestFrameChatPressesKeysThroughTheSource(t *testing.T) {
	r := attachCounting(t)
	if r.f.Chat.Send == nil || r.f.Chat.Keys == nil || r.f.Chat.Interrupt == nil {
		t.Fatalf("send %v, keys %v, interrupt %v", r.f.Chat.Send != nil, r.f.Chat.Keys != nil, r.f.Chat.Interrupt != nil)
	}
}
