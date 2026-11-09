//go:build e2e

package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/session"
	"github.com/contursofran/contr/internal/updated"
)

// seeFixture is a state of the app that scripts/see.sh can show.
type seeFixture struct {
	about string
	setup func(t *testing.T, h *harness)
	ready string // a label prefix that shows once the state is on screen
}

// seeFixtures are the states scripts/see.sh knows. Add one when a change
// needs a state that none of these shows. Data looks like real Claude Code
// workers.
var seeFixtures = map[string]seeFixture{
	"empty": {
		about: "no projects and no agents",
		setup: func(t *testing.T, h *harness) {},
		ready: "Sidebar",
	},
	"workers": {
		about: "project demo: an orchestrator and two builders, one working",
		setup: seeWorkers,
		ready: "lead, ",
	},
	"questions": {
		about: "the workers fixture with two questions and a to-do for the owner",
		setup: func(t *testing.T, h *harness) {
			seeWorkers(t, h)
			h.owner(t, `{"questions":[
{"id":"D1","project":"demo","title":"Which database for the cache?","question":"The cache needs a store. Redis is fast but one more service; sqlite is already in the app. Which one should it use?","options":["sqlite","redis","files"],"recommended":0,"asked":"2026-11-03T10:00:00Z"},
{"id":"D2","project":"demo","title":"Keep the old outline ticks?","question":"The new outline replaces the pin bar. Remove the old ticks now, or after one release?","options":["now","after one release"],"recommended":0,"asked":"2026-11-03T10:05:00Z"}],
"todos":[{"id":"T1","project":"demo","kind":"do","text":"Test the notch drag on the Mac","pinned":"2026-11-03T10:02:00Z"}],
"next_question":3,"next_todo":2}`)
		},
		ready: "lead, ",
	},
	"wide": {
		about: "the core sized a worker's terminal to 300x60 before the app shows it (#481)",
		setup: func(t *testing.T, h *harness) {
			h.spawn(t, agents.Spec{Name: "b203-wide", Profile: "sizer", Role: "builder", Issue: "203", Project: "demo", Worktree: h.repo(t, "demo", "main")})
			h.sizeTerminal(t, "b203-wide", 300, 60)
		},
		ready: "b203-wide",
	},
	"context": {
		about: "the workers fixture; lead has no status line file, only the Stop hook's context labels (D205)",
		setup: func(t *testing.T, h *harness) {
			seeWorkers(t, h)
			h.labelTerminal(t, "lead", map[string]string{agents.LabelContext: "90000", agents.LabelContextLimit: "160000"})
		},
		ready: "lead, ",
	},
	"updated": {
		about: "the first start after an update: the Updated to pill with What's new open (D207)",
		setup: func(t *testing.T, h *harness) {
			n := updated.Note{Version: app.Version, From: "0.0.9", InstalledAt: time.Now().Add(-3 * time.Minute),
				Notes: "## " + app.Version + "\n- What's new opens after an update\n- Faster tab switching\n- Light theme fixes"}
			if err := updated.Save(updated.Path(filepath.Join(h.dir, "state")), n); err != nil {
				t.Fatal(err)
			}
		},
		ready: app.UpdatedPrefix,
	},
	"session": {
		about: "the orchestrator lead with the todos and artifacts of canvas board 11 (D197)",
		setup: seeSession,
		ready: "Todos",
	},
}

