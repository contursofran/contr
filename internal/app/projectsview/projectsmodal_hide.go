package projectsview

// Hide and Undo of the projects modal (D207). Hide takes a project out of the
// sidebar in contr only (D70) and puts it at the top of Recent. A toast of the
// frame offers Undo, which shows the project again at its old place.

import (
	"slices"
	"time"

	"github.com/contursofran/contr/internal/app/spaces"
)

// ProjectsUndo is the label of the Undo button of the hide toast.
const ProjectsUndo = "Undo"

// hiddenToast is the text of the toast after Close project (D291).
func hiddenToast(name string) string { return name + " closed" }

// Hide hides the project and offers Undo in a toast.
func (m *ProjectsModal) Hide(dir string) {
	st := m.store()
	if st == nil {
		return
	}
	groups := m.groupsAll()
	dirs := make([]string, len(groups))
	for i, g := range groups {
		dirs[i] = g.CommonDir
	}
	place := placeOf(st.Order(dirs), dir)
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
	m.Changed()
	if m.Toast != nil {
		m.Toast(hiddenToast(name), ProjectsUndo, m.undoOnce(dir, place))
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

// place is where a project sat in the order: its index, the project before it
// ("" at the start) and the project after it ("" at the end).
type place struct {
	index         int
	after, nextTo string
}

// placeOf finds dir in order.
func placeOf(order []string, dir string) place {
	for i, d := range order {
		if d == dir {
			p := place{index: i}
			if i > 0 {
				p.after = order[i-1]
			}
			if i+1 < len(order) {
				p.nextTo = order[i+1]
			}
			return p
		}
	}
	return place{index: len(order)}
}

// undoOnce returns the Undo action: it runs unhide once, and a second press
// does nothing.
func (m *ProjectsModal) undoOnce(dir string, p place) func() {
	done := false
	return func() {
		if done {
			return
		}
		done = true
		m.unhide(dir, p)
	}
}

// unhide shows the project again at its old place in the current order. Later
// changes to the order stay. It does not need the modal to be open.
func (m *ProjectsModal) unhide(dir string, p place) {
	st := m.store()
	if st == nil {
		return
	}
	st.Show(dir)
	groups := m.groupsAll()
	dirs := make([]string, 0, len(groups)+1)
	for _, g := range groups {
		dirs = append(dirs, g.CommonDir)
	}
	cur := slices.DeleteFunc(st.Order(dirs), func(d string) bool { return d == dir })
	i := min(p.index, len(cur))
	if j := slices.Index(cur, p.after); p.after != "" && j >= 0 {
		i = j + 1
	} else if j := slices.Index(cur, p.nextTo); p.nextTo != "" && j >= 0 {
		i = j
	}
	st.SetOrder(slices.Insert(cur, i, dir))
	m.Changed()
}
