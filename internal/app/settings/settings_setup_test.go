package settings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/setup"
	"github.com/contursofran/contr/internal/usage"
)

// setupEnv is a Mac where every program is there and answers well; meter data is missing
// unless the test writes it. runs counts the commands.
func setupEnv(t *testing.T, missing map[string]bool, runs *int) *setup.Env {
	return &setup.Env{StateDir: t.TempDir(),
		Find: func(name string) (string, error) {
			if missing[name] {
				return "", errors.New("not found")
			}
			return name, nil
		},
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			*runs++
			switch name {
			case "hc":
				return []byte("hc 0.1.18\n"), nil
			case "claude":
				return []byte(`[{"id":"contr@contr","version":"0.1.0","enabled":true}]`), nil
			}
			return nil, nil
		}}
}

func (r *settingsRig) openSetup(env *setup.Env, core bool) {
	r.s.Setup = env
	r.s.CoreVersion = func() (string, bool) { return "0.1.18", core }
	r.open()
}

func TestSetupTabIsFirstAndShowsStepsLeft(t *testing.T) {
	r := newSettingsRig(t, nil)
	runs := 0
	r.openSetup(setupEnv(t, nil, &runs), true)
	if SettingsTabs[0] != "Setup" || runs == 0 {
		t.Fatalf("tabs %v, runs %d", SettingsTabs, runs)
	}
	// Only the meter has no data: one step, its line to copy, and the dot.
	for _, want := range []string{"1 step left", "Required", "Optional", "hc 0.1.18", "running · hc 0.1.18",
		"contr 0.1.0", "signed in", "no data from the status line", setup.CopyMeter} {
		if !r.hasText(want) {
			t.Fatalf("no %q in %q", want, r.texts())
		}
	}
	if !r.has(LabelSetupDot) || !r.s.NeedsDot() {
		t.Fatal("no setup dot while a required check is not ready")
	}
}

func TestSetupTabGoodToGoWhenOnlyOptionalAreMissing(t *testing.T) {
	r := newSettingsRig(t, nil)
	runs := 0
	env := setupEnv(t, map[string]bool{"codex": true}, &runs)
	r.s.Setup = env
	writeMeter(t, env.StateDir)
	r.openSetup(env, true)
	if !r.hasText(TextSetupGood) || r.has(LabelSetupDot) || r.s.NeedsDot() {
		t.Fatalf("want Good to go and no dot; texts %q", r.texts())
	}
	if !r.hasText(setup.CopyCodex) {
		t.Fatal("no codex line to copy")
	}
}

func TestSetupTabCountsTheOfflineCore(t *testing.T) {
	r := newSettingsRig(t, nil)
	runs := 0
	env := setupEnv(t, nil, &runs)
	writeMeter(t, env.StateDir)
	r.openSetup(env, false)
	if !r.hasText("1 step left") || !r.hasText("not connected") {
		t.Fatalf("texts %q", r.texts())
	}
}

func TestSetupRecheckRunsTheChecksAgain(t *testing.T) {
	r := newSettingsRig(t, nil)
	runs := 0
	r.openSetup(setupEnv(t, nil, &runs), true)
	before := runs
	r.click(SetupRecheck)
	if runs <= before {
		t.Fatalf("Recheck ran nothing: %d then %d", before, runs)
	}
	r.s.Activated()
	if runs <= before*2 {
		t.Fatalf("Activated on the Setup tab ran nothing: %d", runs)
	}
}

func TestSetupNothingRunsWhileClosed(t *testing.T) {
	r := newSettingsRig(t, nil)
	runs := 0
	r.s.Setup = setupEnv(t, nil, &runs)
	r.s.Activated()
	if runs != 0 || r.s.NeedsDot() {
		t.Fatalf("closed Settings ran %d commands", runs)
	}
}

func TestSetupCopyPutsTheLineOnTheClipboard(t *testing.T) {
	r := newSettingsRig(t, nil)
	var got string
	r.s.su.write = func(s string) { got = s }
	runs := 0
	r.openSetup(setupEnv(t, nil, &runs), true)
	r.click(SetupCopy)
	if got != setup.CopyMeter || !r.has(SetupCopied) {
		t.Fatalf("copied %q; texts %q", got, r.texts())
	}
}

// writeMeter writes meter data from now into dir.
func writeMeter(t *testing.T, dir string) {
	t.Helper()
	snap := &usage.ClaudeSnapshot{Schema: 1, UpdatedAt: uint64(time.Now().Unix()), Windows: []usage.Window{{Kind: "five_hour"}}}
	if _, err := usage.WriteSnapshot(dir, snap); err != nil {
		t.Fatal(err)
	}
}

func TestSetupCopiedGoesBackToCopy(t *testing.T) {
	r := newSettingsRig(t, nil)
	r.s.su.write = func(string) {}
	runs := 0
	r.openSetup(setupEnv(t, nil, &runs), true)
	r.click(SetupCopy)
	r.tt.Advance(copiedFor + time.Millisecond)
	r.tt.Frame()
	if r.has(SetupCopied) || !r.has(SetupCopy) {
		t.Fatalf("still Copied; texts %q", r.texts())
	}
}
