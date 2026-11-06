package app

// The Agents tab of Settings (D178): the Roles section edits the profiles of agents.json. Only
// Claude Code is in the dropdowns for now. A profile that the dropdowns cannot show is "Custom":
// it shows its command line and stays as it is until the owner picks a value. Reads and writes
// of agents.json run off the UI thread (Runner).

import (
	"log"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/theme"
)

// Texts of the Agents tab.
const (
	TextRolesHint    = "Saved to agents.json · new workers use it"
	TextAgentsFoot   = "Applies to agents started from now on"
	AgentsReset      = "Reset to defaults"
	TextCustom       = "Custom"
	TextNotSet       = "not set"
	TextClaudeCode   = "Claude Code"
	TextAgentsNoPath = "no agents.json path"
)

// Role is one row of the Roles section: a profile name, its title and what it does.
type Role struct{ Profile, Name, Does string }

// Roles are the rows of the Roles section, in order.
var Roles = []Role{
	{"orchestrator", "Orchestrator", "plans and lands work"},
	{"planner", "Planner", "writes PLAN.md"},
	{"builder", "Builder", "builds, opens the PR"},
	{"reviewer", "Reviewer", "reviews the PR head"},
	{"debugger", "Debugger", "after failed fixes"},
}

// RoleModels are the Claude Code models: the argv value and its name.
var RoleModels = [][2]string{{"opus", "Opus 5.5"}, {"sonnet", "Sonnet 5.5"}, {"haiku", "Haiku 5.5"}}

// RoleEfforts are the effort values of Claude Code.
var RoleEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// claudeProgram is the program of the Claude Code CLI.
const claudeProgram = "claude"

// roleDefault is the profile that Reset to defaults and a first pick start from. The
// orchestrator is not in DefaultConfig; its suggestion is claude opus medium.
func roleDefault(profile string) agents.Profile {
	if p, ok := agents.DefaultConfig().Profiles[profile]; ok {
		return agents.Profile{Argv: slices.Clone(p.Argv), Agent: p.Agent}
	}
	return agents.Profile{Argv: []string{claudeProgram, "--model", "opus", "--effort", "medium", "--permission-mode", "auto"}}
}

// flagValue is the value of --name (or --name=) in argv, and whether it is there.
func flagValue(argv []string, name string) (string, bool) {
	for i, a := range argv {
		if a == name && i+1 < len(argv) {
			return argv[i+1], true
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, true
		}
	}
	return "", false
}

// setFlag sets --name to v in argv and keeps the other arguments. A missing flag goes after
// the program, or after --model for --effort.
func setFlag(argv []string, name, v string) []string {
	out := slices.Clone(argv)
	for i, a := range out {
		if a == name && i+1 < len(out) {
			out[i+1] = v
			return out
		}
		if strings.HasPrefix(a, name+"=") {
			out[i] = name + "=" + v
			return out
		}
	}
	at := 1
	if name == "--effort" {
		for i, a := range out {
			if a == "--model" && i+1 < len(out) {
				at = i + 2
			} else if strings.HasPrefix(a, "--model=") {
				at = i + 1
			}
		}
	}
	return slices.Insert(out, min(at, len(out)), name, v)
}

// roleValues reads the CLI, model and effort that the dropdowns show. A value that the
// dropdowns cannot show is "".
func roleValues(argv []string) (cli, model, effort string) {
	if len(argv) == 0 || argv[0] != claudeProgram {
		return "", "", ""
	}
	m, _ := flagValue(argv, "--model")
	e, _ := flagValue(argv, "--effort")
	if !slices.ContainsFunc(RoleModels, func(x [2]string) bool { return x[0] == m }) {
		m = ""
	}
	if !slices.Contains(RoleEfforts, e) {
		e = ""
	}
	return claudeProgram, m, e
}

