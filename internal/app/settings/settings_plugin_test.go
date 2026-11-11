package settings

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/setup"
)

// pluginEnv is setupEnv with the contr plugin in the list given as JSON.
func pluginEnv(t *testing.T, list string, runs *int) *setup.Env {
	env := setupEnv(t, nil, runs)
	run := env.Run
	env.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "claude" {
			*runs++
			return []byte(list), nil
		}
		return run(ctx, name, args...)
	}
	return env
}

const (
	listMissing  = `[]`
	listDisabled = `[{"id":"contr@contr","version":"0.1.0","enabled":false}]`
)

// pluginRig opens the Setup tab with the plugin in list and PluginRun set to run.
func pluginRig(t *testing.T, list string, runs *int, run func(context.Context, setup.Env, string) error) *settingsRig {
	t.Helper()
	r := newSettingsRig(t, nil)
	r.s.PluginRun = run
	r.openSetup(pluginEnv(t, list, runs), true)
	return r
}

// clickPlugin clicks the action, checks the confirm shows line, and clicks button.
func clickPlugin(t *testing.T, r *settingsRig, action, line, button string) {
	t.Helper()
	if err := r.tt.Click(action); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !r.tt.HasText(TextPluginConfirmBody + "\n" + line) {
		t.Fatalf("no confirm with %q: texts %q", line, r.texts())
	}
	if button == "" {
		return
	}
	if err := r.tt.Click(button); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
}

func TestPluginEnableRunsTheLineAndRechecks(t *testing.T) {
	checks, ran := 0, []string{}
	r := pluginRig(t, listDisabled, &checks, func(_ context.Context, _ setup.Env, line string) error {
		ran = append(ran, line)
		return nil
	})
	before := checks
	clickPlugin(t, r, SetupEnable, setup.CopyEnable, LabelPluginRun)
	if !slices.Equal(ran, []string{setup.CopyEnable}) || checks <= before {
		t.Fatalf("ran %q, checks %d then %d", ran, before, checks)
	}
}

func TestPluginInstallShowsTheLastErrorLine(t *testing.T) {
	checks, ran := 0, []string{}
	r := pluginRig(t, listMissing, &checks, func(_ context.Context, _ setup.Env, line string) error {
		ran = append(ran, line)
		return errors.New("first\nmarketplace contr not found")
	})
	before := checks
	clickPlugin(t, r, SetupInstall, setup.CopyPlugin, LabelPluginRun)
	if !slices.Equal(ran, []string{setup.CopyPlugin}) || checks != before ||
		!r.hasText("marketplace contr not found") || !r.has(SetupCopy) {
		t.Fatalf("ran %q, checks %d then %d, texts %q", ran, before, checks, r.texts())
	}
}

func TestPluginTimeout(t *testing.T) {
	old := PluginTimeout
	PluginTimeout = 20 * time.Millisecond
	t.Cleanup(func() { PluginTimeout = old })
	checks := 0
	r := pluginRig(t, listMissing, &checks, func(ctx context.Context, _ setup.Env, _ string) error {
		<-ctx.Done() // the fake hangs until the time limit
		return ctx.Err()
	})
	clickPlugin(t, r, SetupInstall, setup.CopyPlugin, LabelPluginRun)
	if !r.hasText("no answer after " + PluginTimeout.String()) {
		t.Fatalf("texts %q", r.texts())
	}
}

func TestPluginCancelAndEscapeRunNothing(t *testing.T) {
	checks, ran := 0, 0
	r := pluginRig(t, listDisabled, &checks, func(context.Context, setup.Env, string) error { ran++; return nil })
	clickPlugin(t, r, SetupEnable, setup.CopyEnable, LabelPluginCancel)
	clickPlugin(t, r, SetupEnable, setup.CopyEnable, "")
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if ran != 0 || r.s.pl.confirm != "" || r.s.pl.busy {
		t.Fatalf("ran %d, state %+v", ran, r.s.pl)
	}
}

func TestPluginSpinnerWhileRunning(t *testing.T) {
	checks := 0
	r := pluginRig(t, listDisabled, &checks, func(context.Context, setup.Env, string) error { return nil })
	var held func()
	r.s.Env.Run.Go = func(fn func()) { held = fn }
	clickPlugin(t, r, SetupEnable, setup.CopyEnable, LabelPluginRun)
	if !r.has(LabelPluginBusy) || r.has(SetupEnable) {
		t.Fatalf("no spinner: texts %q", r.texts())
	}
	r.s.Env.Run.Go = nil
	held()
	r.tt.Frame()
	if r.has(LabelPluginBusy) {
		t.Fatal("spinner stays after the run")
	}
}

func TestPluginNoButtonOnReadyAndCopyOnlyRows(t *testing.T) {
	checks := 0
	r := newSettingsRig(t, nil)
	r.openSetup(setupEnv(t, map[string]bool{"gh": true, "codex": true}, &checks), true)
	for _, l := range []string{SetupInstall, SetupEnable} {
		if r.has(l) {
			t.Fatalf("%s shows with the plugin ready: texts %q", l, r.texts())
		}
	}
	if !r.has(SetupCopy) {
		t.Fatal("copy-only rows lost Copy")
	}
	for _, res := range []setup.Result{
		{ID: setup.IDMeter, Copy: setup.CopyMeter}, {ID: setup.IDGH, Copy: setup.CopyGHLogin},
		{ID: setup.IDHC, Copy: setup.CopyHC}, {ID: setup.IDPlugin},
	} {
		if a := pluginAction(res); a != "" {
			t.Fatalf("%s gets %q", res.ID, a)
		}
	}
}
