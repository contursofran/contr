package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/theme"
)

func TestOpenTerminalStartsAPlainTerminalAndReturnsItsAddress(t *testing.T) {
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t9", "term-0405", "live", nil)}}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	now := time.Date(2026, 11, 4, 10, 4, 5, 0, time.UTC)
	addr, err := openTerminal(context.Background(), func(k string) string {
		if k == "SHELL" {
			return "/bin/fish"
		}
		return ""
	}, dial, "proj", "/work/proj", now, paletteColors(theme.Light))
	if err != nil || addr != "t9" {
		t.Fatalf("addr %q, err %v", addr, err)
	}
	p := core.params[0]
	l := p["labels"].(map[string]any)
	if l["role"] != "terminal" || l["contr"] != "agent" || l["project"] != "proj" || l["worktree"] != "/work/proj" {
		t.Errorf("labels %v", l)
	}
	if p["cwd"] != "/work/proj" || p["no_parent"] != true || p["outlive_parent"] != true || p["wait"] != false {
		t.Errorf("params %v", p)
	}
	if name, _ := p["name"].(string); len(name) < len("term-04051") || name[:9] != "term-0405" {
		t.Errorf("name %v", p["name"])
	}
	if !core.closed {
		t.Error("the connection must close")
	}
	if d, _ := p["display"].(map[string]any); d["bg"] != paletteColors(theme.Light).BG {
		t.Errorf("display %v, want the Light colors", p["display"])
	}
}

func TestOpenTerminalNamesDifferInTheSameSecond(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 4, 5, 0, time.UTC)
	if a, b := terminalName(now, 1), terminalName(now, 2); a == b || a != "term-04051" {
		t.Errorf("names %q %q", a, b)
	}
}

func TestOpenTerminalReturnsTheDialAndCoreErrors(t *testing.T) {
	boom := errors.New("no core")
	dial := func(context.Context, func(string) string) (agentsCore, error) { return nil, boom }
	if _, err := openTerminal(context.Background(), func(string) string { return "" }, dial, "p", "/d", time.Now(), nil); !errors.Is(err, boom) {
		t.Errorf("dial error %v", err)
	}
	core := &cliCore{errs: map[string]error{"terminal.run": boom}}
	dial = func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	if _, err := openTerminal(context.Background(), func(string) string { return "" }, dial, "p", "/d", time.Now(), nil); !errors.Is(err, boom) {
		t.Errorf("core error %v", err)
	}
	if !core.closed {
		t.Error("the connection must close on error")
	}
}
