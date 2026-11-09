package app

// The sidebar head part (docs/spec/ServerRow.md, docs/spec/ProjectRow.md):
// the Projects head and one row for each project, with its agents, in one
// ui.List of kit.Row, then the Servers section with the This Mac row (D154). The model is in spaces.go.

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/theme"
)

// Element labels of the sidebar head part.
const (
	LabelServersHead  = "Servers"
	LabelProjectsHead = "Projects"
	LabelProjectList  = "project list"
	LabelProjectsOpen = "Open projects"
	LabelProjectsNew  = "New orchestrator"
	// LabelProjectsAdd names the `+` button of the Projects head; its menu has the two above.
	LabelProjectsAdd = "Add"
	// LabelSettings names the Settings button at the bottom of the sidebar (D126, D147).
	LabelSettingsOpen = "Settings"
	// LabelSettingsUpdate names it while an update waits.
	LabelSettingsUpdate = "Settings, update ready"
)

// ServerLabel is the label of the This Mac row: "This Mac, local", or
// "This Mac, offline" while contr does not follow the core.
func ServerLabel(online bool) string {
	if online {
		return "This Mac, local"
	}
	return "This Mac, offline"
}

// ServerMeta is the meta of the This Mac row: "local" and, when the host
// sampler has a reading, its CPU and memory (D126). Offline, it is "offline".
func (l *SpacesList) ServerMeta() string {
	if !l.online {
		return "offline"
	}
	meta := "local"
	if l.Host != nil {
		if s, ok := l.Host.Stats(); ok {
			meta += " · " + s.CPULabel() + " CPU · " + s.MemLabel() + " MEM"
		}
	}
	return meta
}

// ProjectLabel is the label of a project row: its name and branch, or its
// name and "nothing open".
func ProjectLabel(g spaces.RepoGroup) string {
	if meta := projectMeta(g); meta != "" {
		return g.Name + ", " + meta
	}
	return g.Name
}

// projectMeta is the text at the right of a project row.
func projectMeta(g spaces.RepoGroup) string {
	if g.Path != "" {
		return "nothing open"
	}
	return g.Branch
}

// LabelNewIn is the menu item of an empty project that starts an orchestrator there.
func LabelNewIn(g spaces.RepoGroup) string { return "New orchestrator in " + g.Name }

