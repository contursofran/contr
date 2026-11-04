// Package agentstate is the one source the agent views read: the workers from
// hc, and the questions and to-dos from owner.json. It does its I/O on
// goroutines and tells the UI through a notify function; there are no timers.
package agentstate

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/owner"
)

// Snapshot is a copy of the state at one moment.
type Snapshot struct {
	Orchestrators []agents.Worker  // role orchestrator, any status, by project then name
	Workers       []agents.Worker  // the rest, by name
	Terminals     []agents.Worker  // plain terminals (role terminal), by project then name
	Questions     []owner.Question // open only, in asked order
	Todos         []owner.Todo     // open only, in file order
	Resolved      []string         // ids resolved in this run, until ClearDone
	Problem       string           // last error of hc or owner.json; empty when fine
}

// Model holds the state. Its methods are safe from any goroutine.
type Model struct {
	mgr    *agents.Manager
	store  *owner.Store
	notify func()

	mu       sync.Mutex
	workers  []agents.Worker
	state    owner.State
	resolved []string
	hcProb   string // last hc error
	fileProb string // last owner.json error
	actProb  string // last answer or done error
	// resolvedItems keeps what was resolved, so the views can strike it
	// through until ClearDone.
	resolvedQ []owner.Question
	resolvedT []owner.Todo

	wg sync.WaitGroup
}

// New returns a model. notify runs after each change, on a goroutine of the
// model; it must be cheap (Window.Update or Invalidate).
func New(m *agents.Manager, store *owner.Store, notify func()) *Model {
	if notify == nil {
		notify = func() {}
	}
	return &Model{mgr: m, store: store, notify: notify}
}

// Start follows hc and owner.json until ctx ends. Stop waits for both.
func (m *Model) Start(ctx context.Context) {
	m.reload() // the first read does not wait for an event
	m.wg.Add(2)
	go func() {
		defer m.wg.Done()
		err := m.mgr.Watch(ctx, func(ws []agents.Worker) {
			m.mu.Lock()
			m.workers, m.hcProb = ws, ""
			m.mu.Unlock()
			m.notify()
		})
		if err != nil && ctx.Err() == nil {
			m.mu.Lock()
			m.hcProb = "hc: " + err.Error()
			m.mu.Unlock()
			m.notify()
		}
	}()
	go func() {
		defer m.wg.Done()
		err := watchDir(ctx, filepath.Dir(m.store.Path), m.reload)
		if err != nil && ctx.Err() == nil {
			m.mu.Lock()
			m.fileProb = err.Error()
			m.mu.Unlock()
			m.notify()
		}
	}()
}

// Wait blocks until the goroutines of Start have ended; cancel the context
// first.
func (m *Model) Wait() { m.wg.Wait() }

// reload reads owner.json. A bad file keeps the last good items.
func (m *Model) reload() {
	st, err := m.store.Read()
	m.mu.Lock()
	if err != nil {
		m.fileProb = err.Error()
	} else {
		m.state, m.fileProb = st, ""
	}
	m.mu.Unlock()
	m.notify()
}

// Snapshot returns a copy of the state.
func (m *Model) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s Snapshot
	for _, w := range m.workers {
		switch {
		case w.Orchestrator():
			s.Orchestrators = append(s.Orchestrators, w)
		case w.Plain():
			s.Terminals = append(s.Terminals, w)
		default:
			s.Workers = append(s.Workers, w)
		}
	}
	byProjectName := func(a, b agents.Worker) int {
		if c := strings.Compare(a.Project, b.Project); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	}
	slices.SortStableFunc(s.Orchestrators, byProjectName)
	slices.SortStableFunc(s.Terminals, byProjectName)
	s.Questions, s.Todos = m.state.Open("")
	// An item resolved here is still shown, struck through, until ClearDone.
	for _, q := range m.resolvedQ {
		if !slices.ContainsFunc(s.Questions, func(o owner.Question) bool { return o.ID == q.ID }) {
			s.Questions = append(s.Questions, q)
		}
	}
	for _, t := range m.resolvedT {
		if !slices.ContainsFunc(s.Todos, func(o owner.Todo) bool { return o.ID == t.ID }) {
			s.Todos = append(s.Todos, t)
		}
	}
	s.Resolved = slices.Clone(m.resolved)
	for _, p := range []string{m.actProb, m.hcProb, m.fileProb} {
		if p != "" {
			s.Problem = p
			break
		}
	}
	return s
}

