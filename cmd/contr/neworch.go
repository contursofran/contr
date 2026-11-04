package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
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
// orchestrator --no-parent`. A missing profile gives an error that names the
// profiles file.
func spawnOrchestrator(ctx context.Context, getenv func(string) string, dial func(context.Context, func(string) string) (agentsCore, error), dir string, now time.Time) error {
	path := configPath(getenv)
	cfg, err := agents.LoadConfig(path)
	if err != nil {
		return err
	}
	if _, ok := cfg.Profiles[orchestratorProfile]; !ok {
		return fmt.Errorf("no %q profile in %s", orchestratorProfile, path)
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
		Worktree: dir, Project: filepath.Base(dir),
	})
	return err
}
