package main

import (
	"context"
	"testing"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/theme"
)

// No test reads the owner's settings or the macOS appearance: a spawn from
// the command line gets no colors unless the test sets them.
func init() { storedColors = func() *agents.Colors { return nil } }

func TestPaletteColorsAreHex(t *testing.T) {
	l, d := paletteColors(theme.Light), paletteColors(theme.Dark)
	if l.BG == d.BG || l.FG == d.FG {
		t.Errorf("light %+v and dark %+v are equal", l, d)
	}
	for _, c := range []string{l.FG, l.BG, l.Cursor, d.FG, d.BG, d.Cursor} {
		if len(c) != 7 || c[0] != '#' {
			t.Errorf("color %q is not #rrggbb", c)
		}
	}
}

func TestSourceColorsFollowTheStoredAppearance(t *testing.T) {
	sysDark := func() bool { t.Error("a forced appearance asked the system"); return true }
	if got := sourceColors(mygo.ThemeLight, sysDark); got.BG != paletteColors(theme.Light).BG {
		t.Errorf("Light gave %+v", got)
	}
	if got := sourceColors(mygo.ThemeDark, sysDark); got.BG != paletteColors(theme.Dark).BG {
		t.Errorf("Dark gave %+v", got)
	}
	for dark, want := range map[bool]*theme.Palette{true: theme.Dark, false: theme.Light} {
		if got := sourceColors(mygo.ThemeSystem, func() bool { return dark }); got.BG != paletteColors(want).BG {
			t.Errorf("System with dark=%v gave %+v", dark, got)
		}
		if got := sourceColors("", func() bool { return dark }); got.BG != paletteColors(want).BG {
			t.Errorf("empty source with dark=%v gave %+v", dark, got)
		}
	}
}

func TestAgentsSpawnSendsTheStoredColors(t *testing.T) {
	want := paletteColors(theme.Light)
	storedColors = func() *agents.Colors { return want }
	t.Cleanup(func() { storedColors = func() *agents.Colors { return nil } })
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "b", "live", nil)}}
	if code, _, errs := run(t, core, nil, "spawn", "--name", "b", "--profile", "builder", "--worktree", t.TempDir(), "go"); code != 0 {
		t.Fatalf("code %d %q", code, errs)
	}
	d, _ := core.params[0]["display"].(map[string]any)
	if d["bg"] != want.BG || d["fg"] != want.FG {
		t.Errorf("display %v, want %+v", d, want)
	}
}

func TestMCPSpawnSendsTheStoredColors(t *testing.T) {
	want := paletteColors(theme.Light)
	storedColors = func() *agents.Colors { return want }
	t.Cleanup(func() { storedColors = func() *agents.Colors { return nil } })
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "b", "live", nil)}}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	mcpRun(t, dial, nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		callLine(2, "spawn", map[string]any{"name": "b", "profile": "builder", "task": "go", "worktree": t.TempDir()}),
	)
	if len(core.params) == 0 {
		t.Fatal("no terminal.run call")
	}
	if d, _ := core.params[0]["display"].(map[string]any); d["bg"] != want.BG {
		t.Errorf("display %v, want %+v", d, want)
	}
}