// Answer records the owner's answer to a question and sends it to the
// coordinator that asked. A question without a coordinator is only saved. If
// the send fails the answer stays saved and Problem says so.
func (m *Model) Answer(id, answer string) error {
	st, err := m.store.Read()
	if err != nil {
		return err
	}
	var q owner.Question
	i := slices.IndexFunc(st.Questions, func(q owner.Question) bool { return q.ID == id })
	if i >= 0 {
		q = st.Questions[i]
	}
	if err := m.store.Decide([]string{id}, answer, "owner"); err != nil {
		return err
	}
	m.mu.Lock()
	m.resolved = append(m.resolved, id)
	m.resolvedQ = append(m.resolvedQ, q)
	m.mu.Unlock()
	m.reload()
	return m.send(q.Coordinator, fmt.Sprintf("Owner answered %s: %s\n", id, answer))
}

// Done marks a to-do done and tells the coordinator that pinned it.
func (m *Model) Done(id string) error {
	st, err := m.store.Read()
	if err != nil {
		return err
	}
	var t owner.Todo
	if i := slices.IndexFunc(st.Todos, func(t owner.Todo) bool { return t.ID == id }); i >= 0 {
		t = st.Todos[i]
	}
	if err := m.store.Unpin([]string{id}); err != nil {
		return err
	}
	m.mu.Lock()
	m.resolved = append(m.resolved, id)
	m.resolvedT = append(m.resolvedT, t)
	m.mu.Unlock()
	m.reload()
	return m.send(t.Coordinator, fmt.Sprintf("Owner did %s: %s\n", id, t.Text))
}

// send writes text to a coordinator terminal. The file is already written, so
// a failure sets Problem and is also returned.
func (m *Model) send(coordinator, text string) error {
	if coordinator == "" {
		return nil
	}
	if err := m.mgr.Send(context.Background(), coordinator, text); err != nil {
		m.mu.Lock()
		m.actProb = "answer saved; coordinator not reached: " + err.Error()
		m.mu.Unlock()
		m.notify()
		return err
	}
	m.mu.Lock()
	m.actProb = ""
	m.mu.Unlock()
	return nil
}

// ClearDone forgets the items resolved in this run.
func (m *Model) ClearDone() {
	m.mu.Lock()
	m.resolved, m.resolvedQ, m.resolvedT, m.actProb = nil, nil, nil, ""
	m.mu.Unlock()
	m.notify()
}

// Read returns the last lines of a worker's terminal.
func (m *Model) Read(name string, lines int) (string, error) {
	return m.mgr.Read(context.Background(), name, lines)
}

// SendKeys presses raw keys in a worker's terminal, with no Enter.
func (m *Model) SendKeys(name string, keys []byte) error {
	return m.mgr.SendKeys(context.Background(), name, keys)
}

// Interrupt sends the interrupt key to a worker's terminal.
func (m *Model) Interrupt(name string) error {
	_, err := m.mgr.Interrupt(context.Background(), name)
	return err
}

// Stop closes a worker's terminal.
func (m *Model) Stop(name string) error {
	return m.mgr.Stop(context.Background(), name)
}

// Close stops a terminal and removes it from hc, so the watch drops it. A
// stopped terminal stays in hc as finished; Close is for a plain terminal
// that has nothing to keep. It blocks: call it off the UI thread.
func (m *Model) Close(name string) error {
	ctx := context.Background()
	if err := m.mgr.Stop(ctx, name); err != nil {
		return err
	}
	return m.mgr.Remove(ctx, name)
}
