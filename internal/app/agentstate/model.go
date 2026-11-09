// Package agentstate is the one source the agent views read: the workers from
// hc, and the questions and to-dos from owner.json. It does its I/O on
// goroutines and tells the UI through a notify function; there are no timers,
// except the retry wait while hc is offline and the window shows (#559).
package agentstate

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/owner"
)

// The wait after a failed watch: retryBase, doubled by each further failure,
// at most retryMax, as D79. Variables so tests can shorten them.
var (
	retryBase = 500 * time.Millisecond
	retryMax  = 5 * time.Second
)

// actionLimit is the time limit of one action of the window on hc (D223):
// Read, Send, SendKeys, Interrupt, Stop, Close and the message to a
// coordinator. It includes a dial when the connection ended. 10 s, as the
// hooks. A variable so tests can shorten it.
var actionLimit = 10 * time.Second

// action returns a context that ends after actionLimit.
func action() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), actionLimit)
}

// retryDelay is the wait after failures failed watches in a row.
func retryDelay(failures int) time.Duration {
	d := retryBase
	for range failures {
		d *= 2
		if d >= retryMax {
			return retryMax
		}
	}
	return min(d, retryMax)
}

// Snapshot is a copy of the state at one moment.
type Snapshot struct {
	Orchestrators []agents.Worker  // role orchestrator, any status, by project then name
	Workers       []agents.Worker  // the rest, by name
	Terminals     []agents.Worker  // plain terminals (role terminal), by project then name
	Questions     []owner.Question // open only, in asked order
	Todos         []owner.Todo     // open only, in file order
	Resolved      []string         // ids resolved in this run, until ClearDone
	Problem       string           // last error of hc or owner.json; empty when fine
	// OwnerProblem is the last error of owner.json alone; empty when the file
	// reads fine. The questions notch hides while it is set.
	OwnerProblem string
	// CoreVersion is the hc core's version while the model follows it;
	// empty after an hc error.
	CoreVersion string
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
	version  string // the core's version, from SetCoreVersion
	fileProb string // last owner.json error
	readProb string // last error of reading owner.json, cleared by the next good read
	actProb  string // last answer or done error
	// resolvedItems keeps what was resolved, so the views can strike it
	// through until ClearDone.
	resolvedQ []owner.Question
	resolvedT []owner.Todo

	// projects caches the project name of each folder of a foreign terminal;
	// asked holds the folders that are being resolved (D149).
	projects map[string][2]string // name and key
	asked    map[string]bool
	resolve  func(dir string) (name, key string) // reads git; tests replace it

	// orchNums and termNums keep the numbers of unnamed foreign terminals (D156).
	orchNums, termNums Numbering

	// hidden is true while the window does not show: no retry timer runs
	// (D9). tryNow asks the watch loop for a try at once. kick wakes the
	// loop when one of them changes.
	hidden bool
	tryNow bool
	kick   chan struct{}

	wg sync.WaitGroup
}

// ProjectOf gives the project name of a working folder: the folder name of
// the git repository that holds it (as the sidebar names it), else the name
// of the folder. The key is the git common folder (the sidebar's project key),
// or "" outside git. It reads files: call it only from a goroutine.
func ProjectOf(dir string) (name, key string) {
	if dir == "" {
		return "", ""
	}
	if c, ok := git.Discover(dir); ok {
		return filepath.Base(filepath.Dir(c.CommonDir)), c.CommonDir
	}
	return filepath.Base(dir), ""
}

// SameProject tells if w is in the project with this name and key. Names must
// match. Keys must match too when both sides have one, so two repos with the
// same folder name stay apart.
func SameProject(w agents.Worker, name, key string) bool {
	return w.Project == name && (key == "" || w.ProjectKey == "" || key == w.ProjectKey)
}

