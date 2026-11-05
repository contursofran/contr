package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/contursofran/contr/internal/hc"
)

// fterm is an hc terminal that has no contr labels.
func fterm(addr string, extra map[string]any) map[string]any {
	t := map[string]any{"address": addr, "state": "live", "labels": map[string]string{}, "cwd": "/work/" + addr, "turn": 1}
	for k, v := range extra {
		t[k] = v
	}
	return t
}

func decodeT(t *testing.T, v map[string]any) Worker {
	t.Helper()
	w, err := decodeWorker(raw(v))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestForeignClassification(t *testing.T) {
	cases := []struct {
		name  string
		term  map[string]any
		role  string
		label string
	}{
		{"claude foreground", fterm("t1", map[string]any{"argv": []string{"zsh"}, "foreground": map[string]any{"name": "claude"}}), RoleOrchestrator, ""},
		{"claude version foreground", fterm("t1", map[string]any{"argv": []string{"zsh"}, "foreground": map[string]any{"name": "2.1.30"}}), RoleOrchestrator, ""},
		{"claude argv", fterm("t1", map[string]any{"argv": []string{"/usr/local/bin/claude", "--resume"}}), RoleOrchestrator, ""},
		{"hc status", fterm("t1", map[string]any{"argv": []string{"zsh"}, "status": map[string]any{"state": "idle"}}), RoleOrchestrator, ""},
		{"shell", fterm("t1", map[string]any{"argv": []string{"/bin/zsh"}, "foreground": map[string]any{"name": "zsh"}}), RoleTerminal, ""},
		{"no program info", fterm("t1", nil), RoleTerminal, ""},
	}
	for _, c := range cases {
		w := decodeT(t, c.term)
		if w.Role != c.role || !w.Foreign || w.Worktree != "/work/t1" || w.Name != "t1" || !w.Live {
			t.Errorf("%s: role=%q foreign=%v worktree=%q name=%q live=%v", c.name, w.Role, w.Foreign, w.Worktree, w.Name, w.Live)
		}
	}
	// A named terminal keeps its name.
	if w := decodeT(t, fterm("t1", map[string]any{"name": "2"})); w.Name != "2" {
		t.Errorf("name = %q, want 2", w.Name)
	}
}

func TestLabeledTerminalKeepsItsRole(t *testing.T) {
	w := decodeT(t, term("t1", "alpha", "live", builderLabels, nil))
	if w.Foreign || w.Role != "builder" || w.Worktree != "/w/a" {
		t.Errorf("labeled worker changed: %+v", w)
	}
	// Labeled shell with a cwd: the label wins over hc's folder.
	tt := term("t2", "sh", "live", map[string]string{LabelApp: AppAgent, LabelRole: RoleTerminal, LabelWorktree: "/w/b"}, nil)
	tt["cwd"] = "/elsewhere"
	if w := decodeT(t, tt); w.Worktree != "/w/b" || w.Foreign {
		t.Errorf("labeled terminal: %+v", w)
	}
}

func TestWatchAllListsForeignLiveTerminals(t *testing.T) {
	ended := fterm("t3", map[string]any{"state": "finished"})
	s := &fakeStream{
		snap: hc.Snapshot{Terminals: []json.RawMessage{
			raw(term("t1", "alpha", "live", builderLabels, nil)),
			raw(fterm("t2", map[string]any{"foreground": map[string]any{"name": "claude"}})),
			raw(ended),
		}},
		events: make(chan hc.Event, 4),
	}
	gets := map[string]map[string]any{
		"t4": fterm("t4", map[string]any{"argv": []string{"sh"}}),
		"t2": fterm("t2", map[string]any{"state": "finished"}),
	}
	f := &fakeCore{stream: s, handle: map[string]func(map[string]any) (any, error){
		"terminal.get": func(p map[string]any) (any, error) { return gets[p["terminal"].(string)], nil },
	}}
	s.events <- hc.Event{Method: "event", Terminal: "t4", Type: "created"}
	s.events <- hc.Event{Method: "event", Terminal: "t2", Type: "finished"} // an ended foreign terminal goes
	s.events <- hc.Event{Method: "missed", Data: json.RawMessage(`{"seq":"x"}`)}

	var lists [][]string
	_ = New(f, DefaultConfig()).WatchAll(context.Background(), func(ws []Worker) {
		var names []string
		for _, w := range ws {
			names = append(names, w.Address+":"+w.Role)
		}
		lists = append(lists, names)
	})
	if len(f.filterIn.Labels) != 0 {
		t.Errorf("WatchAll subscribed with labels %v", f.filterIn.Labels)
	}
	want := [][]string{{"t1:builder", "t2:orchestrator"}, {"t1:builder", "t2:orchestrator", "t4:terminal"}, {"t1:builder", "t4:terminal"}}
	if len(lists) < 3 {
		t.Fatalf("lists = %v, want to start with %v", lists, want)
	}
	for i := range want {
		if len(lists[i]) != len(want[i]) {
			t.Fatalf("list %d = %v, want %v", i, lists[i], want[i])
		}
		for j := range want[i] {
			if lists[i][j] != want[i][j] {
				t.Fatalf("list %d = %v, want %v", i, lists[i], want[i])
			}
		}
	}
}

func TestWatchStaysOnContrWorkers(t *testing.T) {
	s := &fakeStream{
		snap:   hc.Snapshot{Terminals: []json.RawMessage{raw(fterm("t2", nil))}},
		events: make(chan hc.Event, 1),
	}
	close(s.events)
	f := &fakeCore{stream: s}
	var n []int
	_ = New(f, DefaultConfig()).Watch(context.Background(), func(ws []Worker) { n = append(n, len(ws)) })
	if f.filterIn.Labels[LabelApp] != AppAgent {
		t.Errorf("Watch filter = %v, want contr=agent", f.filterIn.Labels)
	}
	if len(n) != 1 || n[0] != 0 {
		t.Errorf("Watch listed a foreign terminal: %v", n)
	}
}
