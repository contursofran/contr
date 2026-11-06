package app

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/theme"
)

// Flow section texts (D187, docs/spec/FlowSection.md).
const (
	LabelFlow        = "Flow"
	FlowHint         = "How the orchestrator takes an issue to a merge"
	LabelFlowPreset  = "Flow preset"
	LabelPlanFirst   = "Plan first"
	LabelFlowReview  = "Review"
	LabelDebugger    = "Debugger after"
	LabelWorkers     = "Workers at once"
	LabelMerge       = "Merge"
	FlowDocsNote     = "docs-only PRs: the orchestrator reads the diff"
	FlowDebuggerUnit = "failed fix rounds"
	FlowWorkersUnit  = "reviewers count"
	flowStepperLimit = 99
)

// flowPreset is a named set of plan and review values.
type flowPreset struct {
	Name, Plan, Review string
}

// FlowPresets are the presets of the Flow section, in their order.
var FlowPresets = []flowPreset{
	{"Builder only", "never", "none"},
	{"Build and review", "when-needed", "code"},
	{"Plan, build, review", "always", "code"},
}

// flowChoice is one segment: its label and the value it writes.
type flowChoice struct{ Name, Value string }

var (
	flowPlanChoices   = []flowChoice{{"Never", "never"}, {"When needed", "when-needed"}, {"Always", "always"}}
	flowReviewChoices = []flowChoice{{"None", "none"}, {"Code PRs", "code"}, {"Every PR", "every"}}
	flowMergeChoices  = []flowChoice{{"Ask me", "ask"}, {"When CI and review pass", "auto"}}
)

// FlowPresetIndex gives the preset that f matches, or -1.
func FlowPresetIndex(f agents.Flow) int {
	for i, p := range FlowPresets {
		if f.Plan == p.Plan && f.Review == p.Review {
			return i
		}
	}
	return -1
}

// FlowSection draws the Flow section of Settings → Agents for f. Each change calls onChange with
// the new flow; the caller saves it and builds the section again with it.
func FlowSection(c *ui.Context, f agents.Flow, onChange func(agents.Flow)) {
	p := theme.Current()
	set := func(n agents.Flow) {
		if n != f && onChange != nil {
			onChange(n)
		}
	}
	ui.Column(c).Shrink(0).Gap(theme.Space10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
			ui.Text(c, LabelFlow).Bold().TextColor(p.Ink95)
			ui.Text(c, FlowHint).Grow(1).TextColor(p.Ink50)
		})
		names := make([]string, len(FlowPresets))
		for i, ps := range FlowPresets {
			names[i] = ps.Name
		}
		if i := flowSegments(c, "flow-preset", LabelFlowPreset, names, FlowPresetIndex(f)); i >= 0 {
			n := f
			n.Plan, n.Review = FlowPresets[i].Plan, FlowPresets[i].Review
			set(n)
		}
		flowSteps(c, f)
		flowRow(c, LabelPlanFirst, "", func() {
			if v, ok := flowPick(c, "flow-plan", LabelPlanFirst, flowPlanChoices, f.Plan); ok {
				n := f
				n.Plan = v
				set(n)
			}
		})
		note := ""
		if f.Review == "code" {
			note = FlowDocsNote
		}
		flowRow(c, LabelFlowReview, note, func() {
			if v, ok := flowPick(c, "flow-review", LabelFlowReview, flowReviewChoices, f.Review); ok {
				n := f
				n.Review = v
				set(n)
			}
		})
		flowRow(c, LabelDebugger, "", func() {
			if v, ok := flowStepper(c, LabelDebugger, FlowDebuggerUnit, f.DebuggerAfter); ok {
				n := f
				n.DebuggerAfter = v
				set(n)
			}
		})
		flowRow(c, LabelWorkers, "", func() {
			if v, ok := flowStepper(c, LabelWorkers, FlowWorkersUnit, f.Workers); ok {
				n := f
				n.Workers = v
				set(n)
			}
		})
		flowRow(c, LabelMerge, "", func() {
			if v, ok := flowPick(c, "flow-merge", LabelMerge, flowMergeChoices, f.Merge); ok {
				n := f
				n.Merge = v
				set(n)
			}
		})
	})
}

