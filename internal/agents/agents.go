// Package agents starts and follows worker agents in herdr-core terminals.
// A worker is an hc terminal with the label contr=agent and labels for its
// role, issue, profile and worktree. The state comes from hc events; there is
// no polling.
package agents

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/contursofran/contr/internal/hc"
)

// Labels that mark a worker and describe it.
const (
	LabelApp      = "contr"
	AppAgent      = "agent"
	LabelRole     = "role"
	LabelIssue    = "issue"
	LabelProfile  = "profile"
	LabelWorktree = "worktree"
	LabelProject  = "project"
	// LabelModel and LabelEffort are set by the kit's Stop hook on a Claude
	// Code session's own terminal: the short model name ("opus") and the
	// effort level ("high"), for line 2 of its row (D189).
	LabelModel  = "contr.model"
	LabelEffort = "contr.effort"
	// LabelOrchestrating is set by the kit's Stop hook on a Claude Code
	// session that ran /contr:orchestrate, so its row shows that it
	// orchestrates before it has workers (D191).
	LabelOrchestrating = "contr.orchestrating"
	// LabelContext and LabelContextLimit are set by the kit's Stop hook on a
	// Claude Code session's own terminal: the context size in tokens and the
	// window Claude Code compacts at (D189). The Context meter falls back to
	// them when the session runs no status line (D205).
	LabelContext      = "contr.context"
	LabelContextLimit = "contr.context-limit"
)

// RoleOrchestrator is the role label of an orchestrator agent.
const RoleOrchestrator = "orchestrator"

// RoleTerminal is the role label of a plain terminal: a shell, not an agent.
const RoleTerminal = "terminal"

// defaultShell runs in a plain terminal when $SHELL is not set.
const defaultShell = "/bin/zsh"

// Core is the part of the hc client that the manager uses.
type Core interface {
	Call(ctx context.Context, method string, params, result any) error
	Subscribe(ctx context.Context, f hc.Filter) (Stream, error)
}

// Stream is an event subscription.
type Stream interface {
	Snapshot() hc.Snapshot
	Events() <-chan hc.Event
	Err() error
	Close() error
}

// HC adapts an *hc.Client to Core.
type HC struct{ *hc.Client }

// Subscribe opens an hc subscription.
func (c HC) Subscribe(ctx context.Context, f hc.Filter) (Stream, error) {
	s, err := c.Client.Subscribe(ctx, f)
	if err != nil {
		return nil, err
	}
	return hcStream{s}, nil
}

type hcStream struct{ *hc.Subscription }

func (s hcStream) Snapshot() hc.Snapshot { return s.Subscription.Snapshot }

// Profile is how to start one kind of agent: the program and its arguments.
// The task, when there is one, goes after them as the last argument.
type Profile struct {
	Argv []string `json:"argv"`
	// Agent names the kit agent definition (kit/agents/<agent>.md) that the
	// profile starts as. Empty means the profile name, so a variant such as
	// planner-high can start as the planner agent.
	Agent string `json:"agent,omitempty"`
}

// Config maps profile names to profiles. Kit, when set, is the folder of
// contr's Claude Code plugin (the repository's kit folder): a claude profile
// whose name has an agent definition there starts as that agent. Setup maps
// a project's main checkout (an absolute path, or one that starts with ~/)
// to a shell command that prepares a new worktree of it: it runs in the
// worktree before the agent starts, with CONTR_MAIN set to the main checkout.
type Config struct {
	Profiles map[string]Profile `json:"profiles"`
	Kit      string             `json:"kit,omitempty"`
	Setup    map[string]string  `json:"setup,omitempty"`
	// Flow is how the orchestrator takes an issue to a merge (D185).
	Flow Flow `json:"flow"`
	// Source tells where the profiles come from: the file that LoadConfig
	// read, with "(not found)" after it when the file is missing. Spawn puts
	// it in the "no profile" error, so the owner sees which file was read
	// (#484).
	Source string `json:"-"`
	// Notice is a note for the owner that LoadConfig made while it read the
	// file, or "". Today it says the file is in the old Swift format (D206).
	Notice string `json:"-"`
}

