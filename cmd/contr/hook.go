package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/statedir"
	"github.com/contursofran/contr/internal/usage"
)

const hookUsage = `Usage:
  contr hook stop            the kit's Stop hook
  contr hook session-start   the kit's SessionStart hook (startup, resume, clear, compact)
  contr hook post-tool       the kit's PostToolUse hook (todos, artifacts)
  contr hook prompt          the kit's UserPromptSubmit hook

Claude Code runs them and gives them the hook input on stdin.

stop, in a worker (CONTR_COORDINATOR is set): a turn that sent no report
with hc send gets one, "ended its turn without a report. Last words: …",
sent to the coordinator. When Claude Code saved no transcript, it cannot
tell whether the turn sent one: it sends "ended its turn; it may have sent a
report already …" with the last words. It never stops the turn.

stop, in a coordinator (a session that ran /contr:orchestrate, or an hc
terminal with role orchestrator): once the context passes CONTR_SAVE_AT
tokens (default 220000), it keeps the turn going one time and asks the
coordinator to update COORDINATOR-STATE.md before Claude Code compacts.
In any hc session that is not a worker it also labels the terminal with the
context size (contr.context, contr.context-limit) for the status strip, and
with the model and effort (contr.model, contr.effort) for the sidebar row.

session-start, at startup in an hc terminal with role orchestrator (no
transcript yet, so only the role counts), and after a compaction in a
coordinator: prints the playbook, the state file to read (at startup, only
when it exists), the live workers it started, its open owner items and the
flow (contr agents flow), which Claude Code adds to the new context.
After resume and clear it prints nothing more. In an hc terminal it first
prints one line that names the skill contr:chat-writing.

stop, session-start and prompt, in an hc terminal (HC_TERMINAL_ID), save
the transcript_path and session_id of the hook input in the terminal's
session file, for contr's chat view. An error there is logged and does not
fail the hook.

post-tool and prompt run only in an hc terminal (HC_TERMINAL_ID). post-tool
saves the todo list (TodoWrite, TaskCreate, TaskUpdate) and the artifacts
(mcp__claude-artifacts__ calls) in the terminal's session file. prompt asks
Claude to add each request of the owner to its todos with "[you] ". Prompts
that start with "<hc from=" are skipped. Both always exit 0.
`

// saidMax is the most characters of the worker's last words in a report.
const saidMax = 300

// defaultSaveAt is the context size, in tokens, from which a coordinator is
// asked to save its state: below the owner's compaction window of 250000.
const defaultSaveAt = 220000

// defaultCompactAt is the owner's compaction window (autoCompactWindow in
// their Claude Code settings), which the strip shows the context against.
const defaultCompactAt = 250000

// restoreMax is the most characters that session-start prints. Claude Code
// caps a hook's context at 10000 characters.
const restoreMax = 9500

// chatSkillLine is the one line that session-start prints first in an hc terminal: it names the
// kit's skill for answers that the owner reads in contr's chat. internal/transcript hides it from
// the chat by its first words (isPlaybook).
const chatSkillLine = "contr: the owner reads your answers in contr's chat. Before you write one, use the skill contr:chat-writing: it says what the chat shows.\n"

// orchestrateCommand is the kit's coordinator command.
const orchestrateCommand = "contr:orchestrate"

// stateFile is the coordinator's state file in its folder (the playbook).
const stateFile = "COORDINATOR-STATE.md"

// hookInput is the part of Claude Code's hook input that the hooks read.
type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	// LastAssistantMessage is set by newer Claude Code versions.
	LastAssistantMessage string `json:"last_assistant_message"`
	StopHookActive       bool   `json:"stop_hook_active"`
	Source               string `json:"source"` // SessionStart: startup, resume, clear or compact
	// Effort is the session's effort level (low … max). Claude Code leaves it
	// out for a model with no effort setting.
	Effort *struct {
		Level string `json:"level"`
	} `json:"effort"`
}

// hook is one run of a hook command.
type hook struct {
	in     hookInput
	stdout io.Writer
	dial   func(context.Context, func(string) string) (agentsCore, error)
	getenv func(string) string
	// markers is the folder of the per-session "state asked" files.
	markers string
}