// commandLine is argv as a shell line; arguments with spaces are quoted.
func commandLine(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\"'") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		parts[i] = a
	}
	return strings.Join(parts, " ")
}

// pickRole sets one value of a role's profile: what is "cli", "model" or "effort". A profile
// of another program starts again from the role's default.
func pickRole(cfg *agents.Config, profile, what, v string) {
	p, ok := cfg.Profiles[profile]
	if !ok || len(p.Argv) == 0 || p.Argv[0] != claudeProgram {
		d := roleDefault(profile)
		p.Argv = d.Argv
	}
	switch what {
	case "model":
		p.Argv = setFlag(p.Argv, "--model", v)
	case "effort":
		p.Argv = setFlag(p.Argv, "--effort", v)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]agents.Profile{}
	}
	cfg.Profiles[profile] = p
}

// resetRoles puts each role back to its default. A missing orchestrator stays missing.
func resetRoles(cfg *agents.Config) {
	for _, r := range Roles {
		if _, ok := cfg.Profiles[r.Profile]; !ok && r.Profile == agents.RoleOrchestrator {
			continue
		}
		cfg.Profiles[r.Profile] = roleDefault(r.Profile)
	}
}

// agentsState is what the Agents tab holds between builds.
type agentsState struct {
	loaded, loading, saving bool
	cfg                     agents.Config
	err                     string
}

// loadAgents reads agents.json off the UI thread.
func (s *Settings) loadAgents() {
	if s.ag.loading {
		return
	}
	if s.AgentsConfig == "" {
		s.ag.err = TextAgentsNoPath
		return
	}
	s.ag.loading = true
	path := s.AgentsConfig
	s.Run.Do(func() func() {
		cfg, err := agents.LoadConfig(path)
		return func() {
			s.ag.loading = false
			if err != nil {
				log.Printf("contr: read agents config: %v", err)
				s.ag.err = "read failed: " + err.Error()
				s.redraw()
				return
			}
			s.ag.cfg, s.ag.loaded, s.ag.err = cfg, true, ""
			s.redraw()
		}
	})
}

// saveAgents writes a copy of the config off the UI thread. An error shows in the tab.
func (s *Settings) saveAgents() {
	cfg := s.ag.cfg
	cfg.Profiles = make(map[string]agents.Profile, len(s.ag.cfg.Profiles))
	for k, p := range s.ag.cfg.Profiles {
		cfg.Profiles[k] = agents.Profile{Argv: slices.Clone(p.Argv), Agent: p.Agent}
	}
	path := s.AgentsConfig
	s.ag.saving, s.ag.err = true, ""
	s.Run.Do(func() func() {
		err := cfg.Save(path)
		return func() {
			s.ag.saving = false
			if err != nil {
				log.Printf("contr: save agents config: %v", err)
				s.ag.err = "save failed: " + err.Error()
			}
			s.redraw()
		}
	})
}

// agentsTab builds the Agents tab.
func (s *Settings) agentsTab(c *ui.Context) {
	p := theme.Current()
	if !s.ag.loaded && s.ag.err == "" {
		s.loadAgents()
	}
	ui.Column(c).Shrink(0).Gap(12).PaddingX(theme.ModalPadX).Children(func() {
		// Flow section (slice 3 of #452): the Flow section goes here, above Roles.
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "Roles").Bold().TextColor(p.Ink95)
			ui.Text(c, TextRolesHint).Grow(1).TextColor(p.Ink50)
		})
		if s.ag.err != "" {
			ui.Text(c, s.ag.err).TextColor(p.Blocked)
		}
		if !s.ag.loaded {
			return
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, "Role").Width(180).TextColor(p.Ink50)
			ui.Text(c, "CLI").Width(110).TextColor(p.Ink50)
			ui.Text(c, "Model").Width(110).TextColor(p.Ink50)
			ui.Text(c, "Effort").Width(90).TextColor(p.Ink50)
		})
		for _, r := range Roles {
			s.roleRow(c, r)
		}
	})
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(14).Padding(12, theme.ModalPadX).Margin(12, 0, 0, 0).
		BorderWidth(1, 0, 0, 0).BorderColor(p.Hairline).Children(func() {
		ui.Text(c, TextAgentsFoot).Grow(1).TextColor(p.Ink50)
		var reset ClickFn
		if s.ag.loaded {
			reset = func() {
				resetRoles(&s.ag.cfg)
				s.saveAgents()
			}
		}
		settingsButton(c, AgentsReset, ButtonOutlined, reset)
		settingsButton(c, SettingsDone, ButtonPlain, s.Close)
	})
}

