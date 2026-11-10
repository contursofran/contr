package app

import (
	"fmt"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/usage"
)

// LabelContext is the name of the context line of the files panel.
const LabelContext = "Context"

// planLabels are the names of the plans as the meters show them.
var planLabels = map[usage.PlanID]string{usage.PlanClaude: "Claude", usage.PlanCodex: "Codex"}

// planPercent reads the percent of each plan that is used. It reads the
// sampler's last result, so it never touches a file.
type planPercent interface {
	Left(plan usage.PlanID, now uint64) (float64, bool)
	// Context is the last read context of the agent in hc terminal id.
	Context(id string) (usage.Context, bool)
}

// contextMeter is the info block of the files panel: the context of the
// selected agent (docs/spec/UsageMeter.md). Plan usage is global, so it is in
// the sidebar foot (D196, [planMeters]).
//
// terminal is the hc terminal of the selected agent ("" for none). Its context
// comes from the status line it runs (D144). Without a status line file it
// comes from labels: the Stop hook's context labels of that terminal, with the
// compaction window as the size (D205); a zero Size is no labels. With
// neither, the line shows no data.
func contextMeter(src planPercent, terminal string, labels usage.Context) kit.Meter {
	m := kit.Meter{Label: LabelContext}
	if c, ok := contextOf(src, terminal, labels); ok {
		m.Percent, m.Has, m.Value = c.Percent(), true, c.Label()
	}
	return m
}

// contextOf is the context of the agent in terminal, as the Context line reads it: from its
// status line, else from labels. ok is false when neither has it.
func contextOf(src planPercent, terminal string, labels usage.Context) (usage.Context, bool) {
	if terminal == "" {
		return usage.Context{}, false
	}
	if src != nil {
		if c, ok := src.Context(terminal); ok {
			return c, true
		}
	}
	return labels, labels.Size > 0
}

// planMeters is one meter per plan with its percent used, for the sidebar
// foot (D196).
func planMeters(src planPercent) []kit.Meter {
	var out []kit.Meter
	for _, plan := range usage.Plans {
		m := kit.Meter{Label: planLabels[plan]}
		if src != nil {
			if left, ok := src.Left(plan, usage.UnixNow()); ok {
				m.Percent, m.Has = 100-left, true
				m.Value = fmt.Sprintf("%d%%", int(m.Percent))
			}
		}
		out = append(out, m)
	}
	return out
}
