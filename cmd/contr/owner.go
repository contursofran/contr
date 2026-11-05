package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/owner"
)

const ownerUsage = `Usage:
  contr owner ask [flags] <question…> ask a question; prints its id (D1, …)
  contr owner answer <id> <answer…>   answer a question (D1, …) and tell its coordinator
  contr owner done <id>               mark a to-do (T1, …) done and tell its coordinator

Flags of ask (each of option, impact and ref can repeat):
  --title T         a 2-4 word name
  --from F          the worker, issue or PR it is about
  --option O        a short answer to pick from
  --impact I        what choosing the option means; the nth impact is for the nth option
  --recommended N   index (from 0) of the recommended option
  --ref R           what helps to decide: "PR 12", "#12" or "issue 12", an http(s) URL, a file path
  --source S        a short excerpt of the text that raised the question

answer and done do what the questions card does: the owner file changes first, then the
coordinator that asked gets "Owner answered <id>: <answer>" or
"Owner did <id>: <text>". When the coordinator cannot be reached the change
stays saved and the command exits 1.

Environment:
  CONTR_OWNER_FILE   the owner file (default: the user config folder, contr/owner.json)
  CONTR_HC           the hc program
`

// runOwner is `contr owner`. It returns the exit code: 0 on success, 1 on an
// error, 2 on a usage error.
func runOwner(args []string, stdout, stderr io.Writer, dial func(context.Context, func(string) string) (agentsCore, error), getenv func(string) string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		w, code := stdout, 0
		if len(args) == 0 {
			w, code = stderr, 2
		}
		_, _ = io.WriteString(w, ownerUsage)
		return code
	}
	sub, rest := args[0], args[1:]
	if sub == "ask" {
		return runOwnerAsk(rest, stdout, stderr, getenv)
	}
	switch {
	case sub == "answer" && len(rest) >= 2 && strings.TrimSpace(strings.Join(rest[1:], " ")) != "":
	case sub == "done" && len(rest) == 1:
	case sub == "answer" || sub == "done":
		fmt.Fprintf(stderr, "contr owner %s: wrong number of arguments\n%s", sub, ownerUsage)
		return 2
	default:
		fmt.Fprintf(stderr, "contr owner: unknown command %s\n%s", sub, ownerUsage)
		return 2
	}
	store, err := ownerStore(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "contr owner: %v\n", err)
		return 1
	}
	coordinator, text, err := ownerChange(store, sub, rest[0], strings.Join(rest[1:], " "))
	if err != nil {
		fmt.Fprintf(stderr, "contr owner %s: %v\n", sub, err)
		return 1
	}
	if coordinator == "" {
		return 0
	}
	ctx := context.Background()
	core, err := dial(ctx, getenv)
	if err == nil {
		defer core.Close()
		err = agents.New(core, agents.DefaultConfig()).Send(ctx, coordinator, text)
	}
	if err != nil {
		fmt.Fprintf(stderr, "contr owner %s: saved; coordinator not reached: %v\n", sub, err)
		return 1
	}
	return 0
}

// ownerChange saves the answer or the done mark and returns the coordinator to
// tell and the text to send it. The coordinator is "" when no one asked.
func ownerChange(store *owner.Store, sub, id, answer string) (string, string, error) {
	st, err := store.Read()
	if err != nil {
		return "", "", err
	}
	if sub == "answer" {
		var q owner.Question
		if i := slices.IndexFunc(st.Questions, func(q owner.Question) bool { return q.ID == id }); i >= 0 {
			q = st.Questions[i]
		}
		if err := store.Decide([]string{id}, answer, "owner"); err != nil {
			return "", "", err
		}
		return q.Coordinator, fmt.Sprintf("Owner answered %s: %s\n", id, answer), nil
	}
	var t owner.Todo
	if i := slices.IndexFunc(st.Todos, func(t owner.Todo) bool { return t.ID == id }); i >= 0 {
		t = st.Todos[i]
	}
	if err := store.Unpin([]string{id}); err != nil {
		return "", "", err
	}
	return t.Coordinator, fmt.Sprintf("Owner did %s: %s\n", id, t.Text), nil
}

// listFlag is a flag that can repeat.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// runOwnerAsk is `contr owner ask`: it adds a question to the owner file for
// the project of the session and prints its id.
func runOwnerAsk(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("contr owner ask", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var options, impacts, refs listFlag
	title := fs.String("title", "", "")
	from := fs.String("from", "", "")
	source := fs.String("source", "", "")
	rec := fs.Int("recommended", -1, "")
	fs.Var(&options, "option", "")
	fs.Var(&impacts, "impact", "")
	fs.Var(&refs, "ref", "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	question := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if question == "" {
		fmt.Fprintf(stderr, "contr owner ask: give the question\n%s", ownerUsage)
		return 2
	}
	if *rec >= len(options) || len(impacts) > len(options) {
		fmt.Fprintf(stderr, "contr owner ask: --recommended and --impact need a matching --option\n")
		return 2
	}
	q := owner.Question{Project: projectName(getenv), Coordinator: getenv("HC_TERMINAL_ID"), From: *from, Title: *title,
		Question: question, Options: options, Impacts: impacts, Refs: refs, Source: *source}
	if *rec >= 0 {
		q.Recommended = rec
	}
	store, err := ownerStore(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "contr owner: %v\n", err)
		return 1
	}
	q, err = store.Ask(q)
	if err != nil {
		fmt.Fprintf(stderr, "contr owner ask: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, q.ID)
	return 0
}