// View builds the sidebar head part.
func (l *SpacesList) View(c *ui.Context) {
	ui.Column(c).FillWidth().FillHeight().Padding(theme.Space10, 0, 0, 0).Children(func() {
		if l.compact {
			// An icon column (D128): no section names or problem line; the
			// project buttons stay, one above the other. Same order (D154).
			ui.Column(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(theme.Space2).
				Padding(theme.Space6, 0, theme.Space6, 0).Children(func() {
				kit.IconButton(c, kit.IconButtonOpts{Icon: "folder", Label: LabelProjectsOpen, Small: true, OnClick: l.OnProjects})
				kit.IconButton(c, kit.IconButtonOpts{Icon: "plus", Label: LabelProjectsNew, Small: true, OnClick: l.OnNew})
			})
			l.projectList(c)
			l.serverRow(c)
			l.settingsFoot(c)
			return
		}
		// Projects take the free height and scroll; Servers and Settings stay
		// at the bottom (D154).
		// One `+` button; its menu has the two actions (#436).
		l.head(c, LabelProjectsHead, func() {
			kit.IconButton(c, kit.IconButtonOpts{Icon: "plus", Label: LabelProjectsAdd, Small: true, Menu: l.addMenu})
		})
		l.projectList(c)
		l.head(c, LabelServersHead, nil)
		l.serverRow(c)
		l.coreLine(c)
		l.settingsFoot(c)
	})
}

// addMenu is the menu of the `+` button of the Projects head: a new
// orchestrator, or the projects modal.
func (l *SpacesList) addMenu(m *ui.Menu) {
	if m.Item(LabelProjectsNew).Chosen() && l.OnNew != nil {
		l.OnNew()
	}
	if m.Item(LabelProjectsOpen).Chosen() && l.OnProjects != nil {
		l.OnProjects()
	}
}

// settingsFoot is the foot row of the sidebar: the plan meters at the left (D196)
// and the Settings button at the right; the icon column shows only the button,
// centered (D147). The dot shows while an update waits (D146).
func (l *SpacesList) settingsFoot(c *ui.Context) {
	label, ready := LabelSettingsOpen, l.UpdateReady != nil && l.UpdateReady()
	if ready {
		label = LabelSettingsUpdate
	}
	p := theme.Current()
	plans := !l.compact && l.Plans != nil
	justify := ui.End
	switch {
	case l.compact:
		justify = ui.Center
	case plans:
		// The meters at the left, a spacer, then the button.
		justify = ui.Start
	}
	// The foot insets its content by Space10 at both sides, so the meters'
	// left inset equals the gear's right inset (#495). Build each element
	// inside its parent's Children: a row made before the column is a sibling
	// of it, so the column's padding never reached the row.
	ui.Column(c).Label("sidebar foot").Shrink(0).Padding(theme.Space6, theme.Space10, 0, theme.Space10).Children(func() {
		ui.Box(c).Label("sidebar foot divider").Height(1).Shrink(0).Background(p.Hairline)
		ui.Row(c).Label("sidebar foot row").FillWidth().Shrink(0).AlignItems(ui.Center).Justify(justify).
			Padding(theme.Space6, 0, theme.Space6, 0).Children(func() {
			if plans {
				kit.PlanUsage(c, l.Plans())
				ui.Box(c).Grow(1)
			}
			kit.IconButton(c, kit.IconButtonOpts{Icon: "settings", Label: label, Small: true, Dot: ready,
				Key: "⌘,", OnClick: l.OnSettings})
		})
	})
}

// head is a section head: the name in the label style, then icon buttons.
func (l *SpacesList) head(c *ui.Context, name string, buttons func()) {
	ui.Row(c).Label(name+" header").FillWidth().Shrink(0).AlignItems(ui.Center).Gap(theme.Space2).
		Padding(theme.Space6, theme.Space10, theme.Space2, theme.Space16).Children(func() {
		ui.Text(c, name).SingleLine().Grow(1).MinWidth(0).TextColor(theme.Current().Ink45).
			Font(theme.StyleLabel.Family()).FontSize(theme.StyleLabel.Size).FontWeight(theme.StyleLabel.Weight)
		if buttons != nil {
			buttons()
		}
	})
}

// rowIcon is a 15px line icon in the given ink. The color is set here: a
// selected ButtonBase gives its content the accent's text color, which is
// almost invisible on a flat row (the This Mac icon, #436).
func rowIcon(c *ui.Context, name string, ink ui.Color) {
	if s := icons.Line(name); s != nil {
		ui.Icon(c, s).Size(15, 15).Shrink(0).TextColor(ink)
	}
}

// serverIconInk is the ink of the This Mac icon: its row is always the selected look.
func serverIconInk(p *theme.Palette) ui.Color { return p.Ink100 }

// projectIconInk is the ink of a project's folder icon: ink-100 when selected, ink-42 when the
// project is empty, else ink-85.
func projectIconInk(p *theme.Palette, selected, empty bool) ui.Color {
	switch {
	case selected:
		return p.Ink100
	case empty:
		return p.Ink42
	}
	return p.Ink85
}

// serverRow is the This Mac row. Its `+` stays hidden until remote servers
// have a source (#149).
func (l *SpacesList) serverRow(c *ui.Context) {
	kit.Row(c, kit.RowOpts{
		Flat: true, Label: ServerLabel(l.online), Text: "This Mac", Meta: l.ServerMeta(), Selected: true,
		Lead: func() { rowIcon(c, "computer", serverIconInk(theme.Current())) }, Compact: l.compact, Tip: "This Mac",
	})
}

// coreLine is the hc problem, under the This Mac row, while contr does not
// follow the core (D113).
func (l *SpacesList) coreLine(c *ui.Context) {
	if l.online || l.line == "" {
		return
	}
	ui.Text(c, l.line).TextColor(theme.Current().Ink38).
		Font(theme.StyleSmall.Family()).FontSize(theme.StyleSmall.Size).
		PaddingX(theme.Space16).PaddingY(theme.Space2)
}

// projectList is the projects, in the owner's order, with the agents of each
// under it, in one ui.List. A drag reorders the projects (D109).
func (l *SpacesList) projectList(c *ui.Context) {
	items := l.items()
	l.list.Key = func(i int) any {
		if a := items[i].agent; a != nil {
			return a.w.Address
		}
		return l.groups[items[i].group].CommonDir
	}
	l.list.Label = func(i int) string {
		if a := items[i].agent; a != nil {
			return AgentLabel(a.w, issueText(*a), l.modelOf(a.w))
		}
		return ProjectLabel(l.groups[items[i].group])
	}
	l.list.Reorder = nil
	if l.projects != nil {
		l.list.Reorder = l.Reorder
	}
	// Up, Down and Enter are the list's shortcuts, not its Selected choice:
	// that paints a row in the accent color, which the rows must not show.
	// The app selection stays the one source; l.cursor follows it.
	l.syncCursor(items)
	l.list.Handle.OnShortcut(c, 0, ui.KeyUp, func() { l.move(items, -1) })
	l.list.Handle.OnShortcut(c, 0, ui.KeyDown, func() { l.move(items, 1) })
	l.list.Handle.OnShortcut(c, 0, ui.KeyEnter, func() { l.open(items) })
	ui.List(c.Key("projects"), &l.list, len(items), func(i int) {
		if a := items[i].agent; a != nil {
			l.agentRow(c, *a)
			return
		}
		l.projectRow(c, l.groups[items[i].group], items[i].group)
	}).Label(LabelProjectList).Focusable().FillWidth().Grow(1).MinHeight(0).ContextMenu(func(m *ui.Menu) {
		// The menu key (or Shift+F10) with the list focused opens the menu of the empty
		// project under the key cursor, so no right-click is needed (#443). The rows
		// have menus of their own for the pointer; other rows give no items here.
		if l.cursor >= 0 && l.cursor < len(items) && items[l.cursor].agent == nil {
			if g := l.groups[items[l.cursor].group]; g.Path != "" {
				l.emptyMenu(m, g)
			}
		}
	})
}

// emptyMenu is the menu of an empty project (D116): a new orchestrator in its folder.
func (l *SpacesList) emptyMenu(m *ui.Menu, g spaces.RepoGroup) {
	if m.Item(LabelNewIn(g)).Chosen() && l.OnNewIn != nil {
		l.OnNewIn(g.Path)
	}
}

// projectRow is one ProjectRow; index is its place among the projects. An empty project (D116) has a dim name, the
// meta "nothing open" and a `+` that starts an orchestrator in its folder.
func (l *SpacesList) projectRow(c *ui.Context, g spaces.RepoGroup, index int) {
	p := theme.Current()
	open := !l.Folded(g.CommonDir)
	o := kit.RowOpts{
		Flat: true, Label: ProjectLabel(g), Text: g.Name, Meta: projectMeta(g),
		NameInk: p.Ink85, MetaInk: p.Ink26, NameStyle: theme.StyleBody,
		Expanded: &open, OnClick: func() { l.Activate(g) },
		Compact: l.compact, Tip: g.Name,
	}
	iconInk := projectIconInk(p, false, false)
	if l.selection != nil {
		if sel, ok := l.selection.Current(); ok {
			// An open project shows the selection on the agent row; the project
			// row shows it for the project itself and while the agents are folded.
			o.Selected = l.itemSelected(sideItem{group: l.groupIndex(g)}, sel)
		}
	}
	if o.Selected {
		iconInk = projectIconInk(p, true, false)
	}
	if g.Path != "" {
		// An empty project is dim: name and icon ink-42, no `+` (#436). The
		// start of an orchestrator in its folder (D116) is in the row's menu.
		o.NameInk, iconInk = p.Ink42, projectIconInk(p, false, true)
		o.Menu = func(m *ui.Menu) { l.emptyMenu(m, g) }
	}
	o.Lead = func() { rowIcon(c, "folder", iconInk) }
	if index == 0 {
		kit.Row(c, o)
		return
	}
	// Air between project groups (#436): the list sizes a row from its own
	// box, so the space is the padding of a wrapper, not a margin of the row.
	ui.Column(c).FillWidth().Padding(theme.Space12, 0, 0, 0).Children(func() { kit.Row(c, o) })
}
