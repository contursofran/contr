package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Flow is how the orchestrator takes an issue to a merge (D185). It is saved
// as the flow key of agents.json.
type Flow struct {
	// Plan is when a planner runs first: never, when-needed or always.
	Plan string `json:"plan"`
	// Review is which PRs get a reviewer: none, code or every.
	Review string `json:"review"`
	// DebuggerAfter is the number of failed fix rounds before a debugger.
	DebuggerAfter int `json:"debugger_after"`
	// Workers is the most workers at once, reviewers included.
	Workers int `json:"workers"`
	// Merge is ask (the owner's OK each time) or auto (when CI and review pass).
	Merge string `json:"merge"`
}

// DefaultFlow holds the rules of the kit's orchestrate playbook.
func DefaultFlow() Flow {
	return Flow{Plan: "when-needed", Review: "code", DebuggerAfter: 2, Workers: 4, Merge: "ask"}
}

// Validate gives an error for a value out of range.
func (f Flow) Validate() error {
	check := func(key, v string, ok ...string) error {
		for _, o := range ok {
			if v == o {
				return nil
			}
		}
		return fmt.Errorf("flow %s: %q is not one of %s", key, v, strings.Join(ok, ", "))
	}
	if err := check("plan", f.Plan, "never", "when-needed", "always"); err != nil {
		return err
	}
	if err := check("review", f.Review, "none", "code", "every"); err != nil {
		return err
	}
	if err := check("merge", f.Merge, "ask", "auto"); err != nil {
		return err
	}
	if f.DebuggerAfter < 1 {
		return fmt.Errorf("flow debugger_after: %d is less than 1", f.DebuggerAfter)
	}
	if f.Workers < 1 {
		return fmt.Errorf("flow workers: %d is less than 1", f.Workers)
	}
	return nil
}

// Lines is the flow as plain rules, one per line, for the orchestrator.
func (f Flow) Lines() []string {
	plan := map[string]string{
		"never":       "Plan first: never. Spawn builder directly; the builder plans in its PR body.",
		"when-needed": "Plan first: when needed. Spawn planner first only when the work needs several PRs or the issue leaves the approach open.",
		"always":      "Plan first: always. Spawn planner first for each issue, then builder with the plan.",
	}[f.Plan]
	review := map[string]string{
		"none":  "Review: none. Spawn no reviewer; read the diff yourself.",
		"code":  "Review: code PRs. Every PR that changes code gets a reviewer; for a docs-only or config-only PR, read the diff yourself.",
		"every": "Review: every PR gets a reviewer.",
	}[f.Review]
	merge := map[string]string{
		"ask":  "Merge: ask the owner each time.",
		"auto": "Merge: merge yourself when CI and review pass.",
	}[f.Merge]
	return []string{
		plan,
		review,
		fmt.Sprintf("Debugger: spawn debugger after %d failed fix rounds.", f.DebuggerAfter),
		fmt.Sprintf("Workers at once: at most %d, reviewers included.", f.Workers),
		merge,
	}
}

// Text is the flow block: "The flow:" and one line per rule.
func (f Flow) Text() string {
	return "The flow:\n- " + strings.Join(f.Lines(), "\n- ") + "\n"
}

// Save writes the config to path. Keys in the file that Config does not know
// stay as they are. A profile goes to the file only when the file had it or
// when it differs from DefaultConfig, so the file does not freeze today's
// defaults (D186).
func (c Config) Save(path string) error {
	raw := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("agents: %s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	if err := c.Flow.Validate(); err != nil {
		return fmt.Errorf("agents: %w", err)
	}
	var had map[string]json.RawMessage
	_, hadProfiles := raw["profiles"]
	if hadProfiles {
		if err := json.Unmarshal(raw["profiles"], &had); err != nil {
			return fmt.Errorf("agents: %s: profiles: %w", path, err)
		}
	}
	defaults := DefaultConfig().Profiles
	profiles := map[string]Profile{}
	for name, p := range c.Profiles {
		d, isDefault := defaults[name]
		if _, ok := had[name]; ok || !isDefault || !p.Equal(d) {
			profiles[name] = p
		}
	}
	set := func(key string, v any, empty bool) error {
		if empty {
			delete(raw, key)
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		raw[key] = b
		return nil
	}
	for _, e := range []error{
		set("profiles", profiles, len(profiles) == 0 && !hadProfiles),
		set("kit", c.Kit, c.Kit == ""),
		set("setup", c.Setup, len(c.Setup) == 0),
		set("flow", c.Flow, false),
	} {
		if e != nil {
			return e
		}
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Equal tells whether two profiles start the same agent.
func (p Profile) Equal(o Profile) bool {
	return p.Agent == o.Agent && slices.Equal(p.Argv, o.Argv)
}
