package app

// The view of the projects modal (D207): search field, one list, footer.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/projects"
)

func (m *ProjectsModal) groupsAll() []spaces.RepoGroup {
	if m.Groups == nil {
		return nil
	}
	return m.Groups()
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

// rowKind tells what a row of the list does on Enter.
type rowKind int

const (
	rowOpen   rowKind = iota // switch the sidebar to the project
	rowRecent                // show the project again
	rowClone                 // clone a GitHub repo (Add)
	rowPath                  // open a folder by its path (Add)
)

// paletteRow is one selectable row of the list.
type paletteRow struct {
	kind  rowKind
	group spaces.RepoGroup // rowOpen
	entry projects.Entry   // rowRecent
	owner string           // rowClone
	repo  string           // rowClone: the folder name
	path  string           // rowPath: the text of the field
	busy  bool             // rowClone: this is the running clone
}

func (r paletteRow) name() string {
	switch r.kind {
	case rowOpen:
		return r.group.Name
	case rowClone:
		return r.owner + "/" + r.repo
	case rowPath:
		return pathBase(r.path)
	}
	return storedName(r.entry.Dir, r.entry.Path)
}

// pathBase is the last element of a typed path; `~` and `/` stand for themselves.
func pathBase(p string) string {
	if b := filepath.Base(strings.TrimRight(p, "/")); b != "." && b != "" {
		return b
	}
	return p
}

// add tells whether the row is an Add row.
func (r paletteRow) add() bool { return r.kind == rowClone || r.kind == rowPath }

// rows are the selectable rows: the open groups that match the field, then
// the recent projects that match it.
func (m *ProjectsModal) rows() []paletteRow {
	base := m.queryRows()
	if !m.cloning {
		return base
	}
	// A running clone stays on top, whatever the field says.
	job := paletteRow{kind: rowClone, owner: m.job.owner, repo: m.job.repo, busy: true}
	if len(base) == 1 && base[0].kind == rowClone && base[0].owner == job.owner && base[0].repo == job.repo {
		return []paletteRow{job}
	}
	return append([]paletteRow{job}, base...)
}

// queryRows are the rows for the text of the field.
func (m *ProjectsModal) queryRows() []paletteRow {
	// A repo or a path replaces the list with one Add row. A repo needs a
	// clone folder; without one the text filters like any other text.
	switch k, owner, repo := classifyQuery(m.query); {
	case k == queryRepo && m.CloneParent != "":
		return []paletteRow{{kind: rowClone, owner: owner, repo: repo}}
	case k == queryPath:
		return []paletteRow{{kind: rowPath, path: m.text()}}
	}
	var out []paletteRow
	for _, g := range m.shown() {
		out = append(out, paletteRow{kind: rowOpen, group: g})
	}
	f := m.text()
	for _, e := range m.recent() {
		if _, _, ok := matchSpan(storedName(e.Dir, e.Path), f); ok || f == "" {
			out = append(out, paletteRow{kind: rowRecent, entry: e})
		}
	}
	return out
}

// line is one row of the list: a section header or a selectable row.
type line struct {
	head, hint string
	row        int // index into rows(); -1 for a header
}

// lines puts the section headers between the rows. The Open count is that of
// all open projects, not of the matches.
func (m *ProjectsModal) lines(rows []paletteRow) []line {
	var out []line
	hint := ""
	if m.text() == "" && !m.cloning {
		hint = "drag to reorder the sidebar"
	}
	for i, r := range rows {
		if r.add() && (i == 0 || !rows[i-1].add()) {
			out = append(out, line{head: "Add", row: -1})
		}
		if r.kind == rowOpen && (i == 0 || rows[i-1].kind != rowOpen) {
			out = append(out, line{head: "Open · " + strconv.Itoa(len(m.groupsAll())), hint: hint, row: -1})
		}
		if r.kind == rowRecent && (i == 0 || rows[i-1].kind != rowRecent) {
			out = append(out, line{head: "Recent", row: -1})
		}
		out = append(out, line{row: i})
	}
	return out
}

// run does Enter on a row: switch to an open project and close, or show a
// recent project again.
func (m *ProjectsModal) run(r paletteRow) {
	switch r.kind {
	case rowRecent:
		m.openRecent(r.entry)
		return
	case rowClone:
		m.clone() // does nothing while a clone runs
		return
	case rowPath:
		m.openPath(r.path)
		return
	}
	if m.Switch != nil {
		m.Switch(r.group.CommonDir)
	}
	m.Close()
}

// enterLabel is the word of the footer for the selected row.
func (m *ProjectsModal) enterLabel(rows []paletteRow, sel int) string {
	switch {
	case sel < 0 || sel >= len(rows):
		return ""
	case rows[sel].kind == rowRecent, rows[sel].kind == rowPath:
		return "open"
	case rows[sel].kind == rowClone && m.cloning:
		return "wait"
	case rows[sel].kind == rowClone && m.cloneErr != "":
		return "try again"
	case rows[sel].kind == rowClone:
		return "clone"
	}
	return "switch"
}

// View builds the modal, or nothing while it is closed.
func (m *ProjectsModal) View(c *ui.Context) {
	if !m.open {
		return
	}
	open := true
	p := theme.Current()
	d := ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(p.Scrim).Justify(ui.Start).AlignItems(ui.Center).Padding(theme.ModalTop, 0, 0, 0)
		panel.Label(LabelProjects).Column().Width(theme.ProjectsWidth).Background(p.GlassFloat).
			Border(1, p.FloatLine).Radius(theme.Radius14)
		for _, l := range p.ShadowFloat {
			if !l.Inset {
				panel.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
			}
		}
		panel.Children(func() { m.body(c) })
	})
	if d.Dismissed() || !open {
		m.open = false
		m.redraw()
	}
}

