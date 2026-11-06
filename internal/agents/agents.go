// Package agents starts and follows worker agents in herdr-core terminals.
// A worker is an hc terminal with the label contr=agent and labels for its
// role, issue, profile and worktree. The state comes from hc events; there is
// no polling.
package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
}

// EnvCoordinator holds the address of the terminal that started a worker, so
// the worker can report with `hc send "$CONTR_COORDINATOR" …`.
const EnvCoordinator = "CONTR_COORDINATOR"

// DefaultConfig holds the owner's standard profiles.
func DefaultConfig() Config {
	claude := func(model, effort string) Profile {
		return Profile{Argv: []string{"claude", "--model", model, "--effort", effort, "--permission-mode", "auto"}}
	}
	return Config{Profiles: map[string]Profile{
		"builder":  claude("sonnet", "medium"),
		"planner":  claude("opus", "low"),
		"reviewer": claude("opus", "low"),
		"debugger": claude("opus", "xhigh"),
	}}
}

// ModelOf is the model that the profile starts: the value after --model in
// its argv. It is "" for an unknown profile or one with no --model.
func (c Config) ModelOf(profile string) string {
	return modelFromArgv(c.Profiles[profile].Argv)
}

// modelFromArgv is the value after --model (or --model=) in argv, or "".
func modelFromArgv(argv []string) string {
	for i, a := range argv {
		if a == "--model" && i+1 < len(argv) {
			return argv[i+1]
		}
		if v, ok := strings.CutPrefix(a, "--model="); ok {
			return v
		}
	}
	return ""
}

// LoadConfig reads a JSON config file over the defaults: a profile in the file
// replaces the default of the same name. A missing file gives the defaults.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	var file Config
	if err := json.Unmarshal(data, &file); err != nil {
		return cfg, fmt.Errorf("agents: %s: %w", path, err)
	}
	if file.Kit != "" {
		cfg.Kit = file.Kit
	}
	cfg.Setup = file.Setup
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
	Worktree string // the working folder
	Task     string // the first message, if any
	Parent   string // hc terminal that gets notes about the worker; empty for none
	Project  string // the project name; no control characters
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
	// one. A terminal contr did not start has no profile, so this is how its
	// row knows the model (D169).
	Model string `json:"model,omitempty"`
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
		Model:    modelFromArgv(t.Argv),
	}
	if t.Name != nil {
		w.Name = *t.Name
	}
	if t.Labels[LabelApp] != AppAgent {
		t.foreign(&w)
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
	if t.Status != nil {
		return true
	}
	if t.Foreground != nil {
		if n := filepath.Base(t.Foreground.Name); n == "claude" || versionName.MatchString(n) {
			return true
		}
	}
	return len(t.Argv) > 0 && filepath.Base(t.Argv[0]) == "claude"
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
}

// New returns a manager on core with the profiles of cfg. A relative kit
// path is made absolute here, because the agent runs in its worktree.
func New(core Core, cfg Config) *Manager {
	if cfg.Kit != "" {
		if abs, err := filepath.Abs(cfg.Kit); err == nil {
			cfg.Kit = abs
		}
	}
	return &Manager{core: core, cfg: cfg, now: time.Now, getenv: os.Getenv, git: runGit}
}

// Spawn starts a worker and returns it.
func (m *Manager) Spawn(ctx context.Context, s Spec) (Worker, error) {
	p, ok := m.cfg.Profiles[s.Profile]
	if !ok {
		return Worker{}, fmt.Errorf("agents: no profile %q", s.Profile)
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
	Project string // the open project; empty for none
	Dir     string // the start folder; empty for the core's default
	Shell   string // empty for $SHELL, then /bin/zsh
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
	var raw json.RawMessage
	if err := m.core.Call(ctx, "terminal.run", params, &raw); err != nil {
		return Worker{}, err
	}
	return decodeWorker(raw)
}

// agentArgv returns the profile's command. A claude command whose agent
// (Profile.Agent, else the profile name) has a definition in the kit gets
// `--plugin-dir <kit> --agent contr:<agent>` after the program name.
func (m *Manager) agentArgv(agent string, argv []string) []string {
	out := append([]string(nil), argv...)
	if m.cfg.Kit == "" || len(out) == 0 || filepath.Base(out[0]) != "claude" {
		return out
	}
	if _, err := os.Stat(filepath.Join(m.cfg.Kit, "agents", agent+".md")); err != nil {
		return out
	}
	return append([]string{out[0], "--plugin-dir", m.cfg.Kit, "--agent", "contr:" + agent}, out[1:]...)
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

// Send types text into a worker and presses Enter.
func (m *Manager) Send(ctx context.Context, name, text string) error {
	params := m.looking(name)
	params["text"] = text
	return m.core.Call(ctx, "terminal.send", params, nil)
}

// WithConfig returns a copy of m on the same core with the profiles of cfg.
// A relative kit path is made absolute, as in New.
func (m *Manager) WithConfig(cfg Config) *Manager {
	c := New(m.core, cfg)
	c.now, c.getenv = m.now, m.getenv
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