// roleRow is one role: its name and what it does, the three dropdowns, and the command line.
func (s *Settings) roleRow(c *ui.Context, r Role) {
	p := theme.Current()
	prof, have := s.ag.cfg.Profiles[r.Profile]
	cli, model, effort := roleValues(prof.Argv)
	empty := TextCustom
	if !have {
		empty = TextNotSet
	}
	ui.Column(c).Label(r.Name+" role").Gap(4).Padding(8, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.Hairline).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Column(c).Width(180).Children(func() {
				ui.Text(c, r.Name).TextColor(p.Ink95)
				ui.Text(c, r.Does).TextColor(p.Ink50)
			})
			if v := roleSelect(c, r.Name+" CLI", 110, cli, empty, [][2]string{{claudeProgram, TextClaudeCode}}); v != "" {
				pickRole(&s.ag.cfg, r.Profile, "cli", v)
				s.saveAgents()
			}
			if v := roleSelect(c, r.Name+" model", 110, model, empty, RoleModels); v != "" {
				pickRole(&s.ag.cfg, r.Profile, "model", v)
				s.saveAgents()
			}
			efforts := make([][2]string, len(RoleEfforts))
			for i, e := range RoleEfforts {
				efforts[i] = [2]string{e, e}
			}
			if v := roleSelect(c, r.Name+" effort", 90, effort, empty, efforts); v != "" {
				pickRole(&s.ag.cfg, r.Profile, "effort", v)
				s.saveAgents()
			}
		})
		line := commandLine(prof.Argv)
		if !have {
			line = TextNotSet + " · suggested: " + commandLine(roleDefault(r.Profile).Argv)
		}
		ui.Text(c, line).SingleLine().Ellipsis("…").MinWidth(0).TextColor(p.Ink62).
			Font(theme.StyleValue.Family()).FontSize(theme.StyleValue.Size)
	})
}

// roleSelect is one dropdown on SelectBase. cur is the value shown, "" for none (then empty
// shows). It returns the value the owner picked in this build, or "".
func roleSelect(c *ui.Context, label string, width float32, cur, empty string, opts [][2]string) string {
	p := theme.Current()
	picked := cur
	sel := ui.SelectBase(c.Key("role-"+label), &picked)
	shown, color := empty, p.Ink50
	for _, o := range opts {
		if o[0] == cur {
			shown, color = o[1], p.Ink95
		}
	}
	sel.Trigger.Label(label).Width(width).Padding(4, 8).Radius(theme.CardRadius).Border(1, p.InputLine).
		Cursor(ui.CursorPointer).Children(func() {
		ui.Text(c, shown).SingleLine().Ellipsis("…").MinWidth(0).TextColor(color)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).Radius(theme.CardRadius).Background(p.Ground).Border(1, p.FloatLine)
		for _, o := range opts {
			item := sel.Item(o[0]).Label(label + " " + o[1]).Padding(4, 8).Radius(theme.CardRadius)
			text := p.Ink62
			if item.Highlighted() || o[0] == cur {
				item.Background(p.Selected)
				text = p.Ink100
			}
			item.Children(func() { ui.Text(c, o[1]).TextColor(text) })
		}
	})
	if picked != cur {
		return picked
	}
	return ""
}