// kbd is a key cap: mono text in a thin box.
func kbd(c *ui.Context, text string) {
	p := theme.Current()
	ui.Box(c).Shrink(0).Padding(theme.Space2, theme.Space4).Radius(theme.ModalKbdRadius).Border(1, p.InputLine).Children(func() {
		styled(ui.Text(c, text), theme.StyleKbd).NoWrap().TextColor(p.Ink45)
	})
}

// keys handles the window keys of the palette: ↑ ↓ and ⌘O.
func (m *ProjectsModal) keys(c *ui.Context, rows []paletteRow) {
	switch {
	case c.Shortcut(0, ui.KeyDown):
		m.sel, m.moved = min(m.sel+1, len(rows)-1), true
	case c.Shortcut(0, ui.KeyUp):
		m.sel, m.moved = max(m.sel-1, 0), true
	case c.Shortcut(ui.Cmd, ui.KeyO):
		m.openFolder()
	}
}

// searchField builds the search row. It is a text input with a search icon,
// not ui.SearchField, because the field must see ⌘⌫ and Esc before it takes
// them: ⌘⌫ hides the selected project and Esc clears the text first (Q2).
// SearchField keeps its input inside, out of reach of OnTextKey.
func (m *ProjectsModal) searchField(c *ui.Context, rows []paletteRow) {
	p := theme.Current()
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Space10).Padding(theme.Space14, theme.Space16).
		BorderWidth(0, 0, 1, 0).BorderColor(p.Hairline).Children(func() {
		if s := icons.Line("search"); s != nil {
			ui.Icon(c, s).Size(16, 16).Shrink(0).TextColor(p.Ink38)
		}
		in := ui.TextInputBase(c.Key("projects-search"), &m.query).Label(LabelProjectsSearch).Placeholder(LabelProjectsSearch).
			PlaceholderColor(p.Ink30).TextColor(p.Ink100).FontSize(theme.ModalSearchSize).AutoFocus().Grow(1).MinWidth(0)
		var r *paletteRow
		if m.sel < len(rows) {
			r = &rows[m.sel]
		}
		in.OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyBackspace && k.Mods == ui.Cmd:
				// With text in the field, ⌘⌫ deletes the text as in any field.
				if k.Text() != "" || r == nil || r.kind != rowOpen {
					return false
				}
				m.hide(r.group.CommonDir)
				return true
			case k.Key == ui.KeyEscape && k.Mods == 0 && k.Text() != "":
				k.SetText("")
				m.query, m.sel = "", 0
				m.cloneErr, m.pathErr = "", ""
				return true
			}
			return false
		})
		if in.Changed() {
			m.sel = 0
			m.cloneErr, m.pathErr = "", ""
		}
		if in.Submitted() && r != nil {
			m.run(*r)
		}
		kbd(c, "esc")
	})
}