// runHook is `contr hook`. It returns the exit code: 0 when it is done or has
// nothing to do, 1 on an error, usage errors included. It never returns 2:
// for a Stop hook, 2 blocks the turn and sends stderr to the model. With 1,
// Claude Code shows the error and the turn ends.
func runHook(args []string, stdin io.Reader, stdout, stderr io.Writer, dial func(context.Context, func(string) string) (agentsCore, error), getenv func(string) string) int {
	if len(args) == 1 && (args[0] == "post-tool" || args[0] == "prompt") {
		return runSessionHook(args[0], stdin, stdout, stderr, getenv, sessionStateDir(getenv), time.Now())
	}
	if len(args) != 1 || (args[0] != "stop" && args[0] != "session-start") {
		_, _ = io.WriteString(stderr, hookUsage)
		return 1
	}
	// Outside hc there are no workers and no coordinators.
	if getenv(agents.EnvCoordinator) == "" && getenv("HC_TERMINAL_ID") == "" {
		return 0
	}
	h := &hook{stdout: stdout, dial: dial, getenv: getenv, markers: filepath.Join(os.TempDir(), "contr-hook")}
	if err := json.NewDecoder(stdin).Decode(&h.in); err != nil {
		fmt.Fprintf(stderr, "contr hook: bad hook input: %v\n", err)
		return 1
	}
	// Before the hook's own work, which can fail: the path is saved anyway.
	saveTranscript(stderr, args[0], sessionStateDir(getenv), getenv("HC_TERMINAL_ID"), h.in.SessionID, h.in.TranscriptPath)
	var err error
	switch {
	case args[0] == "session-start":
		if getenv(statedir.EnvTerminal) != "" {
			_, _ = io.WriteString(stdout, chatSkillLine)
		}
		err = h.sessionStart()
	case getenv(agents.EnvCoordinator) != "":
		err = h.workerStop()
	default:
		err = h.coordinatorStop()
	}
	if err != nil {
		fmt.Fprintf(stderr, "contr hook: %v\n", err)
		return 1
	}
	return 0
}

