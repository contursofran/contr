package app

import (
	"bytes"
	"image/png"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/projects"
)

// The ProjectsModal part of the spec sheet (D207, S6): the states that
// scripts/see.sh cannot reach. It sets the model state directly; no git runs.

const (
	pmW, pmH = 600, 520 // size of one state, in points
	pmGap    = 16
	pmCaptH  = 24
	pmCols   = 2
	pmRows   = 5 // the toast state is the last cell
)

// pmState is one state of the part: its caption and how it sets the model.
type pmState struct {
	caption string
	set     func(m *ProjectsModal)
}

var pmStates = []pmState{
	{"at rest, with an orchestrator dot", func(m *ProjectsModal) {}},
	{"filtering: match underline", func(m *ProjectsModal) { m.query = "ac" }},
	{"no match", func(m *ProjectsModal) { m.query = "zzq" }},
	{"Clone row, idle", func(m *ProjectsModal) { m.query = "acme/widget" }},
	{"clone RUNNING over filter results after an edit", func(m *ProjectsModal) {
		m.cloning = true
		m.job = cloneJob{owner: "acme", repo: "widget", query: "acme/widget", dest: "/home/dev/code/widget"}
		m.query = "ac"
	}},
	{"clone error, Try again", func(m *ProjectsModal) {
		m.query = "acme/widget"
		m.cloneErr = "Authentication failed · check the name and your GitHub access"
	}},
	{"path row", func(m *ProjectsModal) { m.query = "/home/dev/code/new" }},
	{"path is not a folder", func(m *ProjectsModal) {
		m.query = "/home/dev/notes.txt"
		m.pathErr = "not a folder: /home/dev/notes.txt"
	}},
}

// pmModal is a modal over fixed data.
func pmModal() *ProjectsModal {
	st := &projects.Store{}
	now := time.Now()
	st.Touch("/home/dev/code/old-site/.git", "/home/dev/code/old-site", now.Add(-3*time.Hour))
	st.Touch("/home/dev/code/dotfiles/.git", "/home/dev/code/dotfiles", now.Add(-26*time.Hour))
	hold := func(s string) *string { return &s }
	groups := []spaces.RepoGroup{
		{CommonDir: "/home/dev/code/acme/.git", Name: "acme", Branch: "main", Rows: []spaces.WorktreeRow{
			{Root: "/home/dev/code/acme", IsMain: true, Hold: hold("orchestrating")}}},
		{CommonDir: "/home/dev/code/contr/.git", Name: "contr", Branch: "main", Rows: []spaces.WorktreeRow{
			{Root: "/home/dev/code/contr", IsMain: true, Hold: hold("2 working")}, {HoldYou: true}}},
		{CommonDir: "/home/dev/code/api/.git", Name: "api", Branch: "main", Rows: []spaces.WorktreeRow{
			{Root: "/home/dev/code/api", IsMain: true}}},
	}
	return &ProjectsModal{
		Groups:       func() []spaces.RepoGroup { return groups },
		Store:        func() *projects.Store { return st },
		CloneParent:  "/home/dev/code",
		PickFolder:   func() (string, error) { return "", ErrPickCancelled },
		PickParent:   func(string) (string, error) { return "", ErrPickCancelled },
		Now:          func() time.Time { return now },
		Orchestrated: func(g spaces.RepoGroup) bool { return g.Name == "acme" },
		hover:        -1,
		open:         true,
	}
}

// pmImage renders one state and returns it as a bitmap for ui.Image.
func pmImage(draw func(c *ui.Context)) *ui.Bitmap {
	img := ui.Render(func(c *ui.Context) {
		theme.Apply(c)
		ui.Column(c).Width(pmW).Height(pmH).Background(theme.Current().Ground).Font(theme.FontFamilySans).
			FontSize(theme.FontSize).Children(func() { draw(c) })
	}, pmW, pmH, 1)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil
	}
	bm, err := ui.DecodeBitmap(b.Bytes())
	if err != nil {
		return nil
	}
	return bm
}

// pmToast draws the hide toast of the frame, with no modal.
func pmToast(c *ui.Context) {
	f := &Frame{Toasts: state.NewToasts(nil)}
	f.Toasts.ShowAction(hiddenToast("api"), ProjectsUndo, func() {})
	f.toastView(c)
}

// projectsModalSheet draws every state under its caption.
func projectsModalSheet(c *ui.Context) {
	type cell struct {
		caption string
		bm      *ui.Bitmap
	}
	var cells []cell
	for _, s := range pmStates {
		m := pmModal()
		s.set(m)
		cells = append(cells, cell{s.caption, pmImage(m.View)})
	}
	cells = append(cells, cell{"hide toast with Undo", pmImage(pmToast)})
	ui.Column(c).Gap(pmGap).Children(func() {
		for r := 0; r < len(cells); r += pmCols {
			ui.Row(c).Gap(pmGap).Children(func() {
				for _, k := range cells[r:min(r+pmCols, len(cells))] {
					ui.Column(c).Width(pmW).Children(func() {
						ui.Text(c, k.caption).Height(pmCaptH).TextColor(theme.Current().Ink62)
						if k.bm != nil {
							ui.Image(c, k.bm)
						}
					})
				}
			})
		}
	})
}