func (m *ProjectsModal) body(c *ui.Context) {
	p := theme.Current()
	rows := m.rows()
	m.sel = max(0, min(m.sel, len(rows)-1))
	m.keys(c, rows)
	lines := m.lines(rows)
	m.sel = max(0, min(m.sel, len(rows)-1))

	m.searchField(c, rows)

	if len(lines) == 0 {
		if k, _, _ := classifyQuery(m.query); k == queryFilter && m.text() != "" {
			styled(ui.Text(c, ProjectsNoMatch(m.text())), theme.StyleSmall).Wrap().Shrink(0).TextAlign(ui.Center).
				Padding(theme.ModalEmptyPadY, theme.Space12).TextColor(p.Ink45)
		}
	} else {
		m.listView(c, rows, lines)
	}
	m.footer(c, rows)
}

func (m *ProjectsModal) listView(c *ui.Context, rows []paletteRow, lines []line) {
	m.list.Key = func(i int) any {
		l := lines[i]
		switch {
		case l.row < 0:
			return l.head
		case rows[l.row].kind == rowOpen:
			return rows[l.row].group.CommonDir
		case rows[l.row].busy:
			return "add-busy"
		case rows[l.row].add():
			return "add"
		}
		return "recent:" + rows[l.row].entry.Dir
	}
	m.list.Header = func(i int) bool { return lines[i].row < 0 }
	m.list.Reorder = nil
	if m.text() == "" && !m.cloning { // the Add row shifts the open rows
		m.list.Reorder = m.listReorder
	}
	home, _ := os.UserHomeDir()
	ui.List(c.Key("projectslist"), &m.list, len(lines), func(i int) {
		if lines[i].row < 0 {
			m.section(c, lines[i])
			return
		}
		m.row(c, rows, lines[i].row, home)
	}).Label(LabelProjectsList).FillWidth().Shrink(0).MaxHeight(theme.ModalListMax).Padding(theme.Space6)
	if m.moved {
		m.moved = false
		for i, ln := range lines {
			if ln.row == m.sel {
				m.list.ScrollIntoView(i)
			}
		}
	}
}

func (m *ProjectsModal) footer(c *ui.Context, rows []paletteRow) {
	p := theme.Current()
	ui.Row(c).Label(LabelProjectsFooter).Shrink(0).AlignItems(ui.Center).Gap(theme.Space14).
		Padding(theme.Space10, theme.Space16).BorderWidth(1, 0, 0, 0).BorderColor(p.Hairline).Children(func() {
		hint := func(key, label string) {
			ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
				kbd(c, key)
				styled(ui.Text(c, label), theme.StyleChip).NoWrap().TextColor(p.Ink45)
			})
		}
		hint("↑↓", "move")
		if lb := m.enterLabel(rows, m.sel); lb != "" {
			hint("⏎", lb)
		}
		if m.sel < len(rows) && rows[m.sel].kind == rowOpen && m.text() == "" {
			hint("⌘⌫", "hide")
		}
		ui.Spacer(c)
		if m.PickFolder == nil {
			return
		}
		b := ui.ButtonBase(c).Label(ProjectsOpenFolder).Shrink(0).Padding(theme.Space6, theme.Space10).
			AlignItems(ui.Center).Gap(theme.Space8).Radius(theme.Radius7).Border(1, p.InputLine).
			Cursor(ui.CursorPointer).OnClick(m.openFolder)
		text := p.Ink62
		if b.Hovered() {
			b = b.Background(p.Hover)
			text = p.Ink100
		}
		b.Children(func() {
			styled(ui.Text(c, ProjectsOpenFolder), theme.StyleSmall).NoWrap().TextColor(text)
			styled(ui.Text(c, "⌘O"), theme.StyleKbd).NoWrap().TextColor(p.Ink45)
		})
	})
}

