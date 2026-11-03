package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/mcp"
)

// managers gives the agents manager for a tool call. It connects to the core
// on the first call, and again after the connection ended.
type managers struct {
	mu     sync.Mutex
	dial   func(context.Context, func(string) string) (agentsCore, error)
	getenv func(string) string
	cfg    agents.Config
	core   agentsCore
}

func (m *managers) get(ctx context.Context) (*agents.Manager, agentsCore, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.core == nil {
		core, err := m.dial(ctx, m.getenv)
		if err != nil {
			return nil, nil, err
		}
		m.core = core
	}
	return agents.New(m.core, m.cfg), m.core, nil
}

// check drops used, the connection of a call, after err says that it ended,
// so the next call connects again. A connection that another call already
// replaced stays as it is.
func (m *managers) check(used agentsCore, err error) error {
	if errors.Is(err, hc.ErrClosed) {
		m.mu.Lock()
		if m.core == used && used != nil {
			_ = used.Close()
			m.core = nil
		}
		m.mu.Unlock()
	}
	return err
}

func (m *managers) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.core != nil {
		_ = m.core.Close()
		m.core = nil
	}
}

// runMCP is `contr mcp`: the agent tools over MCP on stdin and stdout.
func runMCP(stdin io.Reader, stdout, stderr io.Writer, dial func(context.Context, func(string) string) (agentsCore, error), getenv func(string) string) int {
	cfg, err := agents.LoadConfig(configPath(getenv))
	if err != nil {
		fmt.Fprintf(stderr, "contr mcp: %v\n", err)
		return 1
	}
	if kit := getenv("CONTR_KIT"); kit != "" {
		cfg.Kit = kit
	}
	m := &managers{dial: dial, getenv: getenv, cfg: cfg}
	defer m.close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	srv := mcp.New("contr", app.Version, agentTools(m, getenv("HC_TERMINAL_ID")))
	if err := srv.Serve(ctx, stdin, stdout); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(stderr, "contr mcp: %v\n", err)
		return 1
	}
	return 0
}

// prop is one argument of a tool: its JSON type and description.
type prop struct{ typ, desc string }

func str(desc string) prop { return prop{"string", desc} }

func schema(required []string, props map[string]prop) json.RawMessage {
	p := map[string]any{}
	for name, pr := range props {
		p[name] = map[string]any{"type": pr.typ, "description": pr.desc}
	}
	s := map[string]any{"type": "object", "properties": p}
	if len(required) > 0 {
		s["required"] = required
	}
	b, _ := json.Marshal(s) // a map of strings always encodes
	return b
}

func toJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	return string(b), err
}

// agentTools are the MCP tools on the agents core. parent is the coordinator's
// hc terminal, from HC_TERMINAL_ID; spawn makes it the worker's parent.
func agentTools(m *managers, parent string) []mcp.Tool {
	type args struct {
		Name     string `json:"name"`
		Profile  string `json:"profile"`
		Task     string `json:"task"`
		Issue    string `json:"issue"`
		Worktree string `json:"worktree"`
		Role     string `json:"role"`
		Text     string `json:"text"`
		Lines    int    `json:"lines"`
		All      bool   `json:"all"`
	}
	run := func(f func(context.Context, *agents.Manager, args) (string, error)) func(context.Context, json.RawMessage) (string, error) {
		return func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a args
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("bad arguments: %w", err)
			}
			mgr, used, err := m.get(ctx)
			if err != nil {
				return "", err
			}
			out, err := f(ctx, mgr, a)
			return out, m.check(used, err)
		}
	}
	return []mcp.Tool{
		{Name: "spawn", Description: "Start a worker agent in a new hc terminal, in its worktree. It reports back with hc send when its turn ends.",
			Schema: schema([]string{"name", "profile", "task"}, map[string]prop{
				"name": str("Unique worker name: 1-32 of a-z 0-9 - _ (for example b-158)."), "profile": str("builder, planner, reviewer or debugger, or a profile from the profiles file."),
				"task": str("The self-contained brief: the first message."), "issue": str("Issue number."), "worktree": str("The worker's working folder (its git worktree)."), "role": str("Role label; the profile name by default.")}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				w, err := mgr.Spawn(ctx, agents.Spec{Name: a.Name, Profile: a.Profile, Task: a.Task, Issue: a.Issue, Worktree: a.Worktree, Role: a.Role, Parent: parent})
				if err != nil {
					return "", err
				}
				return toJSON(w)
			})},
		{Name: "message", Description: "Type a message into a worker and press Enter. It keeps the worker's context.",
			Schema: schema([]string{"name", "text"}, map[string]prop{"name": str("Worker name."), "text": str("The message.")}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				return "sent", mgr.Send(ctx, a.Name, a.Text)
			})},
		{Name: "interrupt", Description: "Press a worker's stop key to end its turn; it keeps its context. Returns hc's outcome.",
			Schema: schema([]string{"name"}, map[string]prop{"name": str("Worker name.")}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				return mgr.Interrupt(ctx, a.Name)
			})},
		{Name: "stop", Description: "End a worker's program. Its context is lost.",
			Schema: schema([]string{"name"}, map[string]prop{"name": str("Worker name.")}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				return "stopped", mgr.Stop(ctx, a.Name)
			})},
		{Name: "read", Description: "Read a worker's screen and history.",
			Schema: schema([]string{"name"}, map[string]prop{"name": str("Worker name."), "lines": {"integer", "Only the last lines."}}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				return mgr.Read(ctx, a.Name, a.Lines)
			})},
		{Name: "list", Description: "List the workers with their role, issue, state and worktree.",
			Schema: schema(nil, map[string]prop{"all": {"boolean", "Include finished workers."}}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				ws, err := mgr.List(ctx, a.All)
				if err != nil {
					return "", err
				}
				return toJSON(ws)
			})},
	}
}
