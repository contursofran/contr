package chatview

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
)

// The family comes from the model, then the profile's model, then the program (D285).
func TestFamilyOf(t *testing.T) {
	for _, tc := range []struct {
		model, profile, command string
		want                    Family
	}{
		{"opus", "", "", FamilyClaude},
		{"claude-sonnet-5-5", "", "", FamilyClaude},
		{"", "", "/usr/local/bin/claude", FamilyClaude},
		{"", "haiku", "node", FamilyClaude},
		{"gpt-6.1-sol", "", "", FamilyOpenAI},
		{"", "", "codex", FamilyOpenAI},
		{"o4-mini", "", "", FamilyOpenAI},
		{"DeepSeek-V4.1-Flash", "", "opencode", FamilyDeepSeek},
		{"", "deepseek-chat", "opencode", FamilyDeepSeek},
		{"", "", "opencode", FamilyNone},
		{"", "", "zsh", FamilyNone},
		{"", "", "", FamilyNone},
		{"llama", "", "", FamilyNone},
		{"kimi", "", "claude", FamilyClaude},
	} {
		if got := FamilyOf(tc.model, tc.profile, tc.command); got != tc.want {
			t.Errorf("FamilyOf(%q, %q, %q) = %q, want %q", tc.model, tc.profile, tc.command, got, tc.want)
		}
	}
}

// Each family has a mark; none has none.
func TestFamilyMark(t *testing.T) {
	for _, f := range []Family{FamilyClaude, FamilyOpenAI, FamilyDeepSeek} {
		if familyMark(f) == nil {
			t.Errorf("no mark for %q", f)
		}
	}
	if familyMark(FamilyNone) != nil {
		t.Error("a mark for no family")
	}
}

// The ink of a worker tells its state: blocked ink-100, working ink-88, idle and done ink-30; no
// hue (D259, D285).
func TestWorkerInk(t *testing.T) {
	pal := theme.Current()
	for status, want := range map[string]ui.Color{"blocked": pal.Ink100, "working": pal.Ink88, "idle": pal.Ink30, "done": pal.Ink30, "": pal.Ink30} {
		if got := WorkerInk(pal, status); got != want {
			t.Errorf("WorkerInk(%q) = %v, want %v", status, got, want)
		}
	}
}

// A worker of a known family shows its mark in place of the dot, in a box of its own, with no
// red (D285, D259). Another program keeps the dot.
func TestWorkerMarkByState(t *testing.T) {
	for _, status := range []string{"working", "idle", "blocked", "done"} {
		r := newRig(t)
		r.ch.ModelOf = func(p string) string {
			if p == "builder" {
				return "sonnet"
			}
			return ""
		}
		r.ch.Worker = func(addr string) (agents.Worker, bool) {
			return agents.Worker{Name: "b-631", Address: addr, Status: status, Profile: "builder"}, addr == "w2:t1"
		}
		r.savePath(term)
		r.selectTerm(term)
		r.chat()
		r.write(lPrompt, lNote1, lSaid2)
		box := apptest.Box(t, r.tt, "b-631 "+status)
		if box.W != markSize || box.H != markSize {
			t.Errorf("%s: mark is %vx%v, want %v", status, box.W, box.H, markSize)
		}
		if n := reddish(r.tt.Image(), box); n > 0 {
			t.Errorf("%s: the mark has %d red pixels", status, n)
		}
	}

	r := newRig(t)
	r.ch.Worker = func(addr string) (agents.Worker, bool) {
		return agents.Worker{Name: "b-631", Address: addr, Status: "working", Command: "opencode"}, addr == "w2:t1"
	}
	r.savePath(term)
	r.selectTerm(term)
	r.chat()
	r.write(lPrompt, lNote1, lSaid2)
	if box := apptest.Box(t, r.tt, "b-631 working"); box.W != dotSize {
		t.Errorf("an unknown program shows a %v box, want the %v dot", box.W, dotSize)
	}
}
