package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/contursofran/contr"
	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/mcp"
	"github.com/contursofran/contr/internal/owner"
)

// managers gives the agents manager for a tool call. It connects to the core
// on the first call, and again after the connection ended.
type managers struct {
	mu     sync.Mutex
	dial   func(context.Context, func(string) string) (agentsCore, error)
	getenv func(string) string
	cfg    agents.Config
	// load, when set, reads the profiles again for each spawn, so a profile
	// added while the session runs works at once (#257).
	load func() (agents.Config, error)
	core agentsCore
}

// spawnManager is a manager with the profiles read now.
func (m *managers) spawnManager(mgr *agents.Manager) (*agents.Manager, error) {
	if m.load == nil {
		return mgr, nil
	}
	cfg, err := m.load()
	if err != nil {
		return nil, err
	}
	return mgr.WithConfig(cfg), nil
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
	return agents.New(m.core, m.cfg).WithEnv(m.getenv), m.core, nil
}

// do runs f with a manager. When the connection had already ended before f
// sent anything (hc.ErrNotSent), the core never got the call, so do connects
// again and runs f once more. A connection that ends while a call waits for
// its reply is not retried: the core may have done the call.
func (m *managers) do(ctx context.Context, f func(*agents.Manager) (string, error)) (string, error) {
	for try := 0; ; try++ {
		mgr, used, err := m.get(ctx)
		if err != nil {
			return "", err
		}
		out, err := f(mgr)
		err = m.check(used, err)
		if try == 0 && errors.Is(err, hc.ErrNotSent) {
			continue
		}
		return out, err
	}
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
	load := func() (agents.Config, error) {
		cfg, err := agents.LoadConfig(configPath(getenv))
		if kit := getenv("CONTR_KIT"); kit != "" {
			cfg.Kit = kit
		}
		return cfg, err
	}
	cfg, err := load()
	if err != nil {
		fmt.Fprintf(stderr, "contr mcp: %v\n", err)
		return 1
	}
	m := &managers{dial: dial, getenv: getenv, cfg: cfg, load: load}
	defer m.close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := ownerStore(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "contr mcp: %v\n", err)
		return 1
	}
	tools := append(agentTools(m, getenv("HC_TERMINAL_ID")), ownerTools(store, projectName(getenv), getenv("HC_TERMINAL_ID"))...)
	srv := mcp.New("contr", contr.Version(), tools)
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
		if pr.typ == "string[]" {
			p[name] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": pr.desc}
			continue
		}
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
		TaskFile string `json:"task_file"`
		Issue    string `json:"issue"`
		Worktree string `json:"worktree"`
		Project  string `json:"project"`
		Role     string `json:"role"`
		Text     string `json:"text"`
		Lines    int    `json:"lines"`
		All      bool   `json:"all"`
		Keep     bool   `json:"keep"`
		ReviewPR int    `json:"review_pr"`
	}
	run := func(f func(context.Context, *agents.Manager, args) (string, error)) func(context.Context, json.RawMessage) (string, error) {
		return func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a args
			// An unknown argument is an error, so a misnamed field (message
			// for text) does not become an empty value (#483, D200).
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&a); err != nil {
				return "", fmt.Errorf("bad arguments: %w", err)
			}
			return m.do(ctx, func(mgr *agents.Manager) (string, error) { return f(ctx, mgr, a) })
		}
	}
	return []mcp.Tool{
		{Name: "spawn", Description: "Start a worker agent in a new hc terminal, in its worktree. It reports back with hc send when its turn ends.",
			Schema: schema([]string{"name", "profile"}, map[string]prop{
				"name": str("Unique worker name: 1-32 of a-z 0-9 - _ (for example b-158)."), "profile": str("builder, planner, reviewer or debugger, or a profile from the profiles file."),
				"task": str("The self-contained brief: the first message. Give task or task_file."), "task_file": str("An absolute path to a file that holds the brief, instead of task."), "issue": str("Issue number."), "worktree": str("The worker's working folder (its git worktree)."), "role": str("Role label; the profile name by default."), "project": str("Project name; groups the worker with its project's questions."),
				"review_pr": {"integer", "A pull request number: start the worker in a detached worktree at the PR's head, <main checkout>-wt/review-<N>, of the repository at worktree (default: the coordinator's folder)."}}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				mgr, err := m.spawnManager(mgr)
				if err != nil {
					return "", err
				}
				task, err := spawnTask(a.Task, a.TaskFile)
				if err != nil {
					return "", err
				}
				if strings.TrimSpace(task) == "" {
					return "", errors.New("give the brief as task or task_file")
				}
				dir := a.Worktree
				if a.ReviewPR != 0 {
					if dir, err = mgr.ReviewWorktree(ctx, cmp.Or(dir, "."), a.ReviewPR); err != nil {
						return "", err
					}
				}
				w, err := mgr.Spawn(ctx, agents.Spec{Name: a.Name, Profile: a.Profile, Task: task, Issue: a.Issue, Worktree: dir, Role: a.Role, Project: a.Project, Parent: parent, Colors: storedColors()})
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
		{Name: "stop", Description: "End a worker's program and remove its terminal from hc. Its context is lost. Read its RESULT.md first.",
			Schema: schema([]string{"name"}, map[string]prop{"name": str("Worker name."), "keep": {"boolean", "Keep the finished terminal so it can still be read. Default false."}}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				return "stopped", mgr.Stop(ctx, a.Name, a.Keep)
			})},
		{Name: "read", Description: "Read a worker's screen and history.",
			Schema: schema([]string{"name"}, map[string]prop{"name": str("Worker name."), "lines": {"integer", "Only the last lines."}}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				return mgr.Read(ctx, a.Name, a.Lines)
			})},
		{Name: "prompt", Description: "Show the menu a blocked worker waits on: for a permission, the tool and its command, then the question and the numbered choices. \"no prompt\" when it waits on none.",
			Schema: schema([]string{"name"}, map[string]prop{"name": str("Worker name.")}),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				return pendingPrompt(ctx, mgr, a.Name)
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
		{Name: "clear_ended", Description: "Remove the finished terminals of the workers this coordinator started. Other coordinators' workers and plain terminals stay. Returns the names removed.",
			Schema: schema(nil, nil),
			Run: run(func(ctx context.Context, mgr *agents.Manager, a args) (string, error) {
				names, err := mgr.RemoveEnded(ctx)
				if names == nil {
					names = []string{}
				}
				out, jerr := toJSON(names)
				if err != nil {
					return out, err
				}
				return out, jerr
			})},
	}
}

