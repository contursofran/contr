//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
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
	"chat-scrolled": {
		about: "the chat fixture scrolled up into the first turn (D266): its prompt pinned at the top, Jump to latest above the dock, and the time once per turn, under each prompt; the long first prompt ends before the questions notch",
		setup: seeChatLong,
		ready: "lead, ",
		press: []string{"Chat", "wheel:-300", "wait:pinned prompt", "wait:Jump to latest"},
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
}

// seeChat spawns lead and writes a transcript of an orchestrator session in the scratch home, and
// its path in the session file of lead's terminal, as the kit's hooks do (D243).
func seeChat(t *testing.T, h *harness) {
	repo := h.repo(t, "demo", "main")
	w, err := h.mgr.Spawn(h.ctx, agents.Spec{Name: "lead", Profile: "claude-working", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo})
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

// TestSee is scripts/see.sh (#457, D178). It starts the fixture CONTR_SEE
// names, in the themes of CONTR_SEE_THEME (dark, light or both), presses the
// labels of CONTR_SEE_PRESS (one per line, `type:<text>` types text, `wheel:<dy>`
// scrolls the chat by dy points once it rests at its end, `wait:<label>` fails the run when no
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
			} else if label, ok := strings.CutPrefix(l, "wait:"); ok { // fail when it does not show
				c.waitLabel(t, label)
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
