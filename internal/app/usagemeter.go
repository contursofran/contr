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
}

// usageMeters is the info block of the files panel: the context of the
// selected agent, then one line per plan with its percent used
// (docs/spec/UsageMeter.md).
//
// The context has no data source yet (follow-up of #350), so it shows no data.
func usageMeters(src planPercent) []kit.Meter {
	out := []kit.Meter{{Label: LabelContext}}
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