// workerStop sends the coordinator a report for a turn that sent none. With
// no transcript file (Claude Code saved none), it cannot tell whether the
// turn sent one, so it sends a report that says it may repeat one (D216).
func (h *hook) workerStop() error {
	coordinator := h.getenv(agents.EnvCoordinator)
	turn, err := readTurn(h.in.TranscriptPath)
	noTranscript := errors.Is(err, os.ErrNotExist)
	if err != nil && !noTranscript {
		return err
	}
	if turn.Reported {
		return nil
	}
	said := h.in.LastAssistantMessage
	if strings.TrimSpace(said) == "" {
		said = turn.Said
	}
	text := missedReport(said, h.in.Cwd)
	if noTranscript {
		text = unsureReport(said, h.in.Cwd)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	core, err := h.dial(ctx, h.getenv)
	if err != nil {
		return err
	}
	defer func() { _ = core.Close() }()
	m := agents.New(core, agents.Config{}).WithEnv(h.getenv)
	if err := m.Send(ctx, coordinator, text); err != nil {
		return fmt.Errorf("report to %s: %w", coordinator, err)
	}
	return nil
}

// coordinatorStop asks a coordinator, once per compaction, to save its state
// when the context nears the compaction window. With no transcript file it
// does nothing (D216).
func (h *hook) coordinatorStop() error {
	if h.in.StopHookActive || h.in.TranscriptPath == "" {
		return nil
	}
	tokens, model, err := agents.ContextAndModel(h.in.TranscriptPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	labels := h.modelLabels(model)
	if h.ranOrchestrate() {
		labels[agents.LabelOrchestrating] = "1"
	}
	h.showContext(tokens, labels)
	if tokens < h.saveAt() || h.asked() {
		return nil
	}
	coord, err := h.coordinator()
	if err != nil || !coord {
		return err
	}
	if err := h.markAsked(); err != nil {
		return err
	}
	reason := fmt.Sprintf("contr: the context is at %dk tokens, and Claude Code compacts it soon. "+
		"Update %s now (Ready, Running with worker, issue, worktree and PR, Decisions, Next), "+
		"so you can carry on from it after the compaction. Then end your turn.", tokens/1000, stateFile)
	return json.NewEncoder(h.stdout).Encode(map[string]string{"decision": "block", "reason": reason})
}

// sessionStart gives a coordinator its playbook and state after a compaction.
func (h *hook) sessionStart() error {
	if h.in.Source == "startup" {
		return h.startup()
	}
	if h.in.Source != "" && h.in.Source != "compact" {
		return nil
	}
	h.clearAsked()
	h.showContext(0, nil) // the size before the compaction is stale; the next turn's Stop sets it again
	coord, err := h.coordinator()
	if err != nil || !coord {
		return err
	}
	_, err = io.WriteString(h.stdout, h.restoreText())
	return err
}

// startup gives a new session the playbook when its hc terminal has role
// orchestrator (D202). A new session has no transcript, so only the role
// counts. Nothing is cleared or labeled: the session is new.
func (h *hook) startup() error {
	if h.getenv("HC_TERMINAL_ID") == "" {
		return nil
	}
	orch, err := h.orchestratorRole()
	if err != nil || !orch {
		return err
	}
	_, err = io.WriteString(h.stdout, h.startupText())
	return err
}

// modelLabels are the model and effort labels of the session (D189): the
// short name of the transcript's model, and the effort of the hook input,
// else $CLAUDE_EFFORT. A value that is not known is left out, so the label
// keeps its last value.
func (h *hook) modelLabels(model string) map[string]string {
	labels := map[string]string{}
	if model != "" {
		labels[agents.LabelModel] = usage.ShortModel(model, "")
	}
	effort := h.getenv("CLAUDE_EFFORT")
	if h.in.Effort != nil && h.in.Effort.Level != "" {
		effort = h.in.Effort.Level
	}
	if effort != "" {
		labels[agents.LabelEffort] = effort
	}
	return labels
}

// showContext puts the session's context size on its own hc terminal, for
// the status strip: contr.context (tokens) and contr.context-limit (the
// compaction window). 0 removes contr.context. extra are more labels to set
// in the same call (modelLabels). Best effort: a failure never fails the
// hook.
func (h *hook) showContext(tokens int, extra map[string]string) {
	self := h.getenv("HC_TERMINAL_ID")
	if self == "" {
		return
	}
	labels := map[string]string{"contr.context": ""}
	maps.Copy(labels, extra)
	if tokens > 0 {
		labels["contr.context"] = strconv.Itoa(tokens)
		labels["contr.context-limit"] = strconv.Itoa(h.compactAt())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	core, err := h.dial(ctx, h.getenv)
	if err != nil {
		return
	}
	defer func() { _ = core.Close() }()
	_ = core.Call(ctx, "terminal.label", map[string]any{"terminal": self, "labels": labels}, nil)
}

// compactAt is the window Claude Code compacts at: CLAUDE_CODE_AUTO_COMPACT_WINDOW,
// else defaultCompactAt.
func (h *hook) compactAt() int {
	if n, err := strconv.Atoi(h.getenv("CLAUDE_CODE_AUTO_COMPACT_WINDOW")); err == nil && n > 0 {
		return n
	}
	return defaultCompactAt
}

// saveAt is CONTR_SAVE_AT, else defaultSaveAt.
func (h *hook) saveAt() int {
	if n, err := strconv.Atoi(h.getenv("CONTR_SAVE_AT")); err == nil && n > 0 {
		return n
	}
	return defaultSaveAt
}

func (h *hook) marker() string {
	return filepath.Join(h.markers, filepath.Base(h.in.SessionID)+".asked")
}

func (h *hook) asked() bool {
	if h.in.SessionID == "" {
		return false
	}
	_, err := os.Stat(h.marker())
	return err == nil
}

func (h *hook) markAsked() error {
	if h.in.SessionID == "" {
		return nil
	}
	if err := os.MkdirAll(h.markers, 0o700); err != nil {
		return err
	}
	return os.WriteFile(h.marker(), nil, 0o600)
}

func (h *hook) clearAsked() {
	if h.in.SessionID != "" {
		_ = os.Remove(h.marker())
	}
}

// ranOrchestrate tells whether the session's transcript holds
// /contr:orchestrate. An error counts as no: the label is best effort.
func (h *hook) ranOrchestrate() bool {
	if h.in.TranscriptPath == "" {
		return false
	}
	ran, err := agents.RanCommand(h.in.TranscriptPath, orchestrateCommand)
	return err == nil && ran
}

// coordinator tells whether the session is a coordinator: it ran
// /contr:orchestrate, or its hc terminal has role orchestrator.
func (h *hook) coordinator() (bool, error) {
	if h.in.TranscriptPath != "" {
		ran, err := agents.RanCommand(h.in.TranscriptPath, orchestrateCommand)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if ran {
			return true, nil
		}
	}
	return h.orchestratorRole()
}

// orchestratorRole tells whether the session's hc terminal has role
// orchestrator.
func (h *hook) orchestratorRole() (bool, error) {
	self := h.getenv("HC_TERMINAL_ID")
	if self == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	core, err := h.dial(ctx, h.getenv)
	if err != nil {
		return false, err
	}
	defer func() { _ = core.Close() }()
	w, err := agents.New(core, agents.Config{}).Get(ctx, self)
	if err != nil {
		return false, err
	}
	return w.Orchestrator(), nil
}

// restoreText is what a coordinator gets after a compaction: the state file
// to read, its live workers, its open owner items and the playbook, cut to
// restoreMax characters.
func (h *hook) restoreText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "contr: the conversation was compacted. You are the coordinator, and the playbook follows again.\n")
	fmt.Fprintf(&b, "First read %s: you wrote it before the compaction.\n\n", filepath.Join(h.in.Cwd, stateFile))
	// The lists come before the playbook, so a cut takes only playbook text.
	b.WriteString(h.workersText())
	b.WriteString("\n")
	b.WriteString(h.ownerText())
	b.WriteString("\n")
	b.WriteString(flowText(h.getenv))
	b.WriteString("\nThe playbook:\n\n")
	b.WriteString(h.playbook())
	return cut(b.String(), restoreMax)
}

// startupText is what a new coordinator gets: who it is, the state file when
// one exists, its live workers, its open owner items, the flow and the
// playbook, cut to restoreMax characters.
func (h *hook) startupText() string {
	var b strings.Builder
	b.WriteString("contr: you are the coordinator of this project; the owner gives the goal in the next message.\n")
	state := filepath.Join(h.in.Cwd, stateFile)
	if _, err := os.Stat(state); err == nil {
		fmt.Fprintf(&b, "First read %s: an earlier coordinator of this project wrote it.\n", state)
	}
	b.WriteString("\n")
	b.WriteString(h.workersText())
	b.WriteString("\n")
	b.WriteString(h.ownerText())
	b.WriteString("\n")
	b.WriteString(flowText(h.getenv))
	b.WriteString("\nThe playbook:\n\n")
	b.WriteString(h.playbook())
	return cut(b.String(), restoreMax)
}

// playbook is the body of the kit's orchestrate command, without its front
// matter. The goal is in the state file.
func (h *hook) playbook() string {
	kit := h.getenv("CLAUDE_PLUGIN_ROOT")
	if kit == "" {
		kit = h.getenv("CONTR_KIT")
	}
	data, err := os.ReadFile(filepath.Join(kit, "commands", "orchestrate.md"))
	if kit == "" || err != nil {
		return "The playbook could not be read (" + errText(err, "no kit folder") + "). Run /contr:orchestrate with your goal to read it again."
	}
	body := string(data)
	if rest, ok := strings.CutPrefix(body, "---\n"); ok {
		if i := strings.Index(rest, "\n---\n"); i >= 0 {
			body = rest[i+len("\n---\n"):]
		}
	}
	return strings.TrimSpace(strings.ReplaceAll(body, "$ARGUMENTS", "(see the state file)"))
}

// workersText lists the live workers that this coordinator started.
func (h *hook) workersText() string {
	self := h.getenv("HC_TERMINAL_ID")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	core, err := h.dial(ctx, h.getenv)
	if err != nil {
		return "Your live workers: unknown (" + err.Error() + "); use the list tool.\n"
	}
	defer func() { _ = core.Close() }()
	ws, err := agents.New(core, agents.Config{}).List(ctx, false)
	if err != nil {
		return "Your live workers: unknown (" + err.Error() + "); use the list tool.\n"
	}
	var b strings.Builder
	b.WriteString("Your live workers:\n")
	n := 0
	for _, w := range ws {
		if w.Parent != self || self == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "- %s (%s) %s, %s", w.Name, w.Address, w.Role, orText(w.Status, "no status"))
		if w.Issue != "" {
			fmt.Fprintf(&b, ", issue %s", w.Issue)
		}
		if w.Worktree != "" {
			fmt.Fprintf(&b, ", %s", w.Worktree)
		}
		b.WriteString("\n")
	}
	if n == 0 {
		b.WriteString("- none\n")
	}
	return b.String()
}

