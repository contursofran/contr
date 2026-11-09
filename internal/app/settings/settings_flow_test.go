package settings

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
)

// flowTester builds FlowSection for *f; each change updates *f and is recorded.
func flowTester(t *testing.T, f *agents.Flow) (*ui.Tester, *[]agents.Flow) {
	t.Helper()
	var got []agents.Flow
	tt := ui.NewTester(func(c *ui.Context) {
		FlowSection(c, *f, func(n agents.Flow) { got = append(got, n); *f = n })
	}, 900, 700)
	return tt, &got
}

func TestFlowSectionSegmentsWriteTheFlow(t *testing.T) {
	for _, tc := range []struct {
		click string
		want  func(*agents.Flow)
	}{
		{"Never", func(f *agents.Flow) { f.Plan = "never" }},
		{"Always", func(f *agents.Flow) { f.Plan = "always" }},
		{"None", func(f *agents.Flow) { f.Review = "none" }},
		{"Every PR", func(f *agents.Flow) { f.Review = "every" }},
		{"When CI and review pass", func(f *agents.Flow) { f.Merge = "auto" }},
	} {
		f := agents.DefaultFlow()
		tt, got := flowTester(t, &f)
		if err := tt.Click(tc.click); err != nil {
			t.Fatalf("%s: %v", tc.click, err)
		}
		want := agents.DefaultFlow()
		tc.want(&want)
		if len(*got) != 1 || (*got)[0] != want {
			t.Errorf("%s: onChange %+v, want %+v", tc.click, *got, want)
		}
	}
}

func TestFlowSectionPresetsSetTheRows(t *testing.T) {
	for _, ps := range FlowPresets {
		f := agents.DefaultFlow()
		f.Plan, f.Review = "always", "every" // matches no preset
		tt, got := flowTester(t, &f)
		if err := tt.Click(ps.Name); err != nil {
			t.Fatalf("%s: %v", ps.Name, err)
		}
		if len(*got) != 1 || (*got)[0].Plan != ps.Plan || (*got)[0].Review != ps.Review || (*got)[0].Workers != 4 {
			t.Errorf("%s: %+v", ps.Name, *got)
		}
	}
	if FlowPresetIndex(agents.DefaultFlow()) != 1 {
		t.Error("the default flow is not Build and review")
	}
	if FlowPresetIndex(agents.Flow{Plan: "never", Review: "code"}) != -1 {
		t.Error("a flow that matches no preset selects a preset")
	}
}

func TestFlowSectionCodeReviewNote(t *testing.T) {
	f := agents.DefaultFlow()
	tt, _ := flowTester(t, &f)
	if !tt.HasText(FlowDocsNote) {
		t.Error("Code PRs: no docs-only note")
	}
	f.Review = "every"
	tt.Frame()
	if tt.HasText(FlowDocsNote) {
		t.Error("Every PR: the docs-only note shows")
	}
}

// stepAt clicks the up or down arrow of the stepper named label.
func stepAt(t *testing.T, tt *ui.Tester, label string, up bool) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no stepper %q", label)
	}
	y := r.Y + r.H*3/4
	if up {
		y = r.Y + r.H/4
	}
	tt.ClickAt(r.X+r.W/2, y)
}

func TestFlowSectionSteppersStopAtOne(t *testing.T) {
	for _, tc := range []struct {
		label, unit string
		get         func(agents.Flow) int
	}{
		{LabelDebugger, FlowDebuggerUnit, func(f agents.Flow) int { return f.DebuggerAfter }},
		{LabelWorkers, FlowWorkersUnit, func(f agents.Flow) int { return f.Workers }},
	} {
		f := agents.DefaultFlow()
		f.DebuggerAfter, f.Workers = 2, 2
		tt, got := flowTester(t, &f)
		stepAt(t, tt, tc.label+" "+tc.unit, true)
		if tc.get(f) != 3 {
			t.Errorf("%s up: %d", tc.label, tc.get(f))
		}
		for range 4 {
			stepAt(t, tt, tc.label+" "+tc.unit, false)
		}
		if tc.get(f) != 1 {
			t.Errorf("%s down past 1: %d", tc.label, tc.get(f))
		}
		for _, g := range *got {
			if tc.get(g) < 1 {
				t.Errorf("%s: onChange with %+v", tc.label, g)
			}
		}
	}
}

func TestFlowStepsDimOffSteps(t *testing.T) {
	s := FlowSteps(agents.Flow{Plan: "never", Review: "none", DebuggerAfter: 3, Workers: 1, Merge: "auto"})
	if s[0].On || s[2].On || !s[1].On || s[3].Detail != "debugger after 3" || s[4].Detail != "when CI and review pass" {
		t.Errorf("steps: %+v", s)
	}
	s = FlowSteps(agents.DefaultFlow())
	if !s[0].On || s[0].Detail != "when needed" || s[2].Detail != "code PRs" || s[4].Detail != "after your OK" {
		t.Errorf("default steps: %+v", s)
	}
}
