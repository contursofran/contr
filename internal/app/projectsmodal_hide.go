package app

// Hide and Undo of the projects modal (D207). Hide takes a project out of the
// sidebar in contr only (D70) and puts it at the top of Recent. A toast of the
// frame offers Undo, which shows the project again at its old place.

import (
	"time"

	"github.com/contursofran/contr/internal/app/spaces"
)

// hiddenToast is the text of the toast after Hide.
func hiddenToast(name string) string { return name + " hidden from the sidebar" }

// hide hides the project and offers Undo in a toast.
func (m *ProjectsModal) hide(dir string) {
	st := m.store()
	if st == nil {
		return
	}
	groups := m.groupsAll()
	dirs := make([]string, len(groups))
	for i, g := range groups {
		dirs[i] = g.CommonDir
	}
	prev := st.Order(dirs)
	name, path := dir, st.Path(dir)
	for _, g := range groups {
		if g.CommonDir == dir {
			name, path = g.Name, hidePath(st.Path(dir), g)
		}
	}
	now := time.Now
	if m.Now != nil {
		now = m.Now
	}
	st.Hide(dir)
	st.Touch(dir, path, now())
	m.changed()
	if m.Toast != nil {
		m.Toast(hiddenToast(name), ProjectsUndo, func() { m.unhide(dir, prev) })
	}
}

// hidePath is the folder of a project: the stored one, else the folder of the
// group, else the root of its main worktree.
func hidePath(stored string, g spaces.RepoGroup) string {
	if stored != "" {
		return stored
	}
	if g.Path != "" {
		return g.Path
	}
	for _, r := range g.Rows {
		if r.IsMain {
			return r.Root
		}
	}
	return ""
}

// unhide shows the project again and restores the order that was before Hide.
// It does not need the modal to be open.
func (m *ProjectsModal) unhide(dir string, prev []string) {
	st := m.store()
	if st == nil {
		return
	}
	st.Show(dir)
	st.SetOrder(prev)
	m.changed()
}