// OldFormatNotice is the Notice for an agents.json that has a presets key
// and no profiles key: the file of the old Swift app.
const OldFormatNotice = "agents.json is in the old format (presets); its presets are ignored and the default profiles apply"

// EnvCoordinator holds the address of the terminal that started a worker, so
// the worker can report with `hc send "$CONTR_COORDINATOR" …`.
const EnvCoordinator = "CONTR_COORDINATOR"

// DefaultConfig holds the owner's standard profiles.
func DefaultConfig() Config {
	claude := func(model, effort string) Profile {
		return Profile{Argv: []string{"claude", "--model", model, "--effort", effort, "--permission-mode", "auto"}}
	}
	return Config{Flow: DefaultFlow(), Profiles: map[string]Profile{
		"orchestrator": claude("opus", "medium"),
		"builder":      claude("sonnet", "medium"),
		"planner":      claude("opus", "low"),
		"reviewer":     claude("opus", "low"),
		"debugger":     claude("opus", "xhigh"),
		"explorer":     claude("sonnet", "medium"),
	}}
}

// ModelOf is the model that the profile starts: the value after --model in
// its argv. It is "" for an unknown profile or one with no --model.
func (c Config) ModelOf(profile string) string {
	return flagValue(c.Profiles[profile].Argv, "--model")
}

// EffortOf is the effort that the profile starts: the value after --effort
// in its argv. It is "" for an unknown profile or one with no --effort.
func (c Config) EffortOf(profile string) string {
	return flagValue(c.Profiles[profile].Argv, "--effort")
}

// noProfile is the error for a profile that c does not have. It names the
// profiles file that was read and the profiles that c has.
func (c Config) noProfile(name string) error {
	names := slices.Sorted(maps.Keys(c.Profiles))
	if c.Source == "" {
		return fmt.Errorf("agents: no profile %q; profiles: %s", name, strings.Join(names, ", "))
	}
	return fmt.Errorf("agents: no profile %q in %s; profiles: %s", name, c.Source, strings.Join(names, ", "))
}

// flagValue is the value after flag (or flag=) in argv, or "".
func flagValue(argv []string, flag string) string {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1]
		}
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v
		}
	}
	return ""
}