// ownerStore is the owner file: CONTR_OWNER_FILE, or owner.json in contr's
// user config folder.
func ownerStore(getenv func(string) string) (*owner.Store, error) {
	if p := getenv("CONTR_OWNER_FILE"); p != "" {
		return &owner.Store{Path: p}, nil
	}
	p, err := owner.DefaultPath()
	if err != nil {
		return nil, err
	}
	return &owner.Store{Path: p}, nil
}

// projectName is CONTR_PROJECT, or the name of the folder the session runs in.
func projectName(getenv func(string) string) string {
	if p := getenv("CONTR_PROJECT"); p != "" {
		return p
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Base(wd)
}

// ownerTools keep questions and to-dos for the owner in the owner file, for
// project. coordinator is the asking terminal: the UI sends answers there.
func ownerTools(store *owner.Store, project, coordinator string) []mcp.Tool {
	type args struct {
		Question    string   `json:"question"`
		Options     []string `json:"options"`
		Recommended *int     `json:"recommended"`
		Impacts     []string `json:"impacts"`
		Refs        []string `json:"refs"`
		Source      string   `json:"source"`
		Title       string   `json:"title"`
		From        string   `json:"from"`
		Text        string   `json:"text"`
		Kind        string   `json:"kind"`
		IDs         []string `json:"ids"`
		Answer      string   `json:"answer"`
		By          string   `json:"by"`
		All         bool     `json:"all"`
	}
	run := func(f func(args) (string, error)) func(context.Context, json.RawMessage) (string, error) {
		return func(_ context.Context, raw json.RawMessage) (string, error) {
			var a args
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", fmt.Errorf("bad arguments: %w", err)
			}
			return f(a)
		}
	}
	return []mcp.Tool{
		{Name: "ask_owner", Description: "Ask the owner a question only they can answer (a product rule, a design choice, an approval). It is kept until it is decided. Returns its id (D1, D2, …). contr shows it in the questions card, and sends the answer to you as `Owner answered <ID>: <answer>`. When the owner answers in chat, close it with decided.",
			Schema: schema([]string{"question"}, map[string]prop{
				"question": str("The question, with enough context to answer it from there alone."), "options": {"string[]", "Short answers to pick from (2-4)."},
				"recommended": {"integer", "Index in options of your recommendation."}, "title": str("A 2-4 word name."), "from": str("The worker, issue or PR it is about."),
				"impacts": {"string[]", "One line per option, in the order of options: what choosing it means. The sheet shows each under its option."},
				"refs":    {"string[]", "What helps the owner decide: `PR 12` (opens the PR pane), `#12` or `issue 12` (opens in the browser), an http or https URL, or a file path. At most a few."},
				"source":  str("A short excerpt (a few lines) of the text that raised the question, such as a worker's report.")}),
			Run: run(func(a args) (string, error) {
				q, err := store.Ask(owner.Question{Project: project, Coordinator: coordinator, From: a.From, Title: a.Title, Question: a.Question, Options: a.Options, Recommended: a.Recommended, Impacts: a.Impacts, Refs: a.Refs, Source: a.Source})
				return q.ID, err
			})},
		{Name: "decided", Description: "Close questions: ones the owner answered in chat (by owner), or ones you settled yourself because they are not the owner's (by coordinator).",
			Schema: schema([]string{"ids", "answer", "by"}, map[string]prop{"ids": {"string[]", "Question ids, such as D3."}, "answer": str("The answer, short."), "by": str("owner or coordinator.")}),
			Run: run(func(a args) (string, error) {
				return "decided", store.Decide(a.IDs, a.Answer, a.By)
			})},
		{Name: "pin_for_owner", Description: "Pin one line for the owner: something to do (kind do: test a build, merge a PR that waits for them) or to know (kind know). Returns its id (T1, T2, …).",
			Schema: schema([]string{"text"}, map[string]prop{"text": str("One short line with all it takes to act."), "kind": str("do (the default) or know."), "from": str("The worker, issue or PR it is about.")}),
			Run: run(func(a args) (string, error) {
				t, err := store.Pin(owner.Todo{Project: project, Coordinator: coordinator, From: a.From, Kind: a.Kind, Text: a.Text})
				return t.ID, err
			})},
		{Name: "unpin_for_owner", Description: "Remove pinned items once they are done or no longer needed.",
			Schema: schema([]string{"ids"}, map[string]prop{"ids": {"string[]", "Ids such as T3."}}),
			Run: run(func(a args) (string, error) {
				return "unpinned", store.Unpin(a.IDs)
			})},
		{Name: "owner_items", Description: "List the open questions and pinned items of this project (all projects with all).",
			Schema: schema(nil, map[string]prop{"all": {"boolean", "All projects."}}),
			Run: run(func(a args) (string, error) {
				st, err := store.Read()
				if err != nil {
					return "", err
				}
				p := project
				if a.All {
					p = ""
				}
				qs, ts := st.Open(p)
				return toJSON(map[string]any{"questions": qs, "todos": ts})
			})},
	}
}
