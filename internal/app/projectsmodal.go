package app

// The projects modal (D110): the open projects in the sidebar order. The
// owner drags a row to change the order and clicks `close` to hide a project
// in contr only (D70). Each change goes to the projects store at once and is
// saved to projects.json on a goroutine (D63). Esc, `esc` or `Done` closes it.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	LabelProjectsFilter = "Filter projects"
	ProjectsEsc         = "esc"
	ProjectsDone        = "Done"
	ProjectsClose       = "close"
	ProjectsOpenFolder  = "+ open folder…"
	LabelProjectsRecent = "projects recent"
	LabelCloneRemote    = "Clone from GitHub"
	ProjectsClone       = "Clone"
	ProjectsCloneFolder = "Choose folder…"
)

// ProjectsRow is the label of a project row; ProjectsCloseOf is its close
// button.
func ProjectsRow(name string) string     { return "project " + name }
func ProjectsCloseOf(name string) string { return ProjectsClose + " " + name }

// ProjectsOpenOf is the `open` button of a recent project (D116).
func ProjectsOpenOf(name string) string { return "open " + name }

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
	// `+ open folder…` (D116).
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

	open   bool
	filter string
	// remote is the text of the clone field; parent is the folder the owner
	// picked ("" means CloneParent); cloning is true while git runs; cloneErr
	// is the text of the last failure.
	remote   string
	parent   string
	cloning  bool
	cloneErr string
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
	}
	m.filter = ""
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
// (D117): the parent folder of the last opened project, the newest recent
// entry, when it exists; else CloneParent.
func (m *ProjectsModal) defaultParent() string {
	if st := m.store(); st != nil {
		if rec := st.Recent(); len(rec) > 0 && rec[0].Path != "" {
			p := filepath.Dir(rec[0].Path)
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				return p
			}
		}
	}
	return m.CloneParent
}

func (m *ProjectsModal) redraw() {
	if m.Invalidate != nil {
		m.Invalidate()
	}
}

// shown returns the groups that match the filter.
func (m *ProjectsModal) shown() []spaces.RepoGroup {
	var groups []spaces.RepoGroup
	if m.Groups != nil {
		groups = m.Groups()
	}
	f := strings.ToLower(strings.TrimSpace(m.filter))
	if f == "" {
		return groups
	}
	var out []spaces.RepoGroup
	for _, g := range groups {
		if strings.Contains(strings.ToLower(g.Name+" "+g.CommonDir), f) {
			out = append(out, g)
		}
	}
	return out
}

// reorder moves rows of the open list before row to. It does nothing while the
// filter hides rows, since the row numbers then differ from the stored order.
func (m *ProjectsModal) reorder(rows []int, to int) {
	st := m.store()
	if st == nil || strings.TrimSpace(m.filter) != "" {
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

// closeProject hides the project in contr only (D70).
func (m *ProjectsModal) closeProject(dir string) {
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

// folder is the name of the folder that holds the project.
func projectFolder(commonDir string) string {
	dir := commonDir
	if filepath.Base(dir) == ".git" {
		dir = filepath.Dir(dir)
	}
	return filepath.Base(filepath.Dir(dir))
}

// initials are the first letters of the first two words of name, in capitals.
func projectInitials(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == '.' })
	var out []rune
	for _, w := range words {
		out = append(out, []rune(strings.ToUpper(w))[0])
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}

// sub is the second line of a row: the folder, the worktrees and the ones that need the owner.
func projectSub(g spaces.RepoGroup) string {
	you := 0
	for _, r := range g.Rows {
		if r.HoldYou {
			you++
		}
	}
	return projectFolder(g.CommonDir) + " · " + strconv.Itoa(len(g.Rows)) + " worktrees · " + strconv.Itoa(you) + " need you"
}

// View builds the modal, or nothing while it is closed.
func (m *ProjectsModal) View(c *ui.Context) {
	if !m.open {
		return
	}
	open := true
	d := ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(8, 8, 8, 0.72)).Justify(ui.Start).AlignItems(ui.Center).Padding(theme.ModalTop, 0, 0, 0)
		panel.Label(LabelProjects).Column().Width(theme.ModalWidth).Background(theme.Color(theme.BgOverlay)).
			Border(1, theme.Color(theme.OverlayBorder)).Radius(theme.ModalRadius).
			Shadow(0, 24, 80, 0, ui.RGBA(0, 0, 0, 0.6))
		panel.Children(func() { m.body(c) })
	})
	if d.Dismissed() || !open {
		m.open = false
		m.redraw()
	}
}

