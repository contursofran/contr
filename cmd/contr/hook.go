package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/contursofran/contr/internal/agents"
)

const hookUsage = `Usage:
  contr hook stop   the kit's Stop hook for worker sessions

Claude Code runs it when a turn ends and gives it the hook input on stdin.
In a worker (CONTR_COORDINATOR is set), a turn that sent no report with
hc send gets one: "ended its turn without a report. Last words: …",
sent to the coordinator. Otherwise it does nothing. It never stops the turn.
`

// saidMax is the most characters of the worker's last words in a report.
const saidMax = 300

// stopInput is the part of Claude Code's Stop hook input that the hook reads.
type stopInput struct {
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	// LastAssistantMessage is set by newer Claude Code versions.
	LastAssistantMessage string `json:"last_assistant_message"`
}

// runHook is `contr hook`. It returns the exit code: 0 when it is done or has
// nothing to do, 1 on an error, usage errors included. It never returns 2:
// for a Stop hook, 2 blocks the turn and sends stderr to the model. With 1,
// Claude Code shows the error and the turn ends.
func runHook(args []string, stdin io.Reader, stderr io.Writer, dial func(context.Context, func(string) string) (agentsCore, error), getenv func(string) string) int {
	if len(args) != 1 || args[0] != "stop" {
		_, _ = io.WriteString(stderr, hookUsage)
		return 1
	}
	coordinator := getenv(agents.EnvCoordinator)
	if coordinator == "" {
		return 0
	}
	var in stopInput
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		fmt.Fprintf(stderr, "contr hook: bad hook input: %v\n", err)
		return 1
	}
	turn, err := readTurn(in.TranscriptPath)
	if err != nil {
		fmt.Fprintf(stderr, "contr hook: %v\n", err)
		return 1
	}
	if turn.Reported {
		return 0
	}
	said := in.LastAssistantMessage
	if strings.TrimSpace(said) == "" {
		said = turn.Said
	}
	text := missedReport(said, in.Cwd)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	core, err := dial(ctx, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "contr hook: %v\n", err)
		return 1
	}
	defer func() { _ = core.Close() }()
	m := agents.New(core, agents.Config{}).WithEnv(getenv)
	if err := m.Send(ctx, coordinator, text); err != nil {
		fmt.Fprintf(stderr, "contr hook: report to %s: %v\n", coordinator, err)
		return 1
	}
	return 0
}

// readTurn reads the last turn of the transcript at path.
func readTurn(path string) (agents.Turn, error) {
	if path == "" {
		return agents.Turn{}, fmt.Errorf("the hook input has no transcript_path")
	}
	f, err := os.Open(path)
	if err != nil {
		return agents.Turn{}, err
	}
	defer func() { _ = f.Close() }()
	return agents.LastTurn(f)
}

// missedReport is the report for a turn that sent none: the worker's last
// words on one line, cut to saidMax characters, and RESULT.md when the
// worker's folder has one.
func missedReport(said, cwd string) string {
	said = strings.Join(strings.Fields(said), " ")
	if r := []rune(said); len(r) > saidMax {
		said = string(r[:saidMax]) + "…"
	}
	if said == "" {
		said = "(none)"
	}
	text := "ended its turn without a report. Last words: " + said
	if cwd != "" {
		if _, err := os.Stat(filepath.Join(cwd, "RESULT.md")); err == nil {
			text += "; RESULT.md in " + cwd
		}
	}
	return text
}