// flowRow is a row: the label at the left, the control, and an optional note below.
func flowRow(c *ui.Context, label, note string, control func()) {
	p := theme.Current()
	ui.Column(c).Shrink(0).Gap(theme.Space4).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
			ui.Text(c, label).Grow(1).TextColor(p.Ink62)
			control()
		})
		if note != "" {
			ui.Text(c, note).Font(theme.StyleMeta.Family()).FontSize(theme.StyleMeta.Size).TextColor(p.Ink50)
		}
	})
}

// flowSegments draws a segmented control with sel chosen (-1: none). It gives the index the owner
// picked in this build, or -1.
func flowSegments(c *ui.Context, key, label string, names []string, sel int) int {
	p := theme.Current()
	// MyGo clamps the choice into range, so "none chosen" is one more index that has no segment.
	picked := sel
	if picked < 0 {
		picked = len(names)
	}
	start := picked
	seg := ui.SegmentedBase(c.Key(key), &picked, len(names)+1)
	seg.Track.Label(label).Shrink(0).Padding(theme.Space2).Radius(theme.CardRadius).Background(p.Hover).Children(func() {
		for i, name := range names {
			e := seg.Segment(i).Label(name).Padding(theme.Space4, theme.Space12).Radius(theme.CardRadius)
			text := p.Ink62
			if i == picked {
				e.Background(p.Selected)
				text = p.Ink100
			}
			e.Children(func() { ui.Text(c, name).TextColor(text) })
		}
	})
	if seg.Track.Changed() && picked != start && picked < len(names) {
		return picked
	}
	return -1
}

// flowPick is flowSegments over choices: it gives the value picked and true, or false.
func flowPick(c *ui.Context, key, label string, choices []flowChoice, cur string) (string, bool) {
	names := make([]string, len(choices))
	sel := -1
	for i, ch := range choices {
		names[i] = ch.Name
		if ch.Value == cur {
			sel = i
		}
	}
	if i := flowSegments(c, key, label, names, sel); i >= 0 {
		return choices[i].Value, true
	}
	return "", false
}

// flowStepper draws the value, its unit and a MyGo stepper that stops at 1. It gives the new
// value and true when the owner stepped it.
func flowStepper(c *ui.Context, label, unit string, cur int) (int, bool) {
	p := theme.Current()
	v := float64(cur)
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Space6).Children(func() {
		ui.Text(c, strconv.Itoa(cur)).Font(theme.StyleValue.Family()).FontSize(theme.StyleValue.Size).TextColor(p.Ink95)
		ui.Text(c, unit).TextColor(p.Ink50)
		ui.Stepper(c.Key("flow-step-"+label), &v, 1, flowStepperLimit, 1).Label(label + " " + unit)
	})
	if n := int(v); n != cur && n >= 1 {
		return n, true
	}
	return 0, false
}

// FlowStep is one step of the row of steps: its name, its detail and whether it is on.
type FlowStep struct {
	Name, Detail string
	On           bool
}

// FlowSteps gives the row of steps for f.
func FlowSteps(f agents.Flow) []FlowStep {
	plan := map[string]string{"never": "off", "when-needed": "when needed", "always": "always"}[f.Plan]
	review := map[string]string{"none": "off", "code": "code PRs", "every": "every PR"}[f.Review]
	merge := map[string]string{"ask": "after your OK", "auto": "when CI and review pass"}[f.Merge]
	return []FlowStep{
		{"Planner", plan, f.Plan != "never"},
		{"Builder", "", true},
		{"Reviewer", review, f.Review != "none"},
		{"Fix rounds", "debugger after " + strconv.Itoa(f.DebuggerAfter), true},
		{"Merge", merge, true},
	}
}

// flowSteps draws the row of steps. An off step is dim (ink-38).
func flowSteps(c *ui.Context, f agents.Flow) {
	p := theme.Current()
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Space6).Padding(theme.Space8).
		Radius(theme.CardRadius).Background(p.Hover).Children(func() {
		for i, s := range FlowSteps(f) {
			if i > 0 {
				ui.Text(c, "→").TextColor(p.Ink38)
			}
			name, detail := p.Ink85, p.Ink50
			if !s.On {
				name, detail = p.Ink38, p.Ink38
			}
			ui.Column(c).Shrink(0).Children(func() {
				ui.Text(c, s.Name).TextColor(name)
				if s.Detail != "" {
					ui.Text(c, s.Detail).Font(theme.StyleMeta.Family()).FontSize(theme.StyleMeta.Size).TextColor(detail)
				}
			})
		}
	})
}
