package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
)

const agentsUsage = `Usage:
  contr agents ls [--all] [--json]                 list the workers
  contr agents spawn --name N --profile P [--issue I] [--worktree DIR]
                     [--role R] [--no-parent] [--json] [task…]
                                                   start a worker
  contr agents send <name> <text…>                 type text and press Enter
  contr agents interrupt <name>                    press the stop key
  contr agents stop <name>                         end the worker's program
  contr agents read <name> [--lines N]             print the screen and history
  contr agents watch                               print the list as JSON on each change

Environment:
  CONTR_AGENTS_CONFIG   profiles file (default: the user config folder, contr/agents.json)
  CONTR_HC              the hc program (default: hc)
  HC_TERMINAL_ID        set inside hc: spawn makes that terminal the parent
`

// agentsCore is a connection to the core for one command.
type agentsCore interface {
	agents.Core
	Close() error
}

// dialAgents connects to the owner's hc core through `hc connect`.
func dialAgents(ctx context.Context, getenv func(string) string) (agentsCore, error) {
	cl, err := hc.Dial(ctx, hc.Command(getenv("CONTR_HC"), nil))
	if err != nil {
		return nil, err
	}
	return agents.HC{Client: cl}, nil
}

// configPath is the profiles file.
func configPath(getenv func(string) string) string {
	if p := getenv("CONTR_AGENTS_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "contr", "agents.json")
}

// runAgents is `contr agents`. It returns the exit code: 0 on success, 1 on an
// error, 2 on a usage error.
func runAgents(args []string, stdout, stderr io.Writer, dial func(context.Context, func(string) string) (agentsCore, error), getenv func(string) string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		w := stdout
		code := 0
		if len(args) == 0 {
			w, code = stderr, 2
		}
		_, _ = io.WriteString(w, agentsUsage)
		return code
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("contr agents "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		all      = fs.Bool("all", false, "include finished workers")
		asJSON   = fs.Bool("json", false, "print JSON")
		name     = fs.String("name", "", "worker name")
		profile  = fs.String("profile", "", "profile name")
		issue    = fs.String("issue", "", "issue number")
		worktree = fs.String("worktree", "", "working folder")
		role     = fs.String("role", "", "role (default: the profile)")
		noParent = fs.Bool("no-parent", false, "start without a parent terminal")
		lines    = fs.Int("lines", 0, "only the last lines")
	)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	pos := fs.Args()
	need := map[string]int{"ls": 0, "watch": 0, "spawn": 0, "send": 2, "interrupt": 1, "stop": 1, "read": 1}
	n, ok := need[sub]
	if !ok {
		fmt.Fprintf(stderr, "contr agents: unknown command %s\n%s", sub, agentsUsage)
		return 2
	}
	if len(pos) < n || (n == 1 && len(pos) > 1) || (n == 0 && sub != "spawn" && len(pos) > 0) {
		fmt.Fprintf(stderr, "contr agents %s: wrong number of arguments\n%s", sub, agentsUsage)
		return 2
	}

	cfg, err := agents.LoadConfig(configPath(getenv))
	if err != nil {
		fmt.Fprintf(stderr, "contr agents: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	core, err := dial(ctx, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "contr agents: %v\n", err)
		return 1
	}
	defer core.Close()
	m := agents.New(core, cfg)

	err = func() error {
		switch sub {
		case "ls":
			ws, err := m.List(ctx, *all)
			if err != nil {
				return err
			}
			if *asJSON {
				return writeJSON(stdout, ws)
			}
			return writeTable(stdout, ws)
		case "spawn":
			spec := agents.Spec{Name: *name, Profile: *profile, Role: *role, Issue: *issue, Worktree: *worktree, Task: strings.Join(pos, " ")}
			if !*noParent {
				spec.Parent = getenv("HC_TERMINAL_ID")
			}
			w, err := m.Spawn(ctx, spec)
			if err != nil {
				return err
			}
			if *asJSON {
				return writeJSON(stdout, w)
			}
			_, err = fmt.Fprintf(stdout, "%s %s\n", w.Name, w.Address)
			return err
		case "send":
			return m.Send(ctx, pos[0], strings.Join(pos[1:], " "))
		case "interrupt":
			out, err := m.Interrupt(ctx, pos[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(stdout, out)
			return err
		case "stop":
			return m.Stop(ctx, pos[0])
		case "read":
			txt, err := m.Read(ctx, pos[0], *lines)
			if err != nil {
				return err
			}
			_, err = io.WriteString(stdout, strings.TrimRight(txt, "\n")+"\n")
			return err
		default: // watch
			var werr error
			err := m.Watch(ctx, func(ws []agents.Worker) {
				if werr == nil {
					werr = writeJSONLine(stdout, ws)
					if werr != nil {
						cancel()
					}
				}
			})
			if werr != nil {
				return werr
			}
			return err
		}
	}()
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(stderr, "contr agents %s: %v\n", sub, err)
		return 1
	}
	return 0
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeJSONLine(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// stateText is the state column: the program's status, why it is blocked,
// or how it ended.
func stateText(w agents.Worker) string {
	switch {
	case !w.Live && w.ExitCode != nil:
		return fmt.Sprintf("ended (%d)", *w.ExitCode)
	case !w.Live:
		return "ended"
	case w.Status == "blocked" && w.Kind != "":
		return "blocked: " + w.Kind
	case w.Status != "":
		return w.Status
	default:
		return "-"
	}
}

func writeTable(w io.Writer, ws []agents.Worker) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tADDRESS\tROLE\tISSUE\tSTATE\tWORKTREE")
	for _, a := range ws {
		issue := a.Issue
		if issue == "" {
			issue = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.Name, a.Address, a.Role, issue, stateText(a), a.Worktree)
	}
	return tw.Flush()
}