// section builds a header of the list.
func (m *ProjectsModal) section(c *ui.Context, l line) {
	p := theme.Current()
	ui.Row(c).AlignItems(ui.Center).Padding(theme.Space10, theme.Space10, theme.Space4).Children(func() {
		styled(ui.Text(c, l.head), theme.StyleLabel).NoWrap().Grow(1).TextColor(p.Ink45)
		if l.hint != "" {
			styled(ui.Text(c, l.hint), theme.StyleChip).NoWrap().TextColor(p.Ink30)
		}
	})
}

// nameText builds the name of a row with the match of the field underlined.
func (m *ProjectsModal) nameText(c *ui.Context, name string, weight int, colour ui.Color) {
	p := theme.Current()
	st := theme.StyleBody
	st.Weight = weight
	spans := nameSpans(name, m.text(), p.Ink100)
	if len(spans) == 1 {
		styled(ui.Text(c, name), st).SingleLine().MinWidth(0).TextColor(colour)
		return
	}
	styled(ui.RichText(c, spans...), st).SingleLine().MinWidth(0).TextColor(colour)
}

// nameSpans splits name around the match of f: the match is underlined in
// ink. Without a match it is one span.
func nameSpans(name, f string, ink ui.Color) []ui.Span {
	s, e, ok := matchSpan(name, f)
	if !ok {
		return []ui.Span{{Text: name}}
	}
	return []ui.Span{{Text: name[:s]}, {Text: name[s:e], Underline: true, DecorationColor: ink}, {Text: name[e:]}}
}

// row builds the row i of rows.
func (m *ProjectsModal) row(c *ui.Context, rows []paletteRow, i int, home string) {
	p := theme.Current()
	r := rows[i]
	name := r.name()
	selected := i == m.sel
	label := ProjectsRow(name)
	switch r.kind {
	case rowClone:
		label = LabelCloneRow
	case rowPath:
		label = LabelPathRow
	}
	el := ui.Row(c).Label(label).AlignItems(ui.Center).Gap(theme.Space10).
		Padding(theme.Space7, theme.Space10, theme.Space7, theme.Space4).Radius(theme.Radius8).Cursor(ui.CursorPointer).
		OnClick(func() { m.sel = i; m.run(r) })
	hovered := el.Hovered()
	switch {
	case hovered && m.hover != i: // the pointer entered: it moves the selection
		m.hover, m.sel, selected = i, i, true
	case !hovered && m.hover == i:
		m.hover = -1
	}
	switch {
	case selected:
		el = el.Background(p.Selected)
	case hovered:
		el = el.Background(p.Hover)
	}
	active := selected || hovered
	if r.add() {
		el.Children(func() { m.addRowView(c, r, active, selected) })
		return
	}
	meta, age, orch := "", "", false
	if r.kind == rowOpen {
		orch = m.Orchestrated != nil && m.Orchestrated(r.group)
	} else {
		meta = tildePath(r.entry.Path, home)
		now := time.Now
		if m.Now != nil {
			now = m.Now
		}
		age = agoText(now(), r.entry.At)
	}
	weight, nameColour := 600, p.Ink100
	if r.kind == rowRecent {
		weight, nameColour = 500, p.Ink85
	}
	el.Children(func() {
		grip := ""
		if r.kind == rowOpen && active && m.text() == "" {
			grip = "⋮⋮"
		}
		styled(ui.Text(c, grip), theme.StyleSmall).Width(theme.ModalGrip).Shrink(0).NoWrap().TextColor(p.Ink30)
		ui.Box(c).Width(theme.ModalTile).Height(theme.ModalTile).Shrink(0).Radius(theme.Radius8).
			Background(p.Selected).Center().Children(func() {
			styled(ui.Text(c, projectInitials(name)), theme.StyleTile).TextColor(p.Ink62)
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			m.nameText(c, name, weight, nameColour)
			if r.kind == rowOpen {
				m.openMetaView(c, r.group, home, orch)
			} else {
				m.metaText(c, meta, 0)
			}
		})
		switch {
		case r.kind == rowOpen:
			if active {
				b := ui.ButtonBase(c).Label(ProjectsHideOf(name)).Shrink(0).Padding(theme.Space4, theme.Space8).
					Radius(theme.Radius6).Cursor(ui.CursorPointer).OnClick(func() { m.hide(r.group.CommonDir) })
				if b.Hovered() {
					b = b.Background(p.Hover)
				}
				b.Children(func() { styled(ui.Text(c, ProjectsHide), theme.StyleSmall).NoWrap().TextColor(p.Ink45) })
			}
			if selected {
				styled(ui.Text(c, "⏎ switch"), theme.StyleMeta).NoWrap().Shrink(0).TextColor(p.Ink38)
			}
		case active:
			styled(ui.Text(c, "⏎ open"), theme.StyleMeta).NoWrap().Shrink(0).TextColor(p.Ink38)
		default:
			styled(ui.Text(c, age), theme.StyleMeta).NoWrap().Shrink(0).TextColor(p.Ink30)
		}
	})
}

