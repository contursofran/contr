package projectsview

// The model state that the ProjectsModal part of the spec sheet sets (sheet_test.go, package
// projectsview_test). It sets the model state directly; no git runs.

import (
	"time"

	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/projects"
)

// HiddenToast is hiddenToast, for the toast cell of the sheet.
var HiddenToast = hiddenToast

// SheetState is one state of the ProjectsModal part of the spec sheet: its caption and how it
// sets the model.
type SheetState struct {
	Caption string
	Set     func(m *ProjectsModal)
}

// SheetStates are the states of the ProjectsModal part that scripts/see.sh cannot reach.
var SheetStates = []SheetState{
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

// SheetModal is a modal over fixed data, for the spec sheet.
func SheetModal() *ProjectsModal {
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
