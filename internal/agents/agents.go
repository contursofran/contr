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
	"sort"

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
)

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
}

// Config maps profile names to profiles. Kit, when set, is the folder of
// contr's Claude Code plugin (the repository's kit folder): a claude profile
// whose name has an agent definition there starts as that agent.
type Config struct {
	Profiles map[string]Profile `json:"profiles"`
	Kit      string             `json:"kit,omitempty"`
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
}

// Worker is one worker as hc reports it.
type Worker struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Role     string `json:"role"`
	Issue    string `json:"issue,omitempty"`
	Profile  string `json:"profile"`
	Worktree string `json:"worktree"`
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
}

// terminal is the part of an hc terminal that a Worker needs.
type terminal struct {
	Address  string            `json:"address"`
	Name     *string           `json:"name"`
	Labels   map[string]string `json:"labels"`
	State    string            `json:"state"`
	Turn     uint64            `json:"turn"`
	Title    string            `json:"title"`
	ExitCode *int              `json:"exit_code"`
	Status   *struct {
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
		Live:     t.State != "finished",
		Turn:     t.Turn,
		Title:    t.Title,
		ExitCode: t.ExitCode,
	}
	if t.Name != nil {
		w.Name = *t.Name
	}
	if t.Status != nil {
		w.Status, w.Msg = t.Status.State, t.Status.Msg
		if t.Status.Kind != nil {
			w.Kind = *t.Status.Kind
		}
	}
	return w
}

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
}

// New returns a manager on core with the profiles of cfg.
func New(core Core, cfg Config) *Manager {
	return &Manager{core: core, cfg: cfg}
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
	role := s.Role
	if role == "" {
		role = s.Profile
	}
	labels := map[string]string{LabelApp: AppAgent, LabelRole: role, LabelProfile: s.Profile, LabelWorktree: s.Worktree}
	if s.Issue != "" {
		labels[LabelIssue] = s.Issue
	}
	argv := m.agentArgv(s.Profile, p.Argv)
	if s.Task != "" {
		argv = append(argv, s.Task)
	}
	params := map[string]any{"argv": argv, "name": s.Name, "labels": labels, "wait": false, "outlive_parent": true}
	if s.Worktree != "" {
		params["cwd"] = s.Worktree
	}
	if s.Parent != "" {
		params["parent"] = s.Parent
		params["env"] = map[string]string{EnvCoordinator: s.Parent}
	} else {
		params["no_parent"] = true
	}
	var raw json.RawMessage
	if err := m.core.Call(ctx, "terminal.run", params, &raw); err != nil {
		return Worker{}, err
	}
	return decodeWorker(raw)
}

// agentArgv returns the profile's command. A claude command for a profile
// that has an agent definition in the kit gets `--plugin-dir <kit> --agent
// contr:<profile>` after the program name.
func (m *Manager) agentArgv(profile string, argv []string) []string {
	out := append([]string(nil), argv...)
	if m.cfg.Kit == "" || len(out) == 0 || filepath.Base(out[0]) != "claude" {
		return out
	}
	if _, err := os.Stat(filepath.Join(m.cfg.Kit, "agents", profile+".md")); err != nil {
		return out
	}
	return append([]string{out[0], "--plugin-dir", m.cfg.Kit, "--agent", "contr:" + profile}, out[1:]...)
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
	return m.core.Call(ctx, "terminal.send", map[string]any{"terminal": name, "text": text}, nil)
}

// Interrupt presses a worker's stop key. The outcome is hc's: stopped,
// exited, still_working or unknown.
func (m *Manager) Interrupt(ctx context.Context, name string) (string, error) {
	var res struct {
		Outcome string `json:"outcome"`
	}
	err := m.core.Call(ctx, "terminal.interrupt", map[string]any{"terminal": name}, &res)
	return res.Outcome, err
}

// Stop ends a worker's program.
func (m *Manager) Stop(ctx context.Context, name string) error {
	return m.core.Call(ctx, "terminal.kill", map[string]any{"terminal": name}, nil)
}

// Read returns a worker's screen and history; lines > 0 keeps only the last
// lines.
func (m *Manager) Read(ctx context.Context, name string, lines int) (string, error) {
	params := map[string]any{"terminal": name}
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
