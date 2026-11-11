//go:build e2e

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/session"
	"github.com/contursofran/contr/internal/updated"
)

// seeFixture is a state of the app that scripts/see.sh can show.
type seeFixture struct {
	about string
	setup func(t *testing.T, h *harness)
	ready string // a label prefix that shows once the state is on screen
	// press are the labels pressed when CONTR_SEE_PRESS gives none.
	press []string
	// hooks are the steps that `do:<name>` runs between the presses.
	hooks map[string]func(t *testing.T, h *harness)
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
	"projects": {
		about: "the workers fixture and a second project, demo-rune, in the recent list; press Open projects",
		setup: func(t *testing.T, h *harness) {
			seeWorkers(t, h)
			h.recentProject(t, "demo-rune")
		},
		ready: "Sidebar",
	},
	"empty-project": {
		about: "the workers fixture and a stored project, site, with nothing open (D291); press its row, or right-click it for its menu",
		setup: func(t *testing.T, h *harness) {
			seeWorkers(t, h)
			h.storedProject(t, "site")
		},
		ready: "site, ",
	},
	"workers": {
		about: "project demo: an orchestrator and two builders, one working",
		setup: seeWorkers,
		ready: "lead, ",
	},
	"questions": {
		about: "the workers fixture with a blocked worker (permission menu), two questions and a to-do for the owner",
		setup: func(t *testing.T, h *harness) {
			repo := seeWorkersRepo(t, h)
			h.spawn(t, agents.Spec{Name: "b252-s1-cleanup", Profile: "claude-blocked", Role: "builder", Issue: "252", Project: "demo", Worktree: repo})
			h.owner(t, `{"questions":[
{"id":"D1","project":"demo","title":"Which database for the cache?","question":"The cache needs a store. Redis is fast but one more service; sqlite is already in the app. Which one should it use?","options":["sqlite","redis","files"],"recommended":0,"asked":"2026-11-03T10:00:00Z"},
{"id":"D2","project":"demo","title":"Keep the old outline ticks?","question":"The new outline replaces the pin bar. Remove the old ticks now, or after one release?","options":["now","after one release"],"recommended":0,"asked":"2026-11-03T10:05:00Z"}],
"todos":[{"id":"T1","project":"demo","kind":"do","text":"Test the notch drag on the Mac","pinned":"2026-11-03T10:02:00Z"}],
"next_question":3,"next_todo":2}`)
		},
		ready: "lead, ",
	},
	"todo": {
		about: "the workers fixture with one to-do that holds a command in backticks (#498)",
		setup: func(t *testing.T, h *harness) {
			seeWorkers(t, h)
			h.owner(t, `{"questions":[],
"todos":[{"id":"T1","project":"demo","kind":"do","text":"Run `+"`go run ./tmp/usagecheck`"+` and paste the plan rows it prints into the issue","pinned":"2026-11-03T10:02:00Z"}],
"next_question":1,"next_todo":2}`)
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
	"changes": {
		about: "the workers fixture with changes in demo (two folders, the root, a deleted file), so the files panel opens on Changed, and lead's context at 86%, so the Context line says compacts soon (D264)",
		setup: func(t *testing.T, h *harness) {
			seeChanges(t, seeWorkersRepo(t, h))
			h.labelTerminal(t, "lead", map[string]string{agents.LabelContext: "172000", agents.LabelContextLimit: "200000"})
		},
		ready: "lead, ",
	},
	"changes-long": {
		about: "the session fixture with the changes of the changes fixture and 18 nested worktrees under .claude/worktrees, which git lists as untracked folders: the Changed list is longer than the panel (#693)",
		setup: func(t *testing.T, h *harness) {
			repo := seeSessionRepo(t, h)
			seeChanges(t, repo)
			seeNestedWorktrees(t, repo, 18)
		},
		ready: "Todos",
	},
	"updated": {
		about: "the first start after an update: the Updated to pill with What's new open (D209)",
		setup: func(t *testing.T, h *harness) {
			n := updated.Note{Version: settings.Version, From: "0.0.9", InstalledAt: time.Now().Add(-3 * time.Minute),
				Notes: "## " + settings.Version + "\n- What's new opens after an update\n- Faster tab switching\n- Light theme fixes"}
			if err := updated.Save(updated.Path(filepath.Join(h.dir, "state")), n); err != nil {
				t.Fatal(err)
			}
		},
		ready: app.UpdatedPrefix,
	},
	"setup-missing": {
		about: "Settings on the Setup tab (D278): a fake claude lists the contr plugin as disabled, a fake gh is signed out and the meter has no data, so three required steps are left",
		setup: seeSetupMissing,
		ready: "Sidebar",
		press: []string{"Settings"},
	},
	"multi": {
		about: "three projects (demo, api, site): demo has an orchestrator and a builder, the others one builder each",
		setup: func(t *testing.T, h *harness) {
			demo := h.repo(t, "demo", "main")
			api := h.repo(t, "api", "main")
			site := h.repo(t, "site", "main")
			h.spawn(t,
				agents.Spec{Name: "lead", Profile: "claude-idle", Role: agents.RoleOrchestrator, Project: "demo", Worktree: demo},
				agents.Spec{Name: "b201-s1-notch", Profile: "claude-working", Role: "builder", Issue: "201", Project: "demo", Worktree: demo},
				agents.Spec{Name: "b310-s1-routes", Profile: "claude-idle", Role: "builder", Issue: "310", Project: "api", Worktree: api},
				agents.Spec{Name: "b411-s1-footer", Profile: "claude-working", Role: "builder", Issue: "411", Project: "site", Worktree: site},
			)
		},
		ready: "lead, ",
	},
	"oldformat": {
		about: "the workers fixture with an agents.json in the old format (presets)",
		setup: func(t *testing.T, h *harness) {
			seeWorkers(t, h)
			if err := os.WriteFile(filepath.Join(h.dir, "agents.json"), []byte(`{"version":1,"presets":[{"name":"x"}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		ready: "lead, ",
	},
	"nongit": {
		about: "the workers fixture; `+ new` picks the folder notes, which is outside git: press New orchestrator (D233)",
		setup: func(t *testing.T, h *harness) { seeNonGit(t, h) },
		ready: "lead, ",
	},
	"orchestrate": {
		about: "the workers fixture with three pins: lead does not orchestrate, so the title bar has the Orchestrate pill left of the diff summary, and the Chat | Terminal switch sits alone 10px from the terminal's right side, over a short outline column (D234, D253); press Orchestrate. Open",
		setup: func(t *testing.T, h *harness) {
			seeWorkers(t, h)
			h.owner(t, seePins(3))
		},
		ready: "lead, ",
	},
	"orchestrating": {
		about: "project demo: lead orchestrates (the Stop hook's label) with two live builders, so the title bar pill says Orchestrating · 2 workers, or shows the mascot and 2 under CONTR_E2E_WIDTH=1000 (D234, D253); 40 pins make a full outline column, which the switch leaves at 10px while the column's top is under the switch (D237, D253)",
		setup: func(t *testing.T, h *harness) {
			seeOrchestrating(t, h)
			h.owner(t, seePins(40))
		},
		ready: "lead, ",
	},
	"chat": {
		about: "the orchestrator lead in Chat mode (D246, D252): two prompts, an answer with a table, work groups with tool calls and worker notes, a background task, a failed one outside the fold, a message of another session, the team row of 4 live workers (working, idle, blocked, error; an ended fifth is left out, as in the pill) with the running call at its right end (D259, D260), and the composer; the last entry clears the dock",
		setup: seeChat,
		ready: "lead, ",
		press: []string{"Chat"},
	},
	"chat-composer": {
		about: "the chat fixture with text in the composer card (D271): the team row above the card, two lines of text, the round Send button in ink-100 in line with the last line, and the footer under the card: Working with what lead does at the left, the model and context at the right (D276)",
		setup: seeChat,
		ready: "lead, ",
		press: []string{"Chat", "type:Merge #611 first, then tell b-632 to merge main and run the review on its exact head. Then tell me when the queue is empty and all the checks pass."},
	},
	"chat-idle": {
		about: "the chat fixture with lead idle (D276): no round button in the composer card, and the keys at the left of the footer under it",
		setup: seeChatIdle,
		ready: "lead, ",
		press: []string{"Chat"},
	},
	"chat-next": {
		about: "the chat fixture with lead idle and a suggested next prompt in its terminal's input line (D281): the composer shows it in place of the placeholder",
		setup: func(t *testing.T, h *harness) { seeChatAs(t, h, "claude-next") },
		ready: "lead, ",
		press: []string{"Chat"},
	},
	"chat-blocks": {
		about: "the orchestrator lead in Chat mode with an older status board (frozen: the agent's three columns, its answer's time), the live board (state, PR and checks from a fake gh: CI running 2 of 3, CI passed 3 of 3, building, waiting, and a worker that is not found) and a merge queue (merged, CI running, blocked: 1 check failed, a note) (D267)",
		setup: seeChatBlocks,
		ready: "lead, ",
		press: []string{"Chat"},
	},
	"chat-scrolled": {
		about: "the chat fixture scrolled up into the first turn (D266): its prompt pinned at the top, Jump to latest above the dock, and the time once per turn, under each prompt; the long first prompt ends before the questions notch",
		setup: seeChatLong,
		ready: "lead, ",
		press: []string{"Chat", "wheel:-300", "wait:pinned prompt", "wait:Jump to latest"},
	},
	"chat-outline": {
		about: "the chat-scrolled transcript with a turn of each outline tick kind (D270): remarks (decision, warning, done, not verified, and a note that gets none), a pin and a question whose texts come from the owner store, the failed task outside the fold, the owner's prompts; then, while the owner looks at b-630, a turn comes with a prompt, a done remark and a new decision, so back on lead the column shows the three new (ink-100, count 3), and the open question and the new decision blocked",
		setup: seeChatOutline,
		ready: "lead, ",
		press: []string{"Chat", "b-630, ", "do:away", "lead, ", "wait:outline new count"},
		hooks: map[string]func(t *testing.T, h *harness){"away": seeChatOutlineAway},
	},
	"chat-images": {
		about: "the chat fixture with two more prompts (D269): one with two generated images as thumbnails under the bubble, one with images whose bytes are not in the transcript as the chip \"2 images\"",
		setup: seeChatImages,
		ready: "lead, ",
		press: []string{"Chat", "wait:image 2"},
	},
	"chat-flow": {
		about: "the orchestrator lead in Chat mode with an answer that holds a contr-flow block, drawn as a flow diagram (D284): six boxes, edge labels, a loop back under the boxes, review waiting and merge done",
		setup: seeChatFlow,
		ready: "lead, ",
		press: []string{"Chat", "wait:Flow diagram"},
	},
	"chat-seq": {
		about: "the orchestrator lead in Chat mode with an answer that holds a contr-seq block, drawn as a sequence diagram (D287): three actors with lifelines, messages, two dashed replies and a message to itself",
		setup: seeChatSeq,
		ready: "lead, ",
		press: []string{"Chat", "wait:Sequence diagram"},
	},
	"chat-tools": {
		about: "the chat fixture with a turn of mixed tool calls, its work group open (D274): a call line of each kind with its icon (skill, Slack connector, contr plugin, Linear MCP and Notion connector with their brand marks, an unknown MCP server with the connector icon, agent, fetch, Bash, Read, Grep), a tool of no kind with an empty slot, the label and key argument of each, and the Bash call open on its detail",
		setup: seeChatTools,
		ready: "lead, ",
		press: []string{"Chat", "wait:work group 12 tool calls", "work group 12 tool calls", "wait:tool call Grep", "tool call Bash", "wait:call detail Bash"},
	},
	"chat-paths": {
		about: "the chat fixture with \"Look at @app\" typed in the composer (D279): the popup above the card with the folder and files of the repo whose paths hold app, the paths with a part that starts with it first",
		setup: seeChatPaths,
		ready: "lead, ",
		press: []string{"Chat", "type:Look at @app", "wait:@internal/app/"},
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
func seeSession(t *testing.T, h *harness) { seeSessionRepo(t, h) }

// seeSessionRepo is seeSession; it returns the folder of demo.
func seeSessionRepo(t *testing.T, h *harness) string {
	repo := h.repo(t, "demo", "main")
	w, err := h.mgr.Spawn(h.ctx, agents.Spec{Name: "lead", Profile: "claude-working", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo})
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
	return repo
}

// seeChat spawns lead and writes a transcript of an orchestrator session in the scratch home, and
// its path in the session file of lead's terminal, as the kit's hooks do (D243).
func seeChat(t *testing.T, h *harness) { seeChatAs(t, h, "claude-working") }

// seeChatIdle is seeChat with lead idle (D276).
func seeChatIdle(t *testing.T, h *harness) { seeChatAs(t, h, "claude-idle") }

// seeChatAs is seeChat with lead in profile.
func seeChatAs(t *testing.T, h *harness, profile string) {
	repo := h.repo(t, "demo", "main")
	w, err := h.mgr.Spawn(h.ctx, agents.Spec{Name: "lead", Profile: profile, Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo})
	if err != nil {
		t.Fatalf("spawn lead: %v", err)
	}
	from := map[string]string{}
	for _, sp := range []agents.Spec{
		{Name: "b-630", Profile: "claude-working", Role: "builder", Issue: "609", Project: "demo", Worktree: repo, Parent: w.Address},
		{Name: "b-631", Profile: "claude-idle", Role: "builder", Issue: "611", Project: "demo", Worktree: repo, Parent: w.Address},
		{Name: "b-632", Profile: "claude-blocked", Role: "builder", Issue: "613", Project: "demo", Worktree: repo, Parent: w.Address},
		{Name: "b-633", Profile: "claude-error", Role: "builder", Issue: "615", Project: "demo", Worktree: repo, Parent: w.Address},
		{Name: "b-634", Profile: "ended", Role: "builder", Issue: "617", Project: "demo", Worktree: repo, Parent: w.Address},
	} {
		b, err := h.mgr.Spawn(h.ctx, sp)
		if err != nil {
			t.Fatalf("spawn %s: %v", sp.Name, err)
		}
		from[sp.Name] = b.Address + " " + sp.Name
	}
	path := filepath.Join(h.home, ".claude", "projects", "demo", "chat.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(seeTranscript(from)), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &session.Session{UpdatedAt: uint64(time.Now().Unix())}
	s.SetTranscript("chat", path)
	if err := session.Write(filepath.Join(h.dir, "state"), w.Address, s); err != nil {
		t.Fatal(err)
	}
}

// seeChatPaths is seeChat with the files of seeChanges in lead's repo and an untracked one whose
// name holds "app" in the middle.
func seeChatPaths(t *testing.T, h *harness) {
	t.Helper()
	seeChat(t, h)
	repo := filepath.Join(h.dir, "demo")
	seeChanges(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "wrapper.go"), []byte("package demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seeChatLong is seeChat with two more turns, so the transcript is longer than the window.
func seeChatLong(t *testing.T, h *harness) {
	t.Helper()
	seeChat(t, h)
	path := filepath.Join(h.home, ".claude", "projects", "demo", "chat.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	now := time.Now()
	line := func(uuid string, ago time.Duration, typ string, content any) string {
		b, _ := json.Marshal(map[string]any{"type": typ, "uuid": uuid, "timestamp": now.Add(-ago).UTC().Format(time.RFC3339Nano),
			"sessionId": "chat", "message": map[string]any{"role": typ, "content": content}})
		return string(b) + "\n"
	}
	text := func(s string) []any { return []any{map[string]any{"type": "text", "text": s}} }
	for _, l := range []string{
		line("x1", 13*time.Second, "user", "review #633 on its head and merge it when the checks pass"),
		line("x2", 12*time.Second, "assistant", text("I read the diff of #633. It changes the switch only:\n\n"+
			"- `internal/app/chatview/view.go`: the switch is solid now, on `float-solid`.\n"+
			"- `internal/app/chatview/corner.go`: the switch moves clear of the outline column.\n"+
			"- `docs/spec/ChatView.md`: the Switch table.\n\n"+
			"The tests cover each change, and the sheets match the spec in Dark and Light.")),
		line("x3", 11*time.Second, "assistant", text("The checks passed on the head. I merge it now, then b-632 merges `main` before its review.")),
		line("x6", 9*time.Second, "user", "what is left on the board?"),
		line("x7", 8*time.Second, "assistant", text("Three slices are open:\n\n"+
			"| Issue | Slice | State |\n|---|---|---|\n| #609 | the chat model | b-630 builds |\n| #613 | the work group | b-632 waits on `main` |\n| #657 | the pinned prompt | no planner yet |\n\n"+
			"#609 and #613 touch `internal/app/chatview/model.go`, so I merge #609 first. #657 needs a plan before a builder starts.")),
		line("x4", 5*time.Second, "user", "then start a planner for #657"),
		line("x5", 3*time.Second, "assistant", text("The planner for #657 is out as planner-657. It reads the issue, the code it touches and the spec, "+
			"then writes the plan in the issue. I tell you when the plan is ready.")),
	} {
		if _, err := f.WriteString(l); err != nil {
			t.Fatal(err)
		}
	}
}

// seeChatOutline is seeChatLong with a turn that has each kind of outline tick (D270), and the
// owner store with the pin and the question that turn made.
func seeChatOutline(t *testing.T, h *harness) {
	t.Helper()
	seeChatLong(t, h)
	now := time.Now()
	h.owner(t, fmt.Sprintf(`{"questions":[{"id":"D1","project":"demo","question":"Which name for the release: 0.42 or Notch?","asked":%q}],`+
		`"todos":[{"id":"T1","project":"demo","kind":"do","text":"Test the build of 0.42 on your Mac","pinned":%q}],"next_question":2,"next_todo":2}`,
		now.Add(-2*time.Second).UTC().Format(time.RFC3339Nano), now.Add(-2*time.Second).UTC().Format(time.RFC3339Nano)))
	seeAppend(t, h, now.Add(-2*time.Second),
		seeLine("o1", "assistant", seeText("The switch slice is merged.\n\n> [!DONE]\n> PR #633 is merged and `main` is green.\n\n"+
			"> [!WARNING]\n> CI is slow on the hc tests: 9 minutes a run.\n\n> [!NOTE]\n> b-632 rebased on `main`.\n\n"+
			"> [!UNVERIFIED]\n> The e2e run of the switch on a locked screen.")),
		seeLine("o2", "assistant", []any{map[string]any{"type": "tool_use", "id": "tp1", "name": "mcp__plugin_contr_contr__pin_for_owner", "input": map[string]any{"text": "Test the build of 0.42 on your Mac"}}}),
		seeLine("o3", "user", []any{map[string]any{"type": "tool_result", "tool_use_id": "tp1", "content": "T1"}}),
		seeLine("o4", "assistant", []any{map[string]any{"type": "tool_use", "id": "tq1", "name": "mcp__plugin_contr_contr__ask_owner", "input": map[string]any{"question": "Which name for the release: 0.42 or Notch?"}}}),
		seeLine("o5", "user", []any{map[string]any{"type": "tool_result", "tool_use_id": "tq1", "content": "D1"}}),
		seeLine("o6", "assistant", seeText("I pinned the build test and asked for the release name.")),
	)
}

// seeChatOutlineAway is the turn that comes while the owner looks at another worker: a done
// remark and a decision that waits.
func seeChatOutlineAway(t *testing.T, h *harness) {
	t.Helper()
	time.Sleep(1500 * time.Millisecond) // after the owner left lead
	seeAppend(t, h, time.Now(),
		seeLine("n1", "user", "merge #634 when its checks pass"),
		seeLine("n2", "assistant", seeText("#634 is merged.\n\n> [!DONE]\n> PR #634 is merged.\n\n"+
			"> [!DECISION]\n> Ship the notch now, before the release, or keep it behind the switch until you test the glass on a light wallpaper?")),
	)
}

// seeChatTools is seeChat with a turn of a done call of each kind (D274).
func seeChatTools(t *testing.T, h *harness) {
	t.Helper()
	seeChat(t, h)
	lines := []seeLineT{seeLine("m0", "user", "find where the call line draws and tell the team")}
	for i, c := range []struct {
		name  string
		input map[string]any
	}{
		{"Skill", map[string]any{"skill": "contr:chat-writing"}},
		{"mcp__claude_ai_Slack__slack_send_message", map[string]any{"channel": "#dev", "text": "the call line has icons"}},
		{"mcp__plugin_contr_contr__send", map[string]any{"to": "b-709", "body": "merge main"}},
		{"mcp__linear__list_issues", map[string]any{"query": "chat rows"}},
		{"mcp__claude_ai_Notion__notion_search", map[string]any{"query": "chat spec"}},
		{"mcp__acme__list_things", map[string]any{"query": "rows"}},
		{"Task", map[string]any{"subagent_type": "Explore", "description": "Find the call line"}},
		{"WebFetch", map[string]any{"url": "https://example.com/docs/chat"}},
		{"Bash", map[string]any{"command": "go test ./internal/app/chatview", "description": "Run the chat tests"}},
		{"Read", map[string]any{"file_path": "internal/app/chatview/view.go"}},
		{"Grep", map[string]any{"pattern": "callLine"}},
		{"TodoWrite", map[string]any{"todos": []any{}}},
	} {
		id := fmt.Sprintf("tt%d", i)
		lines = append(lines,
			seeLine("m"+id, "assistant", []any{map[string]any{"type": "tool_use", "id": id, "name": c.name, "input": c.input}}),
			seeLine("r"+id, "user", []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok"}}))
	}
	lines = append(lines, seeLine("m9", "assistant", seeText("The call line draws in `internal/app/chatview/view.go`. I told b-709.")))
	seeAppend(t, h, time.Now(), lines...)
}

// seeText is the content of a text of Claude.
func seeText(s string) []any { return []any{map[string]any{"type": "text", "text": s}} }

// seeLine is a transcript line; seeAppend gives it its time.
type seeLineT struct {
	uuid, typ string
	content   any
}

func seeLine(uuid, typ string, content any) seeLineT { return seeLineT{uuid, typ, content} }

// seeAppend appends lines to the chat fixture's transcript, a second apart, the last at end.
func seeAppend(t *testing.T, h *harness, end time.Time, lines ...seeLineT) {
	t.Helper()
	path := filepath.Join(h.home, ".claude", "projects", "demo", "chat.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i, l := range lines {
		at := end.Add(-time.Duration(len(lines)-1-i) * 100 * time.Millisecond)
		b, err := json.Marshal(map[string]any{"type": l.typ, "uuid": l.uuid, "timestamp": at.UTC().Format(time.RFC3339Nano),
			"sessionId": "chat", "message": map[string]any{"role": l.typ, "content": l.content}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

// seeChatImages is seeChat with a prompt of two generated images and a prompt of two images
// without bytes (D269).
func seeChatImages(t *testing.T, h *harness) {
	t.Helper()
	seeChat(t, h)
	path := filepath.Join(h.home, ".claude", "projects", "demo", "chat.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	now := time.Now()
	shot := func(w, h int) map[string]any {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for x := range w {
			for y := range h {
				v := uint8(60 + 120*x/w)
				if y > h/3 && y < h/2 {
					v = 230
				}
				img.Set(x, y, color.RGBA{v, v, v, 255})
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		return map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": base64.StdEncoding.EncodeToString(b.Bytes())}}
	}
	noBytes := map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": "https://example.invalid/old.png"}}
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	line := func(uuid string, ago time.Duration, typ string, content any) string {
		b, _ := json.Marshal(map[string]any{"type": typ, "uuid": uuid, "timestamp": now.Add(-ago).UTC().Format(time.RFC3339Nano),
			"sessionId": "chat", "message": map[string]any{"role": typ, "content": content}})
		return string(b) + "\n"
	}
	for _, l := range []string{
		line("i1", 6*time.Second, "user", []any{text("[Image #1] [Image #2] The row clips here, see both shots."), shot(480, 300), shot(180, 320)}),
		line("i2", 5*time.Second, "assistant", []any{text("I see it: the time runs into the badge.")}),
		line("i3", 4*time.Second, "user", []any{text("and these, from the old build"), noBytes, noBytes}),
	} {
		if _, err := f.WriteString(l); err != nil {
			t.Fatal(err)
		}
	}
}

// seeTranscript is an orchestrator session as Claude Code writes it: hidden lines (meta, the
// playbook, hook output, a reminder), prompts, texts, tool calls and their results, hc notes of
// workers, a background task and a message of another session.
// from gives the hc sender ("ADDR NAME") of each worker. The session ends a little before now,
// so the live line shows a short time.
func seeTranscript(from map[string]string) string {
	n := 0
	start := time.Now().Add(-75 * time.Second)
	at := func(sec int) string {
		return start.Add(time.Duration(sec) * time.Second).UTC().Format(time.RFC3339Nano)
	}
	line := func(typ string, sec int, extra map[string]any) string {
		n++
		m := map[string]any{"type": typ, "uuid": fmt.Sprintf("u%d", n), "timestamp": at(sec), "sessionId": "chat"}
		maps.Copy(m, extra)
		b, _ := json.Marshal(m)
		return string(b) + "\n"
	}
	user := func(sec int, content any) string {
		return line("user", sec, map[string]any{"message": map[string]any{"role": "user", "content": content}})
	}
	said := func(sec int, text string) string {
		return line("assistant", sec, map[string]any{"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}})
	}
	call := func(sec int, id, name string, input map[string]any) string {
		return line("assistant", sec, map[string]any{"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}}})
	}
	result := func(sec int, id, text string, isErr bool) string {
		return user(sec, []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": text, "is_error": isErr}})
	}
	note := func(sec int, from, text string) string {
		return user(sec, `<hc from="`+from+`">`+text+`</hc>`)
	}
	var b strings.Builder
	for _, l := range []string{
		line("user", 0, map[string]any{"isMeta": true, "message": map[string]any{"role": "user", "content": "Caveat: The messages below were generated by the user while running local commands."}}),
		line("attachment", 0, map[string]any{"attachment": map[string]any{"type": "hook_additional_context", "hookEvent": "SessionStart", "content": []any{"contr: you are the coordinator of this project; the playbook follows."}}}),
		user(1, "start builders for 609, 611 and 613; each one gets its own worktree and branch, and tell me when a worker needs me or a PR is ready for review"),
		user(1, "<user-prompt-submit-hook>3 workers live, 0 questions open</user-prompt-submit-hook>"),
		line("assistant", 2, map[string]any{"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "plan the three slices"}}}}),
		call(3, "t1", "mcp__plugin_contr_contr__spawn", map[string]any{"name": "b-630"}),
		result(5, "t1", "spawned b-630", false),
		call(6, "t2", "mcp__plugin_contr_contr__spawn", map[string]any{"name": "b-631"}),
		result(8, "t2", "spawned b-631", false),
		call(9, "t3", "mcp__plugin_contr_contr__spawn", map[string]any{"name": "b-632"}),
		result(11, "t3", "spawned b-632", false),
		said(12, "Three builders are out:\n\n| Worker | Issue | Slice |\n|---|---|---|\n| b-630 | #609 | the chat model |\n| b-631 | #611 | the switch |\n| b-632 | #613 | the work group |\n\nI wait for their reports."),
		user(40, "how is it going?"),
		call(41, "t4", "mcp__plugin_contr_contr__list", map[string]any{}),
		result(42, "t4", "b-630 working\nb-631 done\nb-632 working", false),
		note(50, from["b-631"], "b-631: done, PR #633, check ok; RESULT.md in /tmp/demo"),
		call(51, "t5", "Bash", map[string]any{"command": "gh pr view 633 --json statusCheckRollup"}),
		result(53, "t5", "SUCCESS", false),
		call(54, "t6", "Read", map[string]any{"file_path": "/tmp/demo/docs/DECISIONS.md"}),
		result(54, "t6", "| D246 | …", false),
		call(55, "t7", "Bash", map[string]any{"command": "git log --oneline -3"}),
		result(56, "t7", "fatal: not a git repository", true),
		note(57, from["b-630"], "b-630: working, the model passes its tests"),
		note(58, from["b-632"], "ended its turn without a report. Last words: Waiting for the full-suite run to finish."),
		user(58, "<task-notification>\n<task-id>bx1</task-id>\n<tool-use-id>t9</tool-use-id>\n<output-file>/tmp/demo/bx1.output</output-file>\n<status>completed</status>\n<summary>Background command \"go test ./...\" completed (exit code 0)</summary>\n</task-notification>"),
		user(58, "Another Claude session sent a message:\n<agent-message from=\"planner-657\">The plan for #657 is ready.\nRead it in the issue.</agent-message>"),
		user(58, "<system-reminder>\nThe user hasn't heard from you in a while.\n</system-reminder>"),
		user(58, "<task-notification>\n<task-id>bx2</task-id>\n<tool-use-id>t10</tool-use-id>\n<output-file>/tmp/demo/bx2.output</output-file>\n<status>failed</status>\n<summary>Background command \"go vet ./...\" failed with exit code 1</summary>\n</task-notification>"),
		said(59, "b-631 is done: PR `#633` is open and its check passed. b-632 stopped without a report; I asked it to report when the run ends.\n\nNext I review **#633** on its head:\n\n```sh\ngh pr diff 633\n```"),
		call(60, "t8", "Bash", map[string]any{"command": "gh pr diff 633"}),
	} {
		b.WriteString(l)
	}
	return b.String()
}

// recentProject puts a project that is not open in the recent list of the
// scratch projects file.
func (h *harness) recentProject(t *testing.T, name string) {
	t.Helper()
	dir := filepath.Join(h.home, "Library", "Application Support", "contr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.home, "code", name)
	doc, err := json.Marshal(map[string]any{"version": 1, "recent": []map[string]any{
		{"dir": filepath.Join(path, ".git"), "path": path, "at": time.Now().Add(-10 * time.Minute)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "projects.json"), doc, 0o600); err != nil {
		t.Fatal(err)
	}
}

// storedProject makes the git repo name in the scratch folder and stores it as an
// added folder with no terminal: an empty project (D116).
func (h *harness) storedProject(t *testing.T, name string) {
	t.Helper()
	dir := filepath.Join(h.home, "Library", "Application Support", "contr")
	path := h.repo(t, name, "main")
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc, err := json.Marshal(map[string]any{"version": 1, "order": []string{filepath.Join(path, ".git")},
		"folders": map[string]string{filepath.Join(path, ".git"): path}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "projects.json"), doc, 0o600); err != nil {
		t.Fatal(err)
	}
}

func seeWorkers(t *testing.T, h *harness) { seeWorkersRepo(t, h) }

// seeNonGit is the workers fixture with `+ new` set to pick a folder outside
// git, and a fake orchestrator profile, so no real tool starts. It returns
// the folder.
func seeNonGit(t *testing.T, h *harness) string {
	seeWorkers(t, h)
	dir := filepath.Join(h.home, "code", "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"profiles":{"orchestrator":{"argv":["sh","-c","echo lead; sleep 600"]}}}`
	if err := os.WriteFile(filepath.Join(h.dir, "agents.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTR_E2E_PICK_FOLDER", dir)
	return dir
}

// seePins is the owner file with n to-dos of project demo pinned for the owner: the ticks of
// lead's outline column.
func seePins(n int) string {
	var todos []string
	for i := 1; i <= n; i++ {
		todos = append(todos, fmt.Sprintf(`{"id":"T%d","project":"demo","kind":"do","text":"Pinned message %d","pinned":"2026-11-03T10:%02d:00Z"}`, i, i, i%60))
	}
	return fmt.Sprintf(`{"questions":[],"todos":[%s],"next_question":1,"next_todo":%d}`, strings.Join(todos, ","), n+1)
}

// seeOrchestrating spawns lead with the orchestrating label and two builders whose parent is lead.
func seeOrchestrating(t *testing.T, h *harness) {
	repo := h.repo(t, "demo", "main")
	lead, err := h.mgr.Spawn(h.ctx, agents.Spec{Name: "lead", Profile: "claude-idle", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo})
	if err != nil {
		t.Fatalf("spawn lead: %v", err)
	}
	h.labelTerminal(t, "lead", map[string]string{agents.LabelOrchestrating: "1"})
	h.spawn(t,
		agents.Spec{Name: "b201-s1-notch", Profile: "claude-working", Role: "builder", Issue: "201", Project: "demo", Worktree: repo, Parent: lead.Address},
		agents.Spec{Name: "b202-s1-outline", Profile: "claude-idle", Role: "builder", Issue: "202", Project: "demo", Worktree: repo, Parent: lead.Address},
	)
}

// seeWorkersRepo spawns the workers fixture and returns its repository.
func seeWorkersRepo(t *testing.T, h *harness) string {
	repo := h.repo(t, "demo", "main")
	h.spawn(t,
		agents.Spec{Name: "lead", Profile: "claude-idle", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo},
		agents.Spec{Name: "b201-s1-notch", Profile: "claude-working", Role: "builder", Issue: "201", Project: "demo", Worktree: repo},
		agents.Spec{Name: "b202-s1-outline", Profile: "claude-idle", Role: "builder", Issue: "202", Project: "demo", Worktree: repo},
	)
	return repo
}

// wheel scrolls the element with this label by dy points (a number), at its center, as a
// touchpad does. It waits first until the list in the element was laid out and is at its end:
// its rows stay the same for two looks and no Jump to latest shows.
func (c *client) wheel(t *testing.T, label, dy string) {
	t.Helper()
	d, err := strconv.ParseFloat(dy, 64)
	if err != nil {
		t.Fatalf("wheel %q: %v", dy, err)
	}
	b := c.restingList(t, label)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "wheel", "x": b.X + b.Width/2, "y": b.Y + b.Height/2, "dx": 0, "dy": d, "precise": true}}})
	c.settle(t)
}

// wheelAt scrolls the element with this label by dy points at its center, as a touchpad does,
// without waiting for a list to rest.
func (c *client) wheelAt(t *testing.T, label, dy string) {
	t.Helper()
	d, err := strconv.ParseFloat(dy, 64)
	if err != nil {
		t.Fatalf("scroll %q: %v", dy, err)
	}
	b := c.boundsOf(t, label)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "wheel", "x": b.X + b.Width/2, "y": b.Y + b.Height/2, "dx": 0, "dy": d, "precise": true}}})
	c.settle(t)
}

// restingList waits until the element with this label has rows that stay the same for two looks
// 300 ms apart, and no Jump to latest shows: the list was laid out and is at its end. It returns
// the bounds of the element.
func (c *client) restingList(t *testing.T, label string) rect {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	last := ""
	for {
		nodes := c.fullNodes(t)
		i := slices.IndexFunc(nodes, func(n fullNode) bool { return n.Label == label })
		var rows strings.Builder
		jump := false
		for _, n := range nodes {
			switch {
			case n.Role == "listItem":
				fmt.Fprintf(&rows, "%v@%v,%v;", n.Key, n.Bounds.Y, n.Bounds.Height)
			case strings.HasPrefix(n.Label, "Jump to latest"):
				jump = true
			}
		}
		if i >= 0 && !jump && rows.Len() > 0 && rows.String() == last {
			return nodes[i].Bounds
		}
		if time.Now().After(deadline) {
			t.Fatalf("the list in %q did not come to rest at its end (found %v, jump %v)", label, i >= 0, jump)
		}
		last = rows.String()
		time.Sleep(300 * time.Millisecond)
	}
}

// seeChanges makes the changes of the changes fixture in repo: a committed file
// deleted, edits in internal/app, a new file in internal/hc, an edit at the root.
func seeChanges(t *testing.T, repo string) {
	write := func(rel, text string) {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/app/panel.go", "package app\n")
	write("HANDOFF.md", "one\ntwo\nthree\n")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", "base")
	write("internal/app/panel.go", "package app\n\n// Panel draws a panel.\nfunc Panel() {}\n")
	write("internal/app/tabs.go", "package app\n\nfunc Tabs() {}\n")
	write("internal/hc/events.go", "package hc\n\nfunc Events() {}\n")
	write("README.md", "# demo\n\nChanged.\n")
	if err := os.Remove(filepath.Join(repo, "HANDOFF.md")); err != nil {
		t.Fatal(err)
	}
}

// seeNestedWorktrees makes n git repos under .claude/worktrees in repo, as an
// agent's nested worktrees: git lists each as one untracked folder whose path
// ends with "/" (#693).
func seeNestedWorktrees(t *testing.T, repo string, n int) {
	for i := range n {
		dir := filepath.Join(repo, ".claude", "worktrees", fmt.Sprintf("agent-a004bd4%02d7f1c9e5", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSee is scripts/see.sh (#457, D178). It starts the fixture CONTR_SEE
// names, in the themes of CONTR_SEE_THEME (dark, light or both), presses the
// labels of CONTR_SEE_PRESS (one per line, `type:<text>` types text, `wheel:<dy>`
// scrolls the chat by dy points once it rests at its end, `scroll:<label>:<dy>` scrolls the
// element with that label by dy points, `wait:<label>` fails the run when no
// element with that label shows in 20 s) in order, and writes
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
	if len(press) == 0 {
		press = f.press
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
		for i, l := range press {
			if text, ok := strings.CutPrefix(l, "type:"); ok { // type into the focused field
				c.text(t, text)
			} else if dy, ok := strings.CutPrefix(l, "wheel:"); ok { // scroll the chat
				c.wheel(t, "chat", dy)
			} else if rest, ok := strings.CutPrefix(l, "scroll:"); ok { // scroll the element with this label
				label, dy, _ := strings.Cut(rest, ":")
				c.wheelAt(t, label, dy)
			} else if label, ok := strings.CutPrefix(l, "wait:"); ok { // fail when it does not show
				c.waitLabel(t, label)
			} else if hook, ok := strings.CutPrefix(l, "do:"); ok { // a step of the fixture
				if f.hooks[hook] == nil {
					t.Fatalf("see: fixture %s has no hook %q", name, hook)
				}
				f.hooks[hook](t, h)
			} else {
				c.press(t, l)
			}
			if i < len(press)-1 { // a menu or a dialog opens before the next press
				c.settle(t)
			}
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

// seeSetupMissing puts a fake claude and a fake gh first on PATH: the contr plugin is disabled
// and gh is signed out. The scratch state folder has no meter data.
func seeSetupMissing(t *testing.T, h *harness) {
	bin := filepath.Join(h.dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fakes := map[string]string{
		"claude": "#!/bin/sh\necho '[{\"id\":\"contr@contr\",\"version\":\"0.1.0\",\"enabled\":false}]'\n",
		"gh":     "#!/bin/sh\necho 'You are not logged into any GitHub hosts.' >&2\nexit 1\n",
	}
	for name, body := range fakes {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.env = append(h.env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