// metaText builds the mono line of a row. The `N need you` part is blocked
// text: the one place where the rule allows it (Q1).
func (m *ProjectsModal) metaText(c *ui.Context, meta string, needYou int) {
	p := theme.Current()
	t := ""
	if needYou > 0 {
		t = strconv.Itoa(needYou) + " need you"
	}
	if t == "" || !strings.HasSuffix(meta, t) {
		styled(ui.Text(c, meta), theme.StyleValue).SingleLine().MinWidth(0).TextColor(p.Ink38)
		return
	}
	styled(ui.RichText(c,
		ui.Span{Text: strings.TrimSuffix(meta, t)},
		ui.Span{Text: t, Color: p.Blocked},
	), theme.StyleValue).SingleLine().MinWidth(0).TextColor(p.Ink38)
}

// openMetaView builds the meta of an open row: `<path> · <branch> · <live>`.
// Only the path shrinks and gets the ellipsis. A dot comes before the live
// state when an orchestrator holds the row. `N need you` is blocked text (Q1).
func (m *ProjectsModal) openMetaView(c *ui.Context, g spaces.RepoGroup, home string, orch bool) {
	p := theme.Current()
	path, branch, live, needYou := openMetaParts(g, home)
	ink := func(s string) {
		styled(ui.Text(c, s), theme.StyleValue).NoWrap().Shrink(0).TextColor(p.Ink38)
	}
	// The parts have even gaps: a space token between them, none inside a text.
	gap := func() { ui.Box(c).Width(theme.Space6).Height(1).Shrink(0) }
	sep := func() {
		gap()
		ink("·")
		gap()
	}
	ui.Row(c).AlignItems(ui.Center).MinWidth(0).Children(func() {
		if path != "" {
			styled(ui.Text(c, path), theme.StyleValue).SingleLine().MinWidth(0).TextColor(p.Ink38)
		}
		if branch != "" {
			if path != "" {
				sep()
			}
			ink(branch)
		}
		if live == "" {
			return
		}
		if path != "" || branch != "" {
			sep()
		}
		if orch {
			ui.Box(c).Label(LabelProjectsDot).Width(theme.ModalDot).Height(theme.ModalDot).Shrink(0).
				Radius(theme.ModalDot/2).Background(p.Claude).Margin(0, theme.Space6, 0, 0)
		}
		colour := p.Ink38
		if needYou > 0 {
			colour = p.Blocked
		}
		styled(ui.Text(c, live), theme.StyleValue).NoWrap().Shrink(0).TextColor(colour)
	})
}

// tildePath shows a path in home with a leading ~.
func tildePath(path, home string) string {
	switch {
	case home == "":
		return path
	case path == home:
		return "~"
	case strings.HasPrefix(path, home+"/"):
		return "~" + path[len(home):]
	}
	return path
}
