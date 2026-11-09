package main

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/git"
)

// orchestratorProfile is the profile that `+ new` starts (D57).
const orchestratorProfile = "orchestrator"

// pickFolder asks the owner for a project folder with the system dialog. It
// blocks, so the frame calls it off the UI thread.
func pickFolder() (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:             "Start an orchestrator in…",
		ButtonLabel:       "Start",
		Directory:         true,
		CreateDirectories: true,
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", app.ErrPickCancelled
	}
	return paths[0], nil
}

// openFolder asks the owner for a folder to add to the projects (D116). It
// blocks, so the modal calls it off the UI thread. The folder goes to the
// recent documents of the Dock menu.
func openFolder() (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:       "Open a folder as a project",
		ButtonLabel: "Open",
		Directory:   true,
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", app.ErrPickCancelled
	}
	mygo.App.AddRecentDocument(paths[0])
	return paths[0], nil
}

// projectKey returns the git common dir of a folder, or the folder when it is
// outside git (D116). It reads files.
func projectKey(path string) string {
	if c, ok := git.Discover(path); ok {
		return c.CommonDir
	}
	return path
}

var notNameChars = regexp.MustCompile(`[^a-z0-9_-]+`)

// orchestratorName makes a terminal name from a folder: `orch-<folder>` in
// the characters that hc allows, at most 32 of them, with a time suffix so a
// second orchestrator in the same folder gets its own name.
func orchestratorName(dir string, now time.Time) string {
	base := notNameChars.ReplaceAllString(strings.ToLower(filepath.Base(dir)), "-")
	base = strings.Trim(base, "-")
	suffix := now.Format("-0405")
	name := "orch-" + base
	if max := 32 - len(suffix); len(name) > max {
		name = strings.TrimRight(name[:max], "-")
	}
	return name + suffix
}

// spawnOrchestrator starts the orchestrator profile in dir, with role
// orchestrator and no parent: the same as `contr agents spawn --role
// orchestrator --no-parent`. DefaultConfig has the profile, so it needs no file
// (D206).
func spawnOrchestrator(ctx context.Context, getenv func(string) string, dial func(context.Context, func(string) string) (agentsCore, error), dir string, now time.Time, colors *agents.Colors) error {
	cfg, err := agents.LoadConfig(configPath(getenv))
	if err != nil {
		return err
	}
	if kit := getenv("CONTR_KIT"); kit != "" {
		cfg.Kit = kit
	}
	if dir == "" {
		return errors.New("no folder")
	}
	core, err := dial(ctx, getenv)
	if err != nil {
		return err
	}
	defer core.Close()
	_, err = agents.New(core, cfg).Spawn(ctx, agents.Spec{
		Name: orchestratorName(dir, now), Profile: orchestratorProfile, Role: agents.RoleOrchestrator,
		Worktree: dir, Project: filepath.Base(dir), Colors: colors,
	})
	return err
}