// resolveProjects starts the lookup of the project of each foreign terminal
// whose folder is not cached yet. The lookups run on one goroutine, which
// notifies when it has new names.
func (m *Model) resolveProjects(ws []agents.Worker) {
	m.mu.Lock()
	var dirs []string
	for _, w := range ws {
		if _, ok := m.projects[w.Worktree]; w.Worktree != "" && !ok && !m.asked[w.Worktree] && !slices.Contains(dirs, w.Worktree) {
			dirs = append(dirs, w.Worktree)
		}
	}
	if len(dirs) == 0 {
		m.mu.Unlock()
		return
	}
	if m.asked == nil {
		m.asked = map[string]bool{}
	}
	for _, d := range dirs {
		m.asked[d] = true
	}
	resolve := m.resolve
	m.mu.Unlock()
	go func() {
		got := make(map[string][2]string, len(dirs))
		for _, d := range dirs {
			n, k := resolve(d)
			got[d] = [2]string{n, k}
		}
		m.mu.Lock()
		if m.projects == nil {
			m.projects = map[string][2]string{}
		}
		for d, p := range got {
			m.projects[d] = p
			delete(m.asked, d)
		}
		m.mu.Unlock()
		m.notify()
	}()
}

// New returns a model. notify runs after each change, on a goroutine of the
// model; it must be cheap (Window.Update or Invalidate).
func New(m *agents.Manager, store *owner.Store, notify func()) *Model {
	if notify == nil {
		notify = func() {}
	}
	return &Model{mgr: m, store: store, notify: notify, resolve: ProjectOf, orchNums: Numbering{}, termNums: Numbering{}, kick: make(chan struct{}, 1)}
}

// poke wakes the watch loop when it waits. It does not block.
func (m *Model) poke() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// SetVisible tells the model whether the window shows. A hidden window has
// no retry timer (D9); a window that shows again while hc is offline tries
// at once. It does not block.
func (m *Model) SetVisible(visible bool) {
	m.mu.Lock()
	if !m.hidden == visible {
		m.mu.Unlock()
		return
	}
	m.hidden = !visible
	if visible && m.hcProb != "" {
		m.tryNow = true
	}
	m.mu.Unlock()
	m.poke()
}

// Reconnect tries to follow hc again at once while it is offline. Online,
// or while the first try still runs, it does nothing. It does not block.
func (m *Model) Reconnect() {
	m.mu.Lock()
	if m.hcProb == "" {
		m.mu.Unlock()
		return
	}
	m.tryNow = true
	m.mu.Unlock()
	m.poke()
}

// waitRetry waits d before the next try of the watch. A try asked for with
// tryNow comes at once. While the window is hidden no timer runs. It returns
// false when ctx ends.
func (m *Model) waitRetry(ctx context.Context, d time.Duration) bool {
	for {
		m.mu.Lock()
		now, hidden := m.tryNow, m.hidden
		m.tryNow = false
		m.mu.Unlock()
		if now {
			return true
		}
		var fire <-chan time.Time
		var timer *time.Timer
		if !hidden {
			timer = time.NewTimer(d)
			fire = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return false
		case <-m.kick:
			if timer != nil {
				timer.Stop()
			}
		case <-fire:
			return true
		}
	}
}

// SetCoreVersion sets the version of the hc core that the model follows.
func (m *Model) SetCoreVersion(v string) {
	m.mu.Lock()
	m.version = v
	m.mu.Unlock()
}