// ownerText lists the open questions and to-dos that this coordinator asked.
func (h *hook) ownerText() string {
	self := h.getenv("HC_TERMINAL_ID")
	store, err := ownerStore(h.getenv)
	if err != nil {
		return "Open owner items: unknown (" + err.Error() + "); use the owner_items tool.\n"
	}
	st, err := store.Read()
	if err != nil {
		return "Open owner items: unknown (" + err.Error() + "); use the owner_items tool.\n"
	}
	qs, ts := st.Open("")
	var b strings.Builder
	b.WriteString("Your open owner items:\n")
	n := 0
	for _, q := range qs {
		if q.Coordinator == self {
			n++
			fmt.Fprintf(&b, "- %s %s\n", q.ID, oneLine(orText(q.Title, q.Question)))
		}
	}
	for _, t := range ts {
		if t.Coordinator == self {
			n++
			fmt.Fprintf(&b, "- %s [%s] %s\n", t.ID, t.Kind, oneLine(t.Text))
		}
	}
	if n == 0 {
		b.WriteString("- none\n")
	}
	return b.String()
}

func orText(s, other string) string {
	if strings.TrimSpace(s) == "" {
		return other
	}
	return s
}

func errText(err error, other string) string {
	if err == nil {
		return other
	}
	return err.Error()
}

