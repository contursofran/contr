package app

// The projects modal (D207, docs/spec/ProjectsModal.md): a palette with a
// search field and one list of the open and recent projects. The owner drags
// an open row to change the sidebar order and hides a project in contr only
// (D70). Each change goes to the projects store at once and is saved to
// projects.json on a goroutine (D63). Esc or a click outside closes it.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/projects"
)

// Labels of the projects modal.
const (
	LabelProjects       = "projects modal"
	LabelProjectsList   = "projects list"
	LabelProjectsFooter = "projects footer"
	LabelProjectsSearch = "Search projects, or paste a GitHub repo or a path"
	ProjectsHide        = "Hide"
	ProjectsOpenFolder  = "Open folder…"
	LabelCloneRemote    = "Clone from GitHub"
	ProjectsClone       = "Clone"
	ProjectsCloneFolder = "Choose folder…"
)

// ProjectsRow is the label of a project row; ProjectsHideOf is its Hide
// button.
func ProjectsRow(name string) string    { return "project " + name }
func ProjectsHideOf(name string) string { return ProjectsHide + " " + name }

// ProjectsNoMatch is the text of an empty list under a filter.
func ProjectsNoMatch(q string) string {
	return "No project matches \"" + q + "\". Paste a GitHub repo or a path to add one."
}

// ProjectsModal is the modal. Its methods run on the UI thread.
type ProjectsModal struct {
	// Groups returns the open projects in the sidebar order.
	Groups func() []spaces.RepoGroup
	// Store returns the projects store, or nil while it loads.
	Store func() *projects.Store
	// Apply tells the sidebar that the store changed.
	Apply func()
	// Save writes a copy of the store. It runs on a goroutine; nil saves nothing.
	Save func(*projects.Store) error
	Run  Runner
	// PickFolder asks the owner for a folder. It blocks, so it runs on a
	// goroutine, and returns ErrPickCancelled for no choice. Nil hides
	// `Open folder…` (D116).
	PickFolder func() (string, error)
	// Resolve returns the project key of a folder: its git common dir, or the
	// folder when it is outside git. It reads files, so it runs on a goroutine.
	Resolve func(path string) string
	// CloneParent is the folder that holds a clone when no project was opened
	// before (D117); otherwise the parent folder of the last opened project
	// is the default. Empty hides the clone part.
	CloneParent string
	// PickParent asks the owner for the folder that holds a clone, starting
	// at def. It blocks and returns ErrPickCancelled for no choice. Nil hides
	// `Choose folder…`.
	PickParent func(def string) (string, error)
	// CloneRun runs git for a clone; nil runs the program (git.ExecClone).
	CloneRun git.CloneRunner
	// Now gives the time of an open; nil uses time.Now.
	Now func() time.Time
	// Status shows an error; may be nil.
	Status func(string)
	// Invalidate redraws the window; may be nil.
	Invalidate func()
	// Switch selects the open project with this common dir in the sidebar;
	// may be nil.
	Switch func(dir string)

	open  bool
	query string
	// sel is the selected row, an index into rows(). hover is the row the
	// pointer entered last; it moves sel only when it changes.
	sel, hover int
	// moved is true after a key moved sel: the list then scrolls to it.
	moved bool
	// remote is the text of the clone field; parent is the folder the owner
	// picked ("" means CloneParent); cloning is true while git runs; cloneErr
	// is the text of the last failure.
	remote string
	parent string
	// defParent is the kept default parent; "" means CloneParent.
	defParent string
	cloning   bool
	cloneErr  string
	// cancelClone stops the running clone; the modal calls it when it closes.
	cancelClone context.CancelFunc
	list        ui.ListState
	// saving is true while a save runs; next is the newest copy not yet
	// saved, so the saves go in order and the last change wins.
	saving bool
	next   *projects.Store
}

// Toggle opens the modal, or closes it when it is open. The menu item and its
// shortcut call this; a pill in the Spaces header can call it too.
func (m *ProjectsModal) Toggle() {
	m.open = !m.open
	if !m.open {
		m.stopClone()
	} else {
		m.refreshDefault()
	}
	m.query, m.sel, m.hover = "", 0, -1
	m.redraw()
}

// Close hides the modal.
func (m *ProjectsModal) Close() {
	m.open = false
	m.stopClone()
	m.redraw()
}

// Opened tells whether the modal shows.
func (m *ProjectsModal) Opened() bool { return m.open }

// stopClone cancels a running clone. Its result then adds nothing.
func (m *ProjectsModal) stopClone() {
	if m.cancelClone != nil {
		m.cancelClone()
	}
}

// defaultParent is the folder that holds a clone when the owner picked none
// (D117). It returns the kept value, so the view does no file check; refreshDefault
// keeps it.
func (m *ProjectsModal) defaultParent() string {
	if m.defParent != "" {
		return m.defParent
	}
	return m.CloneParent
}