// Start follows hc and owner.json until ctx ends. Stop waits for both.
func (m *Model) Start(ctx context.Context) {
	m.reload() // the first read does not wait for an event
	m.wg.Add(2)
	go func() {
		defer m.wg.Done()
		m.watchLoop(ctx)
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

// watchLoop follows hc until ctx ends. After a failed watch it shows the
// problem, waits (waitRetry) and watches again; a new watch subscribes again
// and, through agents.Redial, dials again when the connection ended (#559).
// The last list stays while hc is offline.
func (m *Model) watchLoop(ctx context.Context) {
	failures := 0 // only this goroutine uses it: fn runs on it too
	for {
		err := m.mgr.WatchAll(ctx, func(ws []agents.Worker) {
			failures = 0 // D79: data resets the backoff
			m.mu.Lock()
			m.workers, m.hcProb, m.tryNow = ws, "", false
			m.mu.Unlock()
			m.resolveProjects(ws)
			m.notify()
		})
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = hc.ErrClosed
		}
		prob := "hc: " + err.Error()
		m.mu.Lock()
		if m.hcProb != prob {
			log.Printf("agentstate: %s", prob)
		}
		m.hcProb = prob
		m.mu.Unlock()
		m.notify()
		if !m.waitRetry(ctx, retryDelay(failures)) {
			return
		}
		failures++
	}
}

// Wait blocks until the goroutines of Start have ended; cancel the context
// first.
func (m *Model) Wait() { m.wg.Wait() }

// reload reads owner.json. A bad file keeps the last good items.
func (m *Model) reload() {
	st, err := m.store.Read()
	m.mu.Lock()
	if err != nil {
		if m.readProb != err.Error() {
			log.Printf("agentstate: %v", err)
		}
		m.fileProb, m.readProb = err.Error(), err.Error()
	} else {
		m.state, m.fileProb, m.readProb = st, "", ""
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
		if p, ok := m.projects[w.Worktree]; ok { // the cache has them once resolved
			if w.Foreign && w.Project == "" {
				w.Project = p[0]
			}
			w.ProjectKey = p[1]
		}
		switch {
		case w.Orchestrator():
			s.Orchestrators = append(s.Orchestrators, w)
		case w.Plain():
			s.Terminals = append(s.Terminals, w)
		default:
			s.Workers = append(s.Workers, w)
		}
	}
	for i, w := range s.Workers {
		if usableTitle(w.Title, w) {
			s.Workers[i].Task = cleanTitle(w.Title)
		}
	}
	byProjectName := func(a, b agents.Worker) int {
		if c := strings.Compare(a.Project, b.Project); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	}
	nameForeign(s.Orchestrators, m.orchNums)
	nameForeign(s.Terminals, m.termNums)
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
	s.OwnerProblem = m.readProb
	if m.hcProb == "" {
		s.CoreVersion = m.version
	}
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
	ctx, cancel := action()
	defer cancel()
	if err := m.mgr.Send(ctx, coordinator, text); err != nil {
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
	ctx, cancel := action()
	defer cancel()
	return m.mgr.Read(ctx, name, lines)
}

// Send types text into a worker's terminal and presses Enter.
func (m *Model) Send(name, text string) error {
	ctx, cancel := action()
	defer cancel()
	return m.mgr.Send(ctx, name, text)
}

// SendKeys presses raw keys in a worker's terminal, with no Enter.
func (m *Model) SendKeys(name string, keys []byte) error {
	ctx, cancel := action()
	defer cancel()
	return m.mgr.SendKeys(ctx, name, keys)
}

// Interrupt sends the interrupt key to a worker's terminal.
func (m *Model) Interrupt(name string) error {
	ctx, cancel := action()
	defer cancel()
	_, err := m.mgr.Interrupt(ctx, name)
	return err
}

// Stop ends a worker's program and keeps the finished terminal.
func (m *Model) Stop(name string) error {
	ctx, cancel := action()
	defer cancel()
	return m.mgr.Stop(ctx, name, true)
}

// Close stops a terminal and removes it from hc, so the watch drops it. It
// blocks: call it off the UI thread.
func (m *Model) Close(name string) error {
	ctx, cancel := action()
	defer cancel()
	return m.mgr.Stop(ctx, name, false)
}

// LiveWorkersOf is the number of live workers whose parent is addr.
func (s Snapshot) LiveWorkersOf(addr string) int {
	n := 0
	for _, w := range s.Workers {
		if w.Live && addr != "" && w.Parent == addr {
			n++
		}
	}
	return n
}

// Orchestrating tells whether the Claude session w orchestrates: it ran
// /contr:orchestrate (the hook's label) or it has live workers (D191).
func (s Snapshot) Orchestrating(w agents.Worker) bool {
	return w.Orchestrating || s.LiveWorkersOf(w.Address) > 0
}
