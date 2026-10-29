// Package usage holds plan usage for contr's status bar: the usage model, with
// Claude Code's status line and Codex's rollout logs as sources. It has no UI,
// so `contr statusline` runs without a window.
package usage

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
)

// PlanID names a plan whose usage contr shows. The values are the ids used in
// files and element ids.
type PlanID string

// The plans contr shows.
const (
	PlanClaude PlanID = "claude"
	PlanCodex  PlanID = "codex"
)

// Plans lists the plans in bar order (docs/DESIGN.md). v0.1 has no OpenCode Go
// segment.
var Plans = [...]PlanID{PlanClaude, PlanCodex}

// Label is the name shown in the bar.
func (p PlanID) Label() string { return string(p) }

// ID is the id used in files and element ids.
func (p PlanID) ID() string { return string(p) }

// Window is one limit window, as its plan reports it.
type Window struct {
	// Kind is "five_hour", "seven_day", "rolling", "weekly", "monthly",
	// "primary" or "secondary".
	Kind string `json:"kind"`
	// UsedPercent is 0 to 100 as the plan reports it; above 100 when the
	// window is over.
	UsedPercent float64 `json:"used_percent"`
	// ResetsAt is the Unix second when the window resets; nil when the plan
	// does not say.
	ResetsAt *uint64 `json:"resets_at"`
}

// PlanUsage holds the windows one plan reported, and when they were taken.
type PlanUsage struct {
	Plan    PlanID
	Windows []Window
	// ReadAt is the Unix second when the windows were taken.
	ReadAt uint64
}

// LeftPercent is the percent left for the bar. ok is false when the plan
// reported no window.
func (u PlanUsage) LeftPercent(now uint64) (left float64, ok bool) {
	return leftOfTightestWindow(u.Windows, now)
}

// leftOfTightestWindow decides which window counts as "left" (owner question
// D60; recommended: the tighter of the windows). Change the rule here only.
//
// The result is 100 minus the highest used percent. A window whose ResetsAt is
// at or before now has already reset and counts as 0 used. The result is
// clamped to 0 to 100.
func leftOfTightestWindow(windows []Window, now uint64) (float64, bool) {
	if len(windows) == 0 {
		return 0, false
	}
	mostUsed := 0.0
	for _, w := range windows {
		used := w.UsedPercent
		if w.ResetsAt != nil && *w.ResetsAt <= now {
			used = 0
		}
		mostUsed = math.Max(mostUsed, used)
	}
	return math.Min(math.Max(100-mostUsed, 0), 100), true
}

// LeftLabel is the bar's number: the left percent rounded, or "–" when there
// is no data.
func LeftLabel(left float64, ok bool) string {
	if !ok {
		return "–"
	}
	return fmt.Sprintf("%d%%", int64(math.Round(left)))
}

// StateDir is where contr keeps its own files. See StateDirFrom. It returns ""
// when no folder can be found.
func StateDir() string {
	home, _ := os.UserHomeDir()
	return StateDirFrom(os.Getenv, home, runtime.GOOS == "darwin")
}

// StateDirFrom returns $CONTR_STATE_DIR; else on macOS
// "~/Library/Application Support/contr"; else $XDG_STATE_HOME/contr, or
// ~/.local/state/contr. Empty variables count as unset. The environment and
// home are passed in so tests need no real variables. An empty home means no
// home; the result is "" when no folder can be found.
func StateDirFrom(env func(string) string, home string, macos bool) string {
	if dir := env("CONTR_STATE_DIR"); dir != "" {
		return dir
	}
	if macos {
		if home == "" {
			return ""
		}
		return filepath.Join(home, "Library/Application Support/contr")
	}
	if state := env("XDG_STATE_HOME"); state != "" {
		return filepath.Join(state, "contr")
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".local/state/contr")
}