// refreshDefault works out the default parent on a goroutine (D117): the parent
// folder of the newest recent project, when it exists; else CloneParent. It
// runs when the modal opens and when the store changes.
func (m *ProjectsModal) refreshDefault() {
	recent := ""
	if st := m.store(); st != nil {
		if rec := st.Recent(); len(rec) > 0 {
			recent = rec[0].Path
		}
	}
	if recent == "" { // nothing to check: no file read
		m.defParent = ""
		return
	}
	m.Run.Do(func() func() {
		def := ""
		p := filepath.Dir(recent)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			def = p
		}
		return func() {
			if def != m.defParent {
				m.defParent = def
				m.redraw()
			}
		}
	})
}

func (m *ProjectsModal) redraw() {
	if m.Invalidate != nil {
		m.Invalidate()
	}
}

// text is the trimmed text of the field.
func (m *ProjectsModal) text() string { return strings.TrimSpace(m.query) }

// shown returns the open groups whose name has the text of the field.
func (m *ProjectsModal) shown() []spaces.RepoGroup {
	groups := m.groupsAll()
	f := m.text()
	if f == "" {
		return groups
	}
	var out []spaces.RepoGroup
	for _, g := range groups {
		if _, _, ok := matchSpan(g.Name, f); ok {
			out = append(out, g)
		}
	}
	return out
}

// reorder moves rows of the open list before row to. It does nothing while the
// filter hides rows, since the row numbers then differ from the stored order.
func (m *ProjectsModal) reorder(rows []int, to int) {
	st := m.store()
	if st == nil || m.text() != "" {
		return
	}
	groups := m.Groups()
	dirs := make([]string, len(groups))
	for i, g := range groups {
		dirs[i] = g.CommonDir
	}
	st.SetOrder(dirs)
	st.Move(rows, to)
	m.changed()
}

// listReorder maps a drag in the list to the open rows: the list has the
// `Open` header first, so its row i is open row i-1. A drag of another row,
// or a drop in Recent, does nothing.
func (m *ProjectsModal) listReorder(rows []int, to int) {
	n := len(m.groupsAll())
	if to < 1 || to > 1+n {
		if to < 1 {
			to = 1
		} else {
			return
		}
	}
	open := make([]int, 0, len(rows))
	for _, r := range rows {
		if r < 1 || r > n {
			return
		}
		open = append(open, r-1)
	}
	m.reorder(open, to-1)
}

// hide hides the project in contr only (D70).
func (m *ProjectsModal) hide(dir string) {
	st := m.store()
	if st == nil {
		return
	}
	st.Hide(dir)
	m.changed()
}

// openFolder asks for a folder on a goroutine, then adds it to the store and
// to the recent list (D116). No herdr workspace and no terminal start (owner
// comment, #245). A cancelled dialog does nothing.
func (m *ProjectsModal) openFolder() {
	if m.PickFolder == nil {
		return
	}
	m.Run.Do(func() func() {
		path, err := m.PickFolder()
		dir := path
		if err == nil && m.Resolve != nil {
			dir = m.Resolve(path)
		}
		return func() {
			switch {
			case errors.Is(err, ErrPickCancelled):
			case err != nil:
				m.status("open folder: " + err.Error())
			default:
				m.add(dir, path)
			}
		}
	})
}

// cloneDest returns the clone URL and the destination folder for the field.
// The URL and the folder are empty while the field is empty; err is set when
// the field is not a remote.
func (m *ProjectsModal) cloneDest() (url, dest string, err error) {
	if strings.TrimSpace(m.remote) == "" {
		return "", "", nil
	}
	url, name, err := git.ParseRemote(m.remote)
	if err != nil {
		return "", "", err
	}
	parent := m.parent
	if parent == "" {
		parent = m.defaultParent()
	}
	return url, filepath.Join(parent, name), nil
}

// chooseParent asks for the folder that holds the clone, on a goroutine.
func (m *ProjectsModal) chooseParent() {
	if m.PickParent == nil || m.cloning {
		return
	}
	def := m.parent
	if def == "" {
		def = m.defaultParent()
	}
	m.Run.Do(func() func() {
		path, err := m.PickParent(def)
		return func() {
			switch {
			case errors.Is(err, ErrPickCancelled):
			case err != nil:
				m.status("choose folder: " + err.Error())
			default:
				m.parent = path
				m.redraw()
			}
		}
	})
}