// sizeTerminal attaches to the worker's terminal with input at cols x rows,
// and detaches, so the core keeps that size.
func (h *harness) sizeTerminal(t *testing.T, name string, cols, rows uint16) {
	t.Helper()
	ws, err := h.mgr.List(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range ws {
		if w.Name != name {
			continue
		}
		fc, err := hc.DialFramed(h.ctx, hc.Command("", h.env))
		if err != nil {
			t.Fatal(err)
		}
		defer fc.Close()
		s, err := fc.Attach(h.ctx, hc.AttachParams{Terminal: w.Address, Cols: cols, Rows: rows})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		_ = s.Close()
		return
	}
	t.Fatalf("no worker %q", name)
}

// labelTerminal sets labels on the worker's terminal, as the kit's Stop hook
// does.
func (h *harness) labelTerminal(t *testing.T, name string, labels map[string]string) {
	t.Helper()
	ws, err := h.mgr.List(h.ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range ws {
		if w.Name == name {
			if err := h.cl.Call(h.ctx, "terminal.label", map[string]any{"terminal": w.Address, "labels": labels}, nil); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no worker %q", name)
}

// seeSession spawns lead and writes the session file of its terminal, as the
// kit's hooks do (D195).
func seeSession(t *testing.T, h *harness) {
	repo := h.repo(t, "demo", "main")
	w, err := h.mgr.Spawn(h.ctx, agents.Spec{Name: "lead", Profile: "claude-idle", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo})
	if err != nil {
		t.Fatalf("spawn lead: %v", err)
	}
	now := uint64(time.Now().Unix())
	todo := func(text, status string, you bool) session.Todo {
		return session.Todo{Text: text, Active: text, Status: status, You: you}
	}
	s := &session.Session{UpdatedAt: now, Todos: []session.Todo{
		todo("Merge #453, the flow key", session.Completed, false),
		todo("Send the canvas backdrop as a PNG", session.Completed, true),
		{Text: "Design the todos and artifacts panel", Active: "Designing the todos and artifacts panel", Status: session.InProgress, You: true},
		todo("Merge #460 after CI passes", session.Pending, false),
		todo("Start step 2 of #456 (Flow section)", session.Pending, false),
		todo("Move plan usage to the left sidebar", session.Pending, true),
		todo("Open an issue for the todo hook", session.Pending, true),
	}, Artifacts: []session.Artifact{
		{Slug: "https://claude.ai/artifact/9bmb", Title: "contr · from-zero canvas", UpdatedAt: now},
		{Slug: "9bmb", Title: "contr · from-zero canvas", UpdatedAt: now - 60},
		{Slug: "design-system", Title: "contr design system", URL: "https://claude.ai/artifact/45Kp", UpdatedAt: now - 38*60},
		{Slug: "native-final", Title: "Native · final", URL: "https://claude.ai/artifact/LU6P", UpdatedAt: now - 2*86400},
	}}
	if err := session.Write(filepath.Join(h.dir, "state"), w.Address, s); err != nil {
		t.Fatal(err)
	}
}

func seeWorkers(t *testing.T, h *harness) {
	repo := h.repo(t, "demo", "main")
	h.spawn(t,
		agents.Spec{Name: "lead", Profile: "claude-idle", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo},
		agents.Spec{Name: "b201-s1-notch", Profile: "claude-working", Role: "builder", Issue: "201", Project: "demo", Worktree: repo},
		agents.Spec{Name: "b202-s1-outline", Profile: "claude-idle", Role: "builder", Issue: "202", Project: "demo", Worktree: repo},
	)
}

// TestSee is scripts/see.sh (#457, D178). It starts the fixture CONTR_SEE
// names, in the themes of CONTR_SEE_THEME (dark, light or both), presses the
// labels of CONTR_SEE_PRESS (one per line) in order, and writes
// tmp/see/<fixture>-<theme>.png and tmp/see/<fixture>-<theme>.txt, the tree
// of elements. Without CONTR_SEE it lists the fixtures.
func TestSee(t *testing.T) {
	name := os.Getenv("CONTR_SEE")
	f, ok := seeFixtures[name]
	if !ok {
		if name != "" {
			t.Errorf("see: no fixture %q", name)
		}
		for _, n := range slices.Sorted(maps.Keys(seeFixtures)) {
			t.Logf("see: %s: %s", n, seeFixtures[n].about)
		}
		if name != "" {
			t.FailNow()
		}
		t.Skip("set CONTR_SEE to a fixture (scripts/see.sh does)")
	}
	themes := []string{"dark", "light"}
	switch th := os.Getenv("CONTR_SEE_THEME"); th {
	case "", "both":
	case "dark", "light":
		themes = []string{th}
	default:
		t.Fatalf("see: theme %q: want dark, light or both", th)
	}
	var press []string
	for _, l := range strings.Split(os.Getenv("CONTR_SEE_PRESS"), "\n") {
		if l != "" {
			press = append(press, l)
		}
	}

	h := newHarness(t)
	out := filepath.Join(h.root, "tmp", "see")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	f.setup(t, h)
	for _, th := range themes {
		h.theme(t, th)
		c := h.launch(t)
		c.waitLabel(t, f.ready)
		c.settle(t)
		for _, l := range press {
			c.press(t, l)
		}
		if len(press) > 0 {
			c.settle(t)
		}
		// The capture can lag the element tree by a frame or more: wait for a repaint.
		time.Sleep(time.Second)
		c.settle(t)
		base := filepath.Join(out, name+"-"+th)
		c.capture(t, base+".png")
		c.dump(t, base+".txt")
		t.Logf("see: %s.png", base)
		t.Logf("see: %s.txt", base)
	}
}
