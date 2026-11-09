package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
)

// mergeRig is a footer rig whose pane tells an orchestrator through a fake source.
func mergeRig(t *testing.T, addr string, src *source) (*prRig, *ghFake) {
	t.Helper()
	r, g := footerRig(t, nil)
	r.pane.Orchestrator = func(dir string) string {
		if dir != "/w" {
			t.Errorf("orchestrator asked for %q", dir)
		}
		return addr
	}
	r.pane.Send = src.Send
	return r, g
}

func mergeClick(t *testing.T, r *prRig, g *ghFake) {
	t.Helper()
	g.pr = strings.Replace(g.pr, `"state":"OPEN"`, `"state":"MERGED"`, 1)
	if err := r.tt.Click(LabelPRMerge); err != nil {
		t.Fatal(err)
	}
	r.flush() // the merge and the re-read
	r.flush() // the send
}

func TestPRMergeMarksItemDoneAndTellsPrincipal(t *testing.T) {
	src := &source{}
	r, g := mergeRig(t, "orch-1", src)
	mergeClick(t, r, g)
	if len(src.told) != 1 || src.told[0] != [2]string{"orch-1", "contr: PR #642 merged at " + shortSHA(r.pane.Loaded().HeadSHA)} {
		t.Fatalf("told %v", src.told)
	}
	if !r.tt.HasText(LabelPRMerged) || r.tt.HasText(LabelPRMerge) || r.tt.HasText(LabelPRComment) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestShortSHA(t *testing.T) {
	if shortSHA("0123456789") != "0123456" || shortSHA("abc") != "abc" {
		t.Error("shortSHA")
	}
}

func TestPRMergeNoOrchestratorSaysNoOneTold(t *testing.T) {
	src := &source{}
	r, g := mergeRig(t, "", src)
	mergeClick(t, r, g)
	if len(src.told) != 0 {
		t.Errorf("told %v", src.told)
	}
	if !r.tt.HasText(LabelPRMergedNoOne) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRMergeTellFailsShowsError(t *testing.T) {
	src := &source{tellErr: errors.New("no terminal")}
	r, g := mergeRig(t, "orch-1", src)
	mergeClick(t, r, g)
	if !r.tt.HasText(LabelPRMergedBare) || !r.tt.HasText("principal not told: no terminal") || r.tt.HasText(LabelPRMerged) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRMergedFooterTakesNoMoreWrites(t *testing.T) {
	src := &source{}
	r, g := mergeRig(t, "orch-1", src)
	mergeClick(t, r, g)
	n := len(g.writes())
	r.pane.merge()
	r.flush()
	if len(g.writes()) != n || len(src.told) != 1 {
		t.Errorf("a second merge ran: writes %v told %v", g.writes(), src.told)
	}
}

func TestPROpenClearsTheMergedLine(t *testing.T) {
	src := &source{}
	r, g := mergeRig(t, "orch-1", src)
	mergeClick(t, r, g)
	g.pr = strings.Replace(g.pr, `"state":"MERGED"`, `"state":"OPEN"`, 1)
	r.pane.Open("/w", 642)
	r.flush()
	if r.tt.HasText(LabelPRMerged) || !r.tt.HasText(LabelPRMerge) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestFramePROrchestratorFollowsTheWorkersProject(t *testing.T) {
	f := NewFrame()
	f.agentSnap = func() agentstate.Snapshot {
		return agentstate.Snapshot{
			Orchestrators: []agents.Worker{{Address: "o-a", Project: "a"}, {Address: "o-b", Project: "b"}},
			Workers:       []agents.Worker{{Address: "w1", Project: "b", Worktree: "/wt/b"}},
		}
	}
	f.takeSnapshot()
	f.openProject = "a"
	if got := f.prOrchestrator("/wt/b"); got != "o-b" {
		t.Errorf("worker folder gave %q", got)
	}
	if got := f.prOrchestrator("/other"); got != "o-a" {
		t.Errorf("open project gave %q", got)
	}
	f.openProject = "none"
	if got := f.prOrchestrator("/other"); got != "" {
		t.Errorf("no orchestrator gave %q", got)
	}
}