// LoadConfig reads a JSON config file over the defaults: a profile in the file
// replaces the default of the same name, and a flow key missing from the
// file's flow keeps its default. A missing file gives the defaults. A file
// with a presets key and no profiles key is the old Swift format: it loads
// with the defaults, Notice says so, and the file is not changed.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	cfg.Source = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg.Source = path + " (not found)"
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	file := Config{Flow: DefaultFlow()}
	if err := json.Unmarshal(data, &file); err != nil {
		return cfg, fmt.Errorf("agents: %s: %w", path, err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err == nil {
		_, presets := keys["presets"]
		_, profiles := keys["profiles"]
		if presets && !profiles {
			cfg.Notice = OldFormatNotice
		}
	}
	if file.Kit != "" {
		cfg.Kit = file.Kit
	}
	cfg.Setup = file.Setup
	if err := file.Flow.Validate(); err != nil {
		return cfg, fmt.Errorf("agents: %s: %w", path, err)
	}
	cfg.Flow = file.Flow
	for name, p := range file.Profiles {
		if len(p.Argv) == 0 {
			return cfg, fmt.Errorf("agents: %s: profile %q has no argv", path, name)
		}
		cfg.Profiles[name] = p
	}
	return cfg, nil
}

// Spec describes a worker to start.
type Spec struct {
	Name     string // unique among live terminals: 1-32 of a-z 0-9 - _
	Profile  string
	Role     string // defaults to the profile name
	Issue    string
	Worktree string  // the working folder
	Task     string  // the first message, if any
	Parent   string  // hc terminal that gets notes about the worker; empty for none
	Project  string  // the project name; no control characters
	Colors   *Colors // the terminal's colors for the program's color queries; nil for hc's default
}

// Colors are the foreground, background and cursor colors of the terminal
// theme, each "#rrggbb". hc answers a program's color queries (OSC 10, 11,
// 12) with them, so Claude Code's auto theme sees the right background.
type Colors struct{ FG, BG, Cursor string }

// display is the display of terminal.run, or nil for no colors.
func (c *Colors) display() map[string]any {
	if c == nil {
		return nil
	}
	d := map[string]any{}
	for k, v := range map[string]string{"fg": c.FG, "bg": c.BG, "cursor": c.Cursor} {
		if v != "" {
			d[k] = v
		}
	}
	if len(d) == 0 {
		return nil
	}
	return d
}

// Worker is one worker as hc reports it.
type Worker struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Role     string `json:"role"`
	Issue    string `json:"issue,omitempty"`
	Profile  string `json:"profile"`
	Worktree string `json:"worktree"`
	Project  string `json:"project,omitempty"`
	// Parent is the hc terminal that started the worker, if any.
	Parent string `json:"parent,omitempty"`
	// Command is the program that runs in the terminal now: hc's foreground
	// name, else the base name of the spawn command.
	Command string `json:"command,omitempty"`
	// LastOutput is the time of the last hc event for the terminal, as Watch
	// saw it. hc has no output time (decision D35). Zero when none was seen.
	LastOutput time.Time `json:"last_output,omitzero"`
	// Live is false once the program ended.
	Live bool `json:"live"`
	// Status is the program's own word: idle, working, done, blocked or
	// error. Empty when it never said or after it ended.
	Status   string `json:"status,omitempty"`
	Kind     string `json:"kind,omitempty"` // permission, question or auth, when blocked
	Msg      string `json:"msg,omitempty"`
	Turn     uint64 `json:"turn"`
	Title    string `json:"title,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	// Model is the --model of the program's own command line, when it has
	// one, else the model that the kit's Stop hook put on the terminal. A
	// terminal contr did not start has no profile, so this is how its row
	// knows the model (D169, D189).
	Model string `json:"model,omitempty"`
	// Effort is the --effort of the program's own command line, else the
	// effort that the kit's Stop hook put on the terminal (D189).
	Effort string `json:"effort,omitempty"`
	// Orchestrating is true when the kit's Stop hook saw /contr:orchestrate
	// in the session (LabelOrchestrating, D191).
	Orchestrating bool `json:"orchestrating,omitempty"`
	// ContextTokens and ContextLimit are the context size and the compaction
	// window that the kit's Stop hook put on the terminal (LabelContext,
	// LabelContextLimit). Both are zero when a label is missing or not a
	// positive integer (D205).
	ContextTokens uint64 `json:"context_tokens,omitempty"`
	ContextLimit  uint64 `json:"context_limit,omitempty"`
	// Task is what a worker is doing, for line 1 of its row: the cleaned
	// terminal title. Set by the agent model; empty when the title is not
	// usable (D169).
	Task string `json:"-"`
	// Foreign is true for a terminal that contr did not start: it has no
	// contr labels. Its role and folder come from hc (D149).
	Foreign bool `json:"foreign,omitempty"`
	// ProjectKey is the git common folder of the working folder, set by the
	// agent model once it has read it. It tells apart two repos with the same
	// folder name. Empty when unknown or outside git.
	ProjectKey string `json:"-"`
	// Shown is the readable name of a foreign terminal, set by the agent
	// model (D156). Empty when Name is already readable.
	Shown string `json:"-"`
	// Session is true for a plain terminal of contr that runs Claude Code
	// in the foreground: its role is orchestrator while Claude Code runs,
	// and it is named from its title, as a foreign terminal (D190).
	Session bool `json:"session,omitempty"`
}

// DisplayName is the name the UI shows: Shown when set, else Name.
func (w Worker) DisplayName() string {
	if w.Shown != "" {
		return w.Shown
	}
	return w.Name
}

// terminal is the part of an hc terminal that a Worker needs.
type terminal struct {
	Address  string            `json:"address"`
	Cwd      string            `json:"cwd"`
	Parent   *string           `json:"parent"`
	Name     *string           `json:"name"`
	Labels   map[string]string `json:"labels"`
	State    string            `json:"state"`
	Turn     uint64            `json:"turn"`
	Title    string            `json:"title"`
	ExitCode *int              `json:"exit_code"`
	Argv     []string          `json:"argv"`
	// Foreground is null once the terminal finished.
	Foreground *struct {
		Name string `json:"name"`
	} `json:"foreground"`
	Status *struct {
		State string  `json:"state"`
		Kind  *string `json:"kind"`
		Msg   string  `json:"msg"`
	} `json:"status"`
}

func (t terminal) worker() Worker {
	w := Worker{
		Address:  t.Address,
		Role:     t.Labels[LabelRole],
		Issue:    t.Labels[LabelIssue],
		Profile:  t.Labels[LabelProfile],
		Worktree: t.Labels[LabelWorktree],
		Project:  t.Labels[LabelProject],
		Command:  t.command(),
		Live:     t.State != "finished",
		Turn:     t.Turn,
		Title:    t.Title,
		ExitCode: t.ExitCode,
		Model:    orText(flagValue(t.Argv, "--model"), t.Labels[LabelModel]),
		Effort:   orText(flagValue(t.Argv, "--effort"), t.Labels[LabelEffort]),

		Orchestrating: t.Labels[LabelOrchestrating] != "",
	}
	w.ContextTokens, w.ContextLimit = contextLabels(t.Labels)
	if t.Name != nil {
		w.Name = *t.Name
	}
	if t.Labels[LabelApp] != AppAgent {
		t.foreign(&w)
	} else if w.Role == RoleTerminal && t.claudeInFront() {
		// The owner ran Claude Code in a plain terminal: while it runs, it is
		// a Claude session (D190).
		w.Role, w.Session = RoleOrchestrator, true
	}
	if t.Parent != nil {
		w.Parent = *t.Parent
	}
	if t.Status != nil {
		w.Status, w.Msg = t.Status.State, t.Status.Msg
		if t.Status.Kind != nil {
			w.Kind = *t.Status.Kind
		}
	}
	return w
}

// foreign fills w for a terminal that has no contr labels (D149): the role
// is orchestrator for Claude Code, else terminal; the folder is the working
// folder that hc reports; a terminal without a name shows its address.
func (t terminal) foreign(w *Worker) {
	w.Foreign = true
	w.Worktree = t.Cwd
	if w.Name == "" {
		w.Name = t.Address
	}
	w.Role = RoleTerminal
	if t.runsClaude() {
		w.Role = RoleOrchestrator
	}
}

// versionName matches the name that Claude Code gives itself as foreground
// program: its version, such as 2.1.30.
var versionName = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// runsClaude tells if Claude Code runs in the terminal: it is the
// foreground program (by name or by its version), the program in argv, or the
// terminal has an hc status, which only an agent sets.
func (t terminal) runsClaude() bool {
	if t.Status != nil || t.claudeInFront() {
		return true
	}
	return len(t.Argv) > 0 && filepath.Base(t.Argv[0]) == "claude"
}

// claudeInFront tells if Claude Code is the foreground program, by name or
// by its version. In a shell this is false again once Claude Code exits.
func (t terminal) claudeInFront() bool {
	if t.Foreground == nil {
		return false
	}
	n := filepath.Base(t.Foreground.Name)
	return n == "claude" || versionName.MatchString(n)
}

// command is the foreground program's name, else the base of argv[0], or
// of the agent's argv[0] after a setup wrapper.
func (t terminal) command() string {
	if t.Foreground != nil && t.Foreground.Name != "" {
		return t.Foreground.Name
	}
	argv := t.Argv
	if len(argv) > 5 && argv[3] == setupName {
		argv = argv[5:]
	}
	if len(argv) > 0 {
		return filepath.Base(argv[0])
	}
	return ""
}

// Orchestrator tells if the worker is an orchestrator.
func (w Worker) Orchestrator() bool { return w.Role == RoleOrchestrator }

// Plain tells if the worker is a plain terminal (a shell, not an agent).
func (w Worker) Plain() bool { return w.Role == RoleTerminal }

func decodeWorker(raw json.RawMessage) (Worker, error) {
	var t terminal
	if err := json.Unmarshal(raw, &t); err != nil {
		return Worker{}, fmt.Errorf("agents: bad terminal: %w", err)
	}
	return t.worker(), nil
}

// filter selects the workers.
var filter = map[string]string{LabelApp: AppAgent}

// Manager starts, lists and controls workers.
type Manager struct {
	core Core
	cfg  Config
	now  func() time.Time // the clock; tests replace it

	getenv func(string) string // reads the environment; tests replace it
	git    git                 // runs git; tests replace it

	// exe is the contr binary that Claude's status line runs; empty for none.
	exe string
}

// New returns a manager on core with the profiles of cfg. A relative kit
// path is made absolute here, because the agent runs in its worktree.
func New(core Core, cfg Config) *Manager {
	if cfg.Kit != "" {
		if abs, err := filepath.Abs(cfg.Kit); err == nil {
			cfg.Kit = abs
		}
	}
	return &Manager{core: core, cfg: cfg, now: time.Now, getenv: os.Getenv, git: runGit, exe: contrExe()}
}

// contrExe is the absolute path of the running contr binary, found once; ""
// when it cannot be found.
var contrExe = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		slog.Warn("unable to find the contr binary; Claude sessions get no status line", "err", err)
		return ""
	}
	if abs, err := filepath.Abs(exe); err == nil {
		return abs
	}
	return exe
})

// WithExecutable sets the contr binary path that Claude's status line runs
// (empty for none) and returns m. New sets it from os.Executable.
func (m *Manager) WithExecutable(path string) *Manager {
	m.exe = path
	return m
}

// Spawn starts a worker and returns it.
func (m *Manager) Spawn(ctx context.Context, s Spec) (Worker, error) {
	p, ok := m.cfg.Profiles[s.Profile]
	if !ok {
		return Worker{}, m.cfg.noProfile(s.Profile)
	}
	if s.Name == "" {
		return Worker{}, errors.New("agents: a worker needs a name")
	}
	if strings.ContainsFunc(s.Project, unicode.IsControl) {
		return Worker{}, errors.New("agents: the project name has a control character")
	}
	if s.Worktree != "" {
		dir, err := worktreeDir(s.Worktree)
		if err != nil {
			return Worker{}, err
		}
		s.Worktree = dir
	}
	role := s.Role
	if role == "" {
		role = s.Profile
	}
	labels := map[string]string{LabelApp: AppAgent, LabelRole: role, LabelProfile: s.Profile, LabelWorktree: s.Worktree}
	if s.Project != "" {
		labels[LabelProject] = s.Project
	}
	if s.Issue != "" {
		labels[LabelIssue] = s.Issue
	}
	agent := p.Agent
	if agent == "" {
		agent = s.Profile
	}
	argv := m.agentArgv(agent, p.Argv)
	if s.Task != "" {
		argv = append(argv, s.Task)
	}
	env := map[string]string{}
	if setup, main := m.setupFor(ctx, s.Worktree); setup != "" {
		argv = withSetup(setup, argv)
		env[EnvMain] = main
	}
	params := map[string]any{"argv": argv, "name": s.Name, "labels": labels, "wait": false, "outlive_parent": true}
	if s.Worktree != "" {
		params["cwd"] = s.Worktree
	}
	if s.Parent != "" {
		env[EnvCoordinator] = s.Parent
	}
	if len(env) > 0 {
		params["env"] = env
	}
	if d := s.Colors.display(); d != nil {
		params["display"] = d
	}
	if s.Parent != "" {
		params["parent"] = s.Parent
	} else {
		params["no_parent"] = true
	}
	var raw json.RawMessage
	if err := m.core.Call(ctx, "terminal.run", params, &raw); err != nil {
		return Worker{}, err
	}
	return decodeWorker(raw)
}

// TerminalSpec describes a plain terminal.
type TerminalSpec struct {
	Name    string
	Project string  // the open project; empty for none
	Dir     string  // the start folder; empty for the core's default
	Shell   string  // empty for $SHELL, then /bin/zsh
	Colors  *Colors // as Spec.Colors
}

// OpenTerminal starts a login shell in an hc terminal and returns it. The
// terminal has the worker labels with role "terminal", so it stays in hc
// after contr closes and agents can read it.
func (m *Manager) OpenTerminal(ctx context.Context, s TerminalSpec) (Worker, error) {
	if s.Name == "" {
		return Worker{}, errors.New("agents: a terminal needs a name")
	}
	if strings.ContainsFunc(s.Project, unicode.IsControl) {
		return Worker{}, errors.New("agents: the project name has a control character")
	}
	shell := s.Shell
	if shell == "" && m.getenv != nil {
		shell = m.getenv("SHELL")
	}
	if shell == "" {
		shell = defaultShell
	}
	labels := map[string]string{LabelApp: AppAgent, LabelRole: RoleTerminal, LabelWorktree: s.Dir}
	if s.Project != "" {
		labels[LabelProject] = s.Project
	}
	params := map[string]any{"argv": []string{shell, "-l"}, "name": s.Name, "labels": labels, "wait": false, "outlive_parent": true, "no_parent": true}
	if s.Dir != "" {
		params["cwd"] = s.Dir
	}
	if d := s.Colors.display(); d != nil {
		params["display"] = d
	}
	var raw json.RawMessage
	if err := m.core.Call(ctx, "terminal.run", params, &raw); err != nil {
		return Worker{}, err
	}
	return decodeWorker(raw)
}

// agentArgv returns the profile's command. With a kit set, a claude command
// gets `--plugin-dir <kit>` after the program name, unless it has
// `--plugin-dir` already (D211). When its agent (Profile.Agent, else the
// profile name) has a definition in the kit, it also gets
// `--agent contr:<agent>`. Every claude command gets `--settings` with a
// status line that runs `contr statusline`, unless it has `--settings`
// already (D203).
func (m *Manager) agentArgv(agent string, argv []string) []string {
	out := append([]string(nil), argv...)
	if len(out) == 0 || filepath.Base(out[0]) != "claude" {
		return out
	}
	head := []string{out[0]}
	if m.cfg.Kit != "" {
		if !hasFlag(out[1:], "--plugin-dir") {
			head = append(head, "--plugin-dir", m.cfg.Kit)
		}
		if _, err := os.Stat(filepath.Join(m.cfg.Kit, "agents", agent+".md")); err == nil {
			head = append(head, "--agent", "contr:"+agent)
		}
	}
	if m.exe != "" && !hasFlag(out[1:], "--settings") {
		if js, err := statuslineSettings(m.exe); err != nil {
			slog.Warn("unable to build the status line settings", "err", err)
		} else {
			head = append(head, "--settings", js)
		}
	}
	return append(head, out[1:]...)
}

// hasFlag reports whether args set flag, as `flag v` or `flag=v`.
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

// statuslineSettings is the --settings JSON that makes Claude Code run
// `<exe> statusline` as its status line. exe is quoted for a POSIX shell.
func statuslineSettings(exe string) (string, error) {
	type statusLine struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	b, err := json.Marshal(map[string]statusLine{"statusLine": {Type: "command", Command: shellQuote(exe) + " statusline"}})
	return string(b), err
}

// shellQuote quotes s as one word for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// List returns the workers, sorted by name. With all, finished ones too.
func (m *Manager) List(ctx context.Context, all bool) ([]Worker, error) {
	var res struct {
		Terminals []json.RawMessage `json:"terminals"`
	}
	if err := m.core.Call(ctx, "terminal.list", map[string]any{"all": all, "labels": filter}, &res); err != nil {
		return nil, err
	}
	out := make([]Worker, 0, len(res.Terminals))
	for _, raw := range res.Terminals {
		w, err := decodeWorker(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	sortWorkers(out)
	return out, nil
}

// Send types text into a worker and presses Enter. Blank text is an error:
// hc would still submit its sender tag, an empty message (#483, D200). A send
// that hc refuses (sent false) is an error with hc's reason.
func (m *Manager) Send(ctx context.Context, name, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("agents: no text to send")
	}
	params := m.looking(name)
	params["text"] = text
	var res struct {
		Sent   *bool  `json:"sent"`
		Reason string `json:"reason"`
	}
	if err := m.core.Call(ctx, "terminal.send", params, &res); err != nil {
		return err
	}
	if res.Sent != nil && !*res.Sent {
		return fmt.Errorf("agents: %s did not take the text: %s", name, cmp.Or(res.Reason, "no reason given"))
	}
	return nil
}

// WithConfig returns a copy of m on the same core with the profiles of cfg.
// A relative kit path is made absolute, as in New.
func (m *Manager) WithConfig(cfg Config) *Manager {
	c := New(m.core, cfg)
	c.now, c.getenv, c.exe = m.now, m.getenv, m.exe
	return c
}

// WithEnv makes m read the environment with getenv (HC_TERMINAL_ID, SHELL)
// and returns m.
func (m *Manager) WithEnv(getenv func(string) string) *Manager {
	m.getenv = getenv
	return m
}

// SendKeys presses raw keys in a worker and does not press Enter. hc's
// terminal.keys method takes key names, so each byte becomes a name: printable
// ASCII is itself, and CR, LF, tab, ESC, DEL, space, Ctrl+A..Z and the arrow
// sequences ESC [ A..D have names. Any other byte is an error and nothing is
// sent.
func (m *Manager) SendKeys(ctx context.Context, name string, keys []byte) error {
	if len(keys) == 0 {
		return errors.New("agents: no keys to send")
	}
	names, err := keyNames(keys)
	if err != nil {
		return err
	}
	return m.core.Call(ctx, "terminal.keys", map[string]any{"terminal": name, "keys": names}, nil)
}

// keyNames turns raw bytes into hc key names.
func keyNames(keys []byte) ([]string, error) {
	var out []string
	for i := 0; i < len(keys); i++ {
		b := keys[i]
		switch {
		case b == 0x1b:
			if i+2 < len(keys) && (keys[i+1] == '[' || keys[i+1] == 'O') && keys[i+2] >= 'A' && keys[i+2] <= 'D' {
				out = append(out, [...]string{"up", "down", "right", "left"}[keys[i+2]-'A'])
				i += 2
			} else {
				out = append(out, "esc")
			}
		case b == '\r' || b == '\n':
			out = append(out, "enter")
		case b == '\t':
			out = append(out, "tab")
		case b == 0x7f:
			out = append(out, "backspace")
		case b == ' ':
			out = append(out, "space")
		case b >= 1 && b <= 26:
			out = append(out, "ctrl+"+string(rune('a'+b-1)))
		case b > ' ' && b < 0x7f:
			out = append(out, string(rune(b)))
		default:
			return nil, fmt.Errorf("agents: key byte 0x%02x has no hc name", b)
		}
	}
	return out, nil
}

// looking returns the params of a call that looks at, acts on or sends to a
// terminal. Inside hc they name this terminal as the caller. When it is the
// worker's parent, hc then counts the worker's news as seen and types no note
// about it into the parent: a coordinator that stops a worker gets no exit
// note. On a send between two agents, hc tags the text with the sender.
func (m *Manager) looking(name string) map[string]any {
	params := map[string]any{"terminal": name}
	if m.getenv != nil {
		if caller := m.getenv("HC_TERMINAL_ID"); caller != "" {
			params["caller"] = caller
		}
	}
	return params
}

// Interrupt presses a worker's stop key. The outcome is hc's: stopped,
// exited, still_working or unknown.
func (m *Manager) Interrupt(ctx context.Context, name string) (string, error) {
	var res struct {
		Outcome string `json:"outcome"`
	}
	err := m.core.Call(ctx, "terminal.interrupt", m.looking(name), &res)
	return res.Outcome, err
}

// Stop ends a worker's program and then removes its terminal from hc. With
// keep, the finished terminal stays for reading. A terminal that already
// finished is not killed again: it is only removed (or left, with keep).
func (m *Manager) Stop(ctx context.Context, name string, keep bool) error {
	w, err := m.Get(ctx, name)
	if err != nil {
		return err
	}
	if w.Live {
		if err := m.core.Call(ctx, "terminal.kill", m.looking(name), nil); err != nil {
			return err
		}
	}
	if keep {
		return nil
	}
	return m.Remove(ctx, name)
}

// RemoveEnded removes the finished workers that the caller started: those
// whose parent is the terminal in HC_TERMINAL_ID. Orchestrators and plain
// terminals stay, and so do the workers of other coordinators. It returns
// the names removed. Outside hc it removes nothing.
func (m *Manager) RemoveEnded(ctx context.Context) ([]string, error) {
	var caller string
	if m.getenv != nil {
		caller = m.getenv("HC_TERMINAL_ID")
	}
	if caller == "" {
		return nil, nil
	}
	ws, err := m.List(ctx, true)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, w := range ws {
		if w.Live || w.Orchestrator() || w.Plain() || w.Parent != caller {
			continue
		}
		if err := m.Remove(ctx, w.Address); err != nil {
			return removed, fmt.Errorf("remove %s: %w", w.Name, err)
		}
		removed = append(removed, w.Name)
	}
	return removed, nil
}

// Get returns one worker by name or address.
func (m *Manager) Get(ctx context.Context, name string) (Worker, error) {
	var raw json.RawMessage
	if err := m.core.Call(ctx, "terminal.get", m.looking(name), &raw); err != nil {
		return Worker{}, err
	}
	return decodeWorker(raw)
}

// Remove deletes a finished terminal from hc, so lists and watches drop it.
// hc refuses a live one (error kind "live").
func (m *Manager) Remove(ctx context.Context, name string) error {
	return m.core.Call(ctx, "terminal.rm", m.looking(name), nil)
}

// Read returns a worker's screen and history; lines > 0 keeps only the last
// lines.
func (m *Manager) Read(ctx context.Context, name string, lines int) (string, error) {
	params := m.looking(name)
	if lines > 0 {
		params["lines"] = lines
	}
	var res struct {
		Text string `json:"text"`
	}
	err := m.core.Call(ctx, "terminal.read", params, &res)
	return res.Text, err
}

func sortWorkers(ws []Worker) {
	sort.Slice(ws, func(i, j int) bool {
		if ws[i].Name != ws[j].Name {
			return ws[i].Name < ws[j].Name
		}
		return ws[i].Address < ws[j].Address
	})
}

// orText is s, else other when s is empty.
// contextLabels reads LabelContext and LabelContextLimit. Both must be
// positive integers; else it gives no data (0, 0).
func contextLabels(labels map[string]string) (tokens, limit uint64) {
	t, errT := strconv.ParseUint(labels[LabelContext], 10, 64)
	l, errL := strconv.ParseUint(labels[LabelContextLimit], 10, 64)
	if errT != nil || errL != nil || t == 0 || l == 0 {
		return 0, 0
	}
	return t, l
}

func orText(s, other string) string {
	if s != "" {
		return s
	}
	return other
}