func oneLine(s string) string { return cut(strings.Join(strings.Fields(s), " "), 160) }

// cut keeps at most n characters of s, with "…" at the end when it cuts.
func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// readTurn reads the last turn of the transcript at path.
func readTurn(path string) (agents.Turn, error) {
	if path == "" {
		return agents.Turn{}, fmt.Errorf("the hook input has no transcript_path")
	}
	f, err := os.Open(path)
	if err != nil {
		return agents.Turn{}, err
	}
	defer func() { _ = f.Close() }()
	return agents.LastTurn(f)
}

// missedReport is the report for a turn that sent none: the worker's last
// words on one line, cut to saidMax characters, and RESULT.md when the
// worker's folder has one.
func missedReport(said, cwd string) string {
	return report("ended its turn without a report", said, cwd)
}

// unsureReport is the report for a turn with no transcript: as missedReport,
// but it says that it may repeat a report the worker sent.
func unsureReport(said, cwd string) string {
	return report("ended its turn; it may have sent a report already (no transcript, so contr cannot tell)", said, cwd)
}

// report is lead, the worker's last words and RESULT.md, as missedReport.
func report(lead, said, cwd string) string {
	said = strings.Join(strings.Fields(said), " ")
	if r := []rune(said); len(r) > saidMax {
		said = string(r[:saidMax]) + "…"
	}
	if said == "" {
		said = "(none)"
	}
	text := lead + ". Last words: " + said
	if cwd != "" {
		if _, err := os.Stat(filepath.Join(cwd, "RESULT.md")); err == nil {
			text += "; RESULT.md in " + cwd
		}
	}
	return text
}