func (m *ProjectsModal) body(c *ui.Context) {
	groups := m.shown()
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(8).Padding(16, theme.ModalPadX, 12, theme.ModalPadX).Children(func() {
		ui.Text(c, "Projects").Bold().Shrink(0).TextColor(theme.Color(theme.Text))
		ui.Text(c, strconv.Itoa(len(m.groupsAll()))+" open").Grow(1).TextColor(theme.Color(theme.TextDim))
		cardButton(c, ProjectsEsc, ButtonPlain, theme.TextDim, m.Close)
	})
	ui.TextInput(c, &m.filter).Label(LabelProjectsFilter).Placeholder("> filter projects…").
		AutoFocus().Shrink(0).Margin(0, theme.ModalPadX, 12, theme.ModalPadX)
	ui.Text(c, "open in contr · drag to reorder the sidebar").Shrink(0).
		PaddingX(theme.ModalPadX).TextColor(theme.Color(theme.TextDim))
	m.list.Key = func(i int) any { return groups[i].CommonDir }
	m.list.Label = func(i int) string { return ProjectsRow(groups[i].Name) }
	m.list.Reorder = nil
	if strings.TrimSpace(m.filter) == "" {
		m.list.Reorder = m.reorder
	}
	h := float32(len(groups)) * theme.ModalRowHeight
	ui.List(c.Key("projectslist"), &m.list, len(groups), func(i int) { m.row(c, groups[i]) }).
		Label(LabelProjectsList).FillWidth().Shrink(0).Height(min(h, theme.ModalListMax)).Padding(6, 10)
	m.recentSection(c)
	m.cloneSection(c)
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Justify(ui.End).Gap(14).Padding(12, theme.ModalPadX).
		BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
		if m.PickFolder != nil {
			cardButton(c, ProjectsOpenFolder, ButtonOutlined, theme.Text, m.openFolder)
			ui.Spacer(c)
		}
		cardButton(c, ProjectsDone, ButtonFilled, theme.Text, m.Close)
	})
}

// cloneSection is the clone part: the field, the destination preview and the
// buttons. A busy state or the last error shows below.
func (m *ProjectsModal) cloneSection(c *ui.Context) {
	if m.CloneParent == "" {
		return
	}
	_, dest, perr := m.cloneDest()
	ui.Column(c).Shrink(0).Padding(10, theme.ModalPadX).Gap(6).
		BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
		ui.Text(c, "clone from GitHub").TextColor(theme.Color(theme.TextDim))
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			if ui.TextInput(c, &m.remote).Label(LabelCloneRemote).Placeholder("owner/name or a URL").Grow(1).MinWidth(0).
				Submitted() {
				m.clone()
			}
			if m.PickParent != nil {
				cardButton(c, ProjectsCloneFolder, ButtonOutlined, theme.Text, m.chooseParent)
			}
			label := ProjectsClone
			if m.cloning {
				label = "Cloning…"
			}
			cardButton(c, label, ButtonOutlined, theme.Text, m.clone)
		})
		switch {
		case m.cloneErr != "":
			ui.Text(c, m.cloneErr).TextColor(theme.Color(theme.StatusFailed))
		case perr != nil:
			ui.Text(c, "not a repository yet").TextColor(theme.Color(theme.TextDim))
		case dest != "":
			ui.Text(c, "→ "+dest).SingleLine().MinWidth(0).TextColor(theme.Color(theme.Text2))
		}
	})
}

// recentSection lists the recent projects that are not open, each with `open`.
func (m *ProjectsModal) recentSection(c *ui.Context) {
	recent := m.recent()
	if len(recent) == 0 {
		return
	}
	ui.Column(c).Label(LabelProjectsRecent).Shrink(0).Padding(10, theme.ModalPadX).Gap(4).Children(func() {
		ui.Text(c, "recent").TextColor(theme.Color(theme.TextDim))
		for _, e := range recent {
			name := storedName(e.Dir, e.Path)
			ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Text(c, name).Grow(1).SingleLine().MinWidth(0).TextColor(theme.Color(theme.Text2))
				cardButton(c, ProjectsOpenOf(name), ButtonOutlined, theme.Text, func() { m.openRecent(e) })
			})
		}
	})
}

func (m *ProjectsModal) groupsAll() []spaces.RepoGroup {
	if m.Groups == nil {
		return nil
	}
	return m.Groups()
}

func (m *ProjectsModal) row(c *ui.Context, g spaces.RepoGroup) {
	ui.Row(c).Label(ProjectsRow(g.Name)).Height(theme.ModalRowHeight).AlignItems(ui.Center).Gap(12).
		Padding(8, 10).Radius(8).Children(func() {
		ui.Text(c, "⋮⋮").Shrink(0).TextColor(theme.Color(theme.ModalGrip))
		ui.Box(c).Width(theme.ModalTile).Height(theme.ModalTile).Shrink(0).Radius(theme.ModalTileRadius).
			Background(theme.Color(theme.ModalTileBg)).Center().Children(func() {
			ui.Text(c, projectInitials(g.Name)).Bold().TextColor(theme.Color(theme.Text2))
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, g.Name).Bold().SingleLine().MinWidth(0).TextColor(theme.Color(theme.Text))
			ui.Text(c, projectSub(g)).SingleLine().MinWidth(0).TextColor(theme.Color(theme.ModalRowSub))
		})
		cardButton(c, ProjectsCloseOf(g.Name), ButtonOutlined, theme.Text, func() { m.closeProject(g.CommonDir) })
	})
}