// clone runs git clone on a goroutine. On success it adds the folder to the
// store and to the recent list like open folder does (D117); the project then
// shows as an empty group with `+ new` (D116). A failure shows in the modal.
func (m *ProjectsModal) clone() {
	if m.cloning {
		return
	}
	url, dest, err := m.cloneDest()
	if err != nil || url == "" {
		m.cloneErr = "enter owner/name or a URL"
		if err != nil {
			m.cloneErr = "not a repository: " + strings.TrimSpace(m.remote)
		}
		m.redraw()
		return
	}
	m.cloning, m.cloneErr = true, ""
	m.redraw()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelClone = cancel
	m.Run.Do(func() func() {
		err := git.Clone(ctx, m.CloneRun, url, dest)
		key := dest
		if err == nil && m.Resolve != nil {
			key = m.Resolve(dest)
		}
		return func() {
			m.cloning = false
			stopped := ctx.Err() != nil // the modal closed: add nothing
			cancel()
			if stopped {
				return
			}
			if err != nil {
				m.cloneErr = cloneMessage(err, dest)
				m.redraw()
				return
			}
			m.remote, m.parent = "", ""
			m.add(key, dest)
		}
	})
}

// cloneMessage turns a clone error into the text of the modal.
func cloneMessage(err error, dest string) string {
	switch {
	case errors.Is(err, git.ErrDestExists):
		return "folder exists: " + dest
	case errors.Is(err, git.ErrGitNotFound):
		return "git is not installed"
	}
	msg := err.Error()
	for _, k := range []string{"Authentication failed", "could not read Username", "Permission denied", "not found"} {
		if strings.Contains(msg, k) {
			return msg + " · check the name and your GitHub access"
		}
	}
	return msg
}

// add puts a project in the store, shows it and records it as recent.
func (m *ProjectsModal) add(dir, path string) {
	st := m.store()
	if st == nil {
		return
	}
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	// Fix the shown order first, so that the new project comes last.
	var dirs []string
	for _, g := range m.groupsAll() {
		dirs = append(dirs, g.CommonDir)
	}
	st.SetOrder(st.Order(dirs))
	st.AddFolder(dir, path)
	st.Touch(dir, path, now())
	m.changed()
}

// openRecent opens a project of the recent list again.
func (m *ProjectsModal) openRecent(e projects.Entry) { m.add(e.Dir, e.Path) }

func (m *ProjectsModal) status(s string) {
	if m.Status != nil {
		m.Status(s)
	}
}

// recent returns the recent entries that the sidebar does not show now.
func (m *ProjectsModal) recent() []projects.Entry {
	st := m.store()
	if st == nil {
		return nil
	}
	open := m.groupsAll()
	var out []projects.Entry
	for _, e := range st.Recent() {
		if !slices.ContainsFunc(open, func(g spaces.RepoGroup) bool { return g.CommonDir == e.Dir }) {
			out = append(out, e)
		}
	}
	return out
}

func (m *ProjectsModal) store() *projects.Store {
	if m.Store == nil {
		return nil
	}
	return m.Store()
}

// changed updates the sidebar and saves the store.
func (m *ProjectsModal) changed() {
	m.refreshDefault()
	if m.Apply != nil {
		m.Apply()
	}
	m.redraw()
	if m.Save == nil {
		return
	}
	m.next = m.store().Clone()
	if !m.saving {
		m.saving = true
		m.flush()
	}
}

// flush saves the newest copy on a goroutine, then the next one if a change
// came meanwhile.
func (m *ProjectsModal) flush() {
	snap := m.next
	m.next = nil
	m.Run.Do(func() func() {
		err := m.Save(snap)
		return func() {
			m.saving = false
			if err != nil && m.Status != nil {
				m.Status("projects not saved: " + err.Error())
			}
			if m.next != nil {
				m.saving = true
				m.flush()
			}
		}
	})
}

// cloneSection is the clone part: the field, the destination preview and the
// buttons. A busy state or the last error shows below.
func (m *ProjectsModal) cloneSection(c *ui.Context) {
	p := theme.Current()
	if m.CloneParent == "" {
		return
	}
	_, dest, perr := m.cloneDest()
	ui.Column(c).Shrink(0).Padding(10, theme.ModalPadX).Gap(6).
		BorderWidth(1, 0, 0, 0).BorderColor(p.Hairline).Children(func() {
		ui.Text(c, "clone from GitHub").TextColor(p.Ink50)
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			if ui.TextInput(c, &m.remote).Label(LabelCloneRemote).Placeholder("owner/name or a URL").Grow(1).MinWidth(0).
				Submitted() {
				m.clone()
			}
			if m.PickParent != nil {
				cardButton(c, ProjectsCloneFolder, ButtonOutlined, p.Ink95, m.chooseParent)
			}
			label := ProjectsClone
			if m.cloning {
				label = "Cloning…"
			}
			cardButton(c, label, ButtonOutlined, p.Ink95, m.clone)
		})
		switch {
		case m.cloneErr != "":
			ui.Text(c, m.cloneErr).TextColor(p.Blocked)
		case perr != nil:
			ui.Text(c, "not a repository yet").TextColor(p.Ink50)
		case dest != "":
			ui.Text(c, "→ "+dest).SingleLine().MinWidth(0).TextColor(p.Ink62)
		}
	})
}
