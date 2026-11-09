package app

// The Add rows of the projects modal (D207): clone a GitHub repo, or open a
// folder by its path. The search field holds the repo or the path.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

// expandHome turns a leading `~` or `~/` into the home folder. Other text
// stays as it is.
func expandHome(path, home string) string {
	switch {
	case home == "":
		return path
	case path == "~":
		return home
	case strings.HasPrefix(path, "~/"):
		return filepath.Join(home, path[2:])
	}
	return path
}

// openPath checks the folder on a goroutine, then adds it to the store and to
// the recent list like Open folder does (D116). A path that is not a folder
// shows a red line under the row (Q6).
func (m *ProjectsModal) openPath(path string) {
	home, _ := os.UserHomeDir()
	full := expandHome(path, home)
	m.Run.Do(func() func() {
		fi, err := os.Stat(full)
		ok := err == nil && fi.IsDir()
		dir := full
		if ok && m.Resolve != nil {
			dir = m.Resolve(full)
		}
		return func() {
			if m.text() != path { // the field changed meanwhile
				return
			}
			if !ok {
				m.pathErr = "not a folder: " + path
				m.redraw()
				return
			}
			m.query, m.sel, m.pathErr = "", 0, ""
			m.add(dir, full)
		}
	})
}

// addRowView builds the inside of an Add row.
func (m *ProjectsModal) addRowView(c *ui.Context, r paletteRow, active, selected bool) {
	p := theme.Current()
	name, icon, meta := "Open "+r.name(), "folder", r.path
	if r.kind == rowClone {
		icon = "download"
		_, dest, _ := m.cloneDest()
		meta = "into " + dest
		switch {
		case m.cloning:
			name = "Cloning " + r.name()
		case m.cloneErr != "":
			name = "Could not clone " + r.name()
		default:
			name = "Clone " + r.name()
		}
	}
	styled(ui.Text(c, ""), theme.StyleSmall).Width(theme.ModalGrip).Shrink(0)
	ui.Box(c).Width(theme.ModalTile).Height(theme.ModalTile).Shrink(0).Radius(theme.Radius8).
		Background(p.Selected).Center().Children(func() {
		if s := icons.Line(icon); s != nil {
			ui.Icon(c, s).Size(15, 15).TextColor(p.Ink62)
		}
	})
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		styled(ui.Text(c, name), theme.StyleBody).FontWeight(600).SingleLine().MinWidth(0).TextColor(p.Ink100)
		m.metaText(c, meta, 0)
		switch {
		case r.kind == rowClone && m.cloning:
			ui.Progress(c, -1).Label(LabelCloneProgress).Height(theme.Space2).Margin(theme.Space6, 0, 0, 0)
		case r.kind == rowClone && m.cloneErr != "":
			styled(ui.Text(c, m.cloneErr), theme.StyleValue).Wrap().TextColor(p.Blocked)
		case r.kind == rowPath && m.pathErr != "":
			styled(ui.Text(c, m.pathErr), theme.StyleValue).Wrap().TextColor(p.Blocked)
		}
	})
	action := func(label string, fn func()) {
		b := ui.ButtonBase(c).Label(label).Shrink(0).Padding(theme.Space4, theme.Space8).
			Radius(theme.Radius6).Cursor(ui.CursorPointer).OnClick(fn)
		colour := p.Ink45
		if b.Hovered() {
			b, colour = b.Background(p.Hover), p.Ink100
		}
		b.Children(func() { styled(ui.Text(c, label), theme.StyleSmall).NoWrap().TextColor(colour) })
	}
	switch {
	case r.kind == rowClone && m.cloning:
		action(ProjectsCancel, m.stopClone)
	case r.kind == rowClone && m.cloneErr != "":
		action(ProjectsTryAgain, m.clone)
	case r.kind == rowClone && m.PickParent != nil && active:
		action(ProjectsChangeDir, m.chooseParent)
	}
	if selected {
		styled(ui.Text(c, "⏎ "+m.enterLabel([]paletteRow{r}, 0)), theme.StyleMeta).NoWrap().Shrink(0).TextColor(p.Ink38)
	}
}
