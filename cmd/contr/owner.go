package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/owner"
)

const ownerUsage = `Usage:
  contr owner answer <id> <answer…>   answer a question (D1, …) and tell its coordinator
  contr owner done <id>               mark a to-do (T1, …) done and tell its coordinator

Both do what the answer card does: the owner file changes first, then the
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
