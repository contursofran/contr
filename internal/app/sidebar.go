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

// LabelNewIn is the label of the `+ new` button of an empty project.
func LabelNewIn(g spaces.RepoGroup) string { return "New orchestrator in " + g.Name }

// View builds the sidebar head part.
func (l *SpacesList) View(c *ui.Context) {
	ui.Column(c).FillWidth().FillHeight().Padding(theme.Space10, 0, 0, 0).Children(func() {
		buttons := func() {
			kit.IconButton(c, kit.IconButtonOpts{Icon: "folder", Label: LabelProjectsOpen, Small: true, OnClick: l.OnProjects})
			kit.IconButton(c, kit.IconButtonOpts{Icon: "plus", Label: LabelProjectsNew, Small: true, OnClick: l.OnNew})
		}
		if l.compact {
			// An icon column (D128): no section names or problem line; the
			// project buttons stay, one above the other. Same order (D154).
			ui.Column(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(theme.Space2).
				Padding(theme.Space6, 0, theme.Space6, 0).Children(buttons)
			l.projectList(c)
			l.serverRow(c)
			l.settingsFoot(c)
			return
		}
		// Projects take the free height and scroll; Servers and Settings stay
		// at the bottom (D154).
		l.head(c, LabelProjectsHead, buttons)
		l.projectList(c)
		l.head(c, LabelServersHead, nil)
		l.serverRow(c)
		l.coreLine(c)
		l.settingsFoot(c)
	})
}

// settingsFoot is the Settings button at the bottom left of the sidebar, and at the bottom of
// the icon column (D147). The dot shows while an update waits (D146).
func (l *SpacesList) settingsFoot(c *ui.Context) {
	label, ready := LabelSettingsOpen, l.UpdateReady != nil && l.UpdateReady()
	if ready {
		label = LabelSettingsUpdate
	}
	row := ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Padding(theme.Space6, theme.Space10, theme.Space6, theme.Space10)
	if l.compact {
		row = row.Justify(ui.Center)
	}
	row.Children(func() {
		kit.IconButton(c, kit.IconButtonOpts{Icon: "settings", Label: label, Small: true, Dot: ready,
			Key: "⌘,", OnClick: l.OnSettings})
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

// rowIcon is a 15px line icon, drawn in the text color of the row.
func rowIcon(c *ui.Context, name string) {
	if s := icons.Line(name); s != nil {
		ui.Icon(c, s).Size(15, 15).Shrink(0)
	}
}

// serverRow is the This Mac row. Its `+` stays hidden until remote servers
// have a source (#149).
func (l *SpacesList) serverRow(c *ui.Context) {
	kit.Row(c, kit.RowOpts{
		Flat: true, Label: ServerLabel(l.online), Text: "This Mac", Meta: l.ServerMeta(), Selected: true,
		Lead: func() { rowIcon(c, "computer") }, Compact: l.compact, Tip: "This Mac",
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
		l.projectRow(c, l.groups[items[i].group])
	}).Label(LabelProjectList).Focusable().FillWidth().Grow(1).MinHeight(0)
}

// projectRow is one ProjectRow. An empty project (D116) has a dim name, the
// meta "nothing open" and a `+` that starts an orchestrator in its folder.
func (l *SpacesList) projectRow(c *ui.Context, g spaces.RepoGroup) {
	p := theme.Current()
	open := !l.Folded(g.CommonDir)
	o := kit.RowOpts{
		Flat: true, Label: ProjectLabel(g), Text: g.Name, Meta: projectMeta(g),
		NameInk: p.Ink85, MetaInk: p.Ink26, NameStyle: theme.StyleBodyStrong,
		Expanded: &open, OnClick: func() { l.Activate(g) },
		Lead: func() { rowIcon(c, "project") }, Compact: l.compact, Tip: g.Name,
	}
	if l.selection != nil {
		if sel, ok := l.selection.Current(); ok {
			// An open project shows the selection on the agent row; the project
			// row shows it for the project itself and while the agents are folded.
			o.Selected = l.itemSelected(sideItem{group: l.groupIndex(g)}, sel)
		}
	}
	if g.Path != "" {
		o.NameInk = p.Ink42
		// The icon column has no room for the `+` (D128).
		o.Trail = func() {
			kit.IconButton(c, kit.IconButtonOpts{Icon: "plus", Label: LabelNewIn(g), Small: true,
				OnClick: func() {
					if l.OnNewIn != nil {
						l.OnNewIn(g.Path)
					}
				}})
		}
	}
	kit.Row(c, o)
}
