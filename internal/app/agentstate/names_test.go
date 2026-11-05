package agentstate

import (
	"testing"

	"github.com/contursofran/contr/internal/agents"
)

func fw(addr, name, title, project string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Title: title, Project: project, Worktree: "/x/" + project, Foreign: true, Live: true}
}

func TestNameForeignOrder(t *testing.T) {
	cases := map[string]struct {
		w    agents.Worker
		want string
	}{
		"hc name wins":        {fw("t1", "scout", "My title", "contr"), "scout"},
		"title when usable":   {fw("t1", "t1", "✳ Fix the build", "contr"), "✳ Fix the build"},
		"path title skipped":  {fw("t1", "t1", "~/code/contr", "contr"), "contr"},
		"abs path skipped":    {fw("t1", "t1", "/x/contr", "contr"), "contr"},
		"shell name skipped":  {fw("t1", "t1", "zsh", "contr"), "contr"},
		"version skipped":     {fw("t1", "t1", "2.1.30", "contr"), "contr"},
		"empty title skipped": {fw("t1", "t1", "", "contr"), "contr"},
	}
	for name, c := range cases {
		ws := []agents.Worker{c.w}
		nameForeign(ws)
		if got := ws[0].DisplayName(); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestNameForeignCommandTitleSkipped(t *testing.T) {
	w := fw("t1", "t1", "vim", "contr")
	w.Command = "/usr/bin/vim"
	ws := []agents.Worker{w}
	nameForeign(ws)
	if ws[0].DisplayName() != "contr" {
		t.Errorf("got %q", ws[0].DisplayName())
	}
}

func TestNameForeignNumbersProjectsWithMoreThanOne(t *testing.T) {
	ws := []agents.Worker{
		fw("t9", "t9", "", "contr"), fw("t2", "t2", "", "contr"), fw("t5", "t5", "", "aerum"),
		fw("t3", "t3", "", "contr"), fw("t4", "scout", "", "contr"),
	}
	nameForeign(ws)
	got := map[string]string{}
	for _, w := range ws {
		got[w.Address] = w.DisplayName()
	}
	want := map[string]string{"t2": "contr", "t3": "contr 2", "t9": "contr 3", "t5": "aerum", "t4": "scout"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
}

func TestNameForeignKeepsAddressWithoutProject(t *testing.T) {
	w := agents.Worker{Address: "t7", Name: "t7", Foreign: true, Live: true}
	ws := []agents.Worker{w}
	nameForeign(ws)
	if ws[0].DisplayName() != "t7" {
		t.Errorf("got %q", ws[0].DisplayName())
	}
}

func TestNameForeignLeavesContrWorkersAlone(t *testing.T) {
	ws := []agents.Worker{{Address: "t1", Name: "t1", Title: "Some title", Project: "contr"}}
	nameForeign(ws)
	if ws[0].Shown != "" {
		t.Errorf("Shown %q", ws[0].Shown)
	}
}

func TestSnapshotNamesForeignTerminals(t *testing.T) {
	r := newRig(t,
		fterm("t1", "/x/alpha", map[string]any{"foreground": map[string]any{"name": "claude"}}),
		fterm("t2", "/x/alpha", map[string]any{"argv": []string{"zsh"}}),
		fterm("t3", "/x/alpha", map[string]any{"argv": []string{"zsh"}, "title": "~/x/alpha"}),
		fterm("t4", "/x/alpha", map[string]any{"argv": []string{"zsh"}, "name": "build"}),
	)
	r.m.resolve = func(dir string) (string, string) { return "alpha", "" }
	r.start(t)
	waitFor(t, "names", func() bool {
		s := r.m.Snapshot()
		return len(s.Terminals) == 3 && s.Terminals[0].Project != ""
	})
	s := r.m.Snapshot()
	if got := s.Orchestrators[0].DisplayName(); got != "alpha" {
		t.Errorf("orchestrator %q", got)
	}
	names := map[string]string{}
	for _, w := range s.Terminals {
		names[w.Address] = w.DisplayName()
	}
	if names["t2"] != "alpha" || names["t3"] != "alpha 2" || names["t4"] != "build" {
		t.Errorf("terminals %v", names)
	}
}
