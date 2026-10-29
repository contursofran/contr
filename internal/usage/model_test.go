package usage

import "testing"

func u64(v uint64) *uint64 { return &v }

var resetLater = u64(2_000)

func window(kind string, used float64, resetsAt *uint64) Window {
	return Window{Kind: kind, UsedPercent: used, ResetsAt: resetsAt}
}

func usageOf(windows ...Window) PlanUsage {
	return PlanUsage{Plan: PlanClaude, Windows: windows, ReadAt: 1_000}
}

func envOf(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func wantLeft(t *testing.T, u PlanUsage, now uint64, want float64, wantOK bool) {
	t.Helper()
	got, ok := u.LeftPercent(now)
	if ok != wantOK || got != want {
		t.Fatalf("LeftPercent = %v, %v; want %v, %v", got, ok, want, wantOK)
	}
}

func TestBarOrderAndIDs(t *testing.T) {
	if Plans != [...]PlanID{PlanClaude, PlanCodex} {
		t.Fatalf("Plans = %v", Plans)
	}
	for plan, want := range map[PlanID]string{PlanClaude: "claude", PlanCodex: "codex"} {
		if plan.Label() != want || plan.ID() != want {
			t.Fatalf("%v: label %q id %q", plan, plan.Label(), plan.ID())
		}
	}
}

func TestLeftTakesTheTighterWindow(t *testing.T) {
	u := usageOf(window("five_hour", 23.5, resetLater), window("seven_day", 41.2, resetLater))
	wantLeft(t, u, 1_000, 100.0-41.2, true)
}

func TestAWindowPastItsResetCountsAsUnused(t *testing.T) {
	u := usageOf(window("five_hour", 90, u64(999)), window("seven_day", 30, resetLater))
	wantLeft(t, u, 1_000, 70, true)
}

func TestAWindowResettingExactlyNowCountsAsUnused(t *testing.T) {
	wantLeft(t, usageOf(window("five_hour", 90, u64(1_000))), 1_000, 100, true)
}

func TestAWindowWithoutAResetTimeCountsAsUsed(t *testing.T) {
	wantLeft(t, usageOf(window("primary", 40, nil)), 1_000, 60, true)
}

func TestLeftIsClampedToZeroToHundred(t *testing.T) {
	wantLeft(t, usageOf(window("five_hour", 150, resetLater)), 1_000, 0, true)
	wantLeft(t, usageOf(window("five_hour", -5, resetLater)), 1_000, 100, true)
}

func TestLeftIsNoneWithoutWindows(t *testing.T) {
	wantLeft(t, usageOf(), 1_000, 0, false)
}

func TestLabelRoundsAndDashesWithoutData(t *testing.T) {
	for _, c := range []struct {
		left float64
		ok   bool
		want string
	}{{58.8, true, "59%"}, {0.4, true, "0%"}, {100, true, "100%"}, {0, false, "–"}} {
		if got := LeftLabel(c.left, c.ok); got != c.want {
			t.Errorf("LeftLabel(%v, %v) = %q; want %q", c.left, c.ok, got, c.want)
		}
	}
}

func TestStateDirPrefersTheOverride(t *testing.T) {
	env := envOf(map[string]string{"CONTR_STATE_DIR": "/x/state", "XDG_STATE_HOME": "/x/xdg"})
	if got := StateDirFrom(env, "/h", true); got != "/x/state" {
		t.Fatalf("macos: %q", got)
	}
	env = envOf(map[string]string{"CONTR_STATE_DIR": "/x/state"})
	if got := StateDirFrom(env, "/h", false); got != "/x/state" {
		t.Fatalf("other: %q", got)
	}
}

func TestStateDirIsAppSupportOnMacos(t *testing.T) {
	if got := StateDirFrom(envOf(nil), "/h", true); got != "/h/Library/Application Support/contr" {
		t.Fatalf("got %q", got)
	}
}

func TestStateDirFollowsXdgElsewhere(t *testing.T) {
	if got := StateDirFrom(envOf(map[string]string{"XDG_STATE_HOME": "/x"}), "/h", false); got != "/x/contr" {
		t.Fatalf("xdg: %q", got)
	}
	if got := StateDirFrom(envOf(nil), "/h", false); got != "/h/.local/state/contr" {
		t.Fatalf("default: %q", got)
	}
}

func TestEmptyVariablesCountAsUnset(t *testing.T) {
	env := envOf(map[string]string{"CONTR_STATE_DIR": "", "XDG_STATE_HOME": ""})
	if got := StateDirFrom(env, "/h", false); got != "/h/.local/state/contr" {
		t.Fatalf("got %q", got)
	}
}

func TestNoHomeAndNoOverrideIsNone(t *testing.T) {
	for _, macos := range []bool{true, false} {
		if got := StateDirFrom(envOf(nil), "", macos); got != "" {
			t.Fatalf("macos=%v: %q", macos, got)
		}
	}
}
