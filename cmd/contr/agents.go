package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
)

const agentsUsage = `Usage:
  contr agents ls [--all] [--json]                 list the workers
  contr agents spawn --name N --profile P [--issue I] [--worktree DIR] [--project P]
                     [--role R] [--review-pr PR] [--no-parent] [--json]
                     [--task-file F | [--] task…]
                                                   start a worker; --task-file reads the
                                                   task from a file; --review-pr makes a
                                                   detached worktree at the PR's head
                                                   (<main>-wt/review-<PR>) of the repository
                                                   at --worktree (default: here) and starts
                                                   the worker there
  contr agents send <name> <text…>                 type text and press Enter
  contr agents interrupt <name>                    press the stop key
  contr agents stop <name> [--keep]                end the worker's program and remove its
                                                   terminal; --keep leaves the finished one
  contr agents rm <name>                           remove a finished worker's terminal
  contr agents rm --ended                          remove all finished workers that this
                                                   terminal started
  contr agents read <name> [--lines N]             print the screen and history
  contr agents prompt <name>                       print the menu a blocked worker waits on, or "no prompt"
  contr agents watch                               print the list as JSON on each change
  contr agents flow                                print the flow: how the orchestrator takes an issue to a merge

Flags can come before or after the other arguments. After --, every word is
part of the task.

Environment:
  CONTR_AGENTS_CONFIG   profiles file (default: the user config folder, contr/agents.json).
                        Built-in profiles: orchestrator (claude opus, medium effort), builder,
                        planner, reviewer, debugger. A profile in the file replaces the
                        built-in one of the same name. A file with "presets" and no
                        "profiles" is the old format: it is ignored and help says so.
                        {"kit": DIR, "profiles": {NAME: {"argv": [...], "agent": KIT_AGENT}},
                         "setup": {MAIN_CHECKOUT: SHELL_COMMAND},
                         "flow": {"plan": "never|when-needed|always", "review": "none|code|every",
                                  "debugger_after": N, "workers": N, "merge": "ask|auto"}}
                        setup runs in a new worktree of that main checkout before the
                        agent starts, with CONTR_MAIN set to the main checkout
  CONTR_HC              the hc program (default: hc from PATH, then ~/.local/bin, /opt/homebrew/bin, /usr/local/bin)
  CONTR_KIT             contr's Claude Code plugin folder (the repository's kit);
                        claude profiles with an agent there start as that agent
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

// flowText is the flow block of the profiles file, or the default flow with
// the error when the file cannot be read.
func flowText(getenv func(string) string) string {
	cfg, err := agents.LoadConfig(configPath(getenv))
	if err != nil {
		return fmt.Sprintf("The flow could not be read (%v); the default flow follows.\n", err) + agents.DefaultFlow().Text()
	}
	return cfg.Flow.Text()
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
		if code == 0 {
			_, _ = io.WriteString(w, "\n"+flowText(getenv))
			if cfg, err := agents.LoadConfig(configPath(getenv)); err == nil && cfg.Notice != "" {
				_, _ = io.WriteString(w, "\nNote: "+cfg.Notice+".\n")
			}
		}
		return code
	}
	if args[0] == "flow" {
		if len(args) > 1 {
			fmt.Fprintf(stderr, "contr agents flow: takes no arguments\n")
			return 2
		}
		cfg, err := agents.LoadConfig(configPath(getenv))
		if err != nil {
			fmt.Fprintf(stderr, "contr agents: %v\n", err)
			return 1
		}
		_, _ = io.WriteString(stdout, cfg.Flow.Text())
		return 0
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
		project  = fs.String("project", "", "project name")
		role     = fs.String("role", "", "role (default: the profile)")
		noParent = fs.Bool("no-parent", false, "start without a parent terminal")
		taskFile = fs.String("task-file", "", "read the task from this file")
		reviewPR = fs.Int("review-pr", 0, "start in a review worktree at this pull request's head")
		keep     = fs.Bool("keep", false, "keep the finished terminal")
		ended    = fs.Bool("ended", false, "remove all finished workers started by this terminal")
		lines    = fs.Int("lines", 0, "only the last lines")
	)
	need := map[string]int{"ls": 0, "watch": 0, "spawn": 0, "send": 2, "interrupt": 1, "stop": 1, "rm": 0, "read": 1, "prompt": 1}
	n, ok := need[sub]
	if !ok {
		fmt.Fprintf(stderr, "contr agents: unknown command %s\n%s", sub, agentsUsage)
		return 2
	}
	pos, err := parseInterleaved(fs, rest)
	if err != nil {
		return 2
	}
	allowed := map[string][]string{
		"ls":    {"all", "json"},
		"spawn": {"name", "profile", "issue", "worktree", "project", "role", "no-parent", "json", "task-file", "review-pr"},
		"read":  {"lines"},
		"stop":  {"keep"},
		"rm":    {"ended"},
	}
	var bad string
	fs.Visit(func(f *flag.Flag) {
		if bad == "" && !slices.Contains(allowed[sub], f.Name) {
			bad = f.Name
		}
	})
	if bad != "" {
		fmt.Fprintf(stderr, "contr agents %s: flag --%s does not apply\n%s", sub, bad, agentsUsage)
		return 2
	}
	if len(pos) < n || (n == 1 && len(pos) > 1) || (n == 0 && sub != "spawn" && sub != "rm" && len(pos) > 0) {
		fmt.Fprintf(stderr, "contr agents %s: wrong number of arguments\n%s", sub, agentsUsage)
		return 2
	}

	if sub == "rm" && *ended == (len(pos) == 1) || sub == "rm" && len(pos) > 1 {
		fmt.Fprintf(stderr, "contr agents rm: give one name, or --ended\n%s", agentsUsage)
		return 2
	}

	// Only spawn needs the profiles, so a broken file does not block the rest.
	cfg := agents.DefaultConfig()
	if sub == "spawn" {
		if cfg, err = agents.LoadConfig(configPath(getenv)); err != nil {
			fmt.Fprintf(stderr, "contr agents: %v\n", err)
			return 1
		}
		if kit := getenv("CONTR_KIT"); kit != "" {
			cfg.Kit = kit
		}
	}
	// Ctrl-C or SIGTERM ends a watch cleanly, so the connection closes.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	core, err := dial(ctx, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "contr agents: %v\n", err)
		return 1
	}
	defer core.Close()
	m := agents.New(core, cfg).WithEnv(getenv)

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
			task, err := spawnTask(strings.Join(pos, " "), *taskFile)
			if err != nil {
				return err
			}
			spec := agents.Spec{Name: *name, Profile: *profile, Role: *role, Issue: *issue, Worktree: *worktree, Project: *project, Task: task, Colors: storedColors()}
			if *reviewPR != 0 {
				if spec.Worktree, err = m.ReviewWorktree(ctx, cmp.Or(*worktree, "."), *reviewPR); err != nil {
					return err
				}
			}
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
			return m.Stop(ctx, pos[0], *keep)
		case "rm":
			if !*ended {
				return m.Remove(ctx, pos[0])
			}
			names, err := m.RemoveEnded(ctx)
			for _, n := range names {
				fmt.Fprintln(stdout, n)
			}
			return err
		case "prompt":
			txt, err := pendingPrompt(ctx, m, pos[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(stdout, txt)
			return err
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

// parseInterleaved parses flags before and after the other arguments, which
// it returns in order. After "--" every word is an argument.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var tail []string
	if i := slices.Index(args, "--"); i >= 0 {
		args, tail = args[:i], args[i+1:]
	}
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return append(pos, tail...), nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
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

// spawnTask is the task of a spawn: the text, or the content of file. It
// refuses both at once, and a file that is missing or holds only blanks, so
// a quoting slip or a wrong path never starts a worker with a wrong brief.
func spawnTask(text, file string) (string, error) {
	if file == "" {
		return text, nil
	}
	if strings.TrimSpace(text) != "" {
		return "", errors.New("give the task as text or with a task file, not both")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("task file: %w", err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", fmt.Errorf("task file %s is empty", file)
	}
	return string(b), nil
}
