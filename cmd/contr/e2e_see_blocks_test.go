//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/session"
)

// blocksGH is a fake gh for the chat-blocks fixture: the PR of each worker's branch (by the
// folder's name) and the numbers of the board and the queue. Any other number is not found.
const blocksGH = `#!/bin/sh
ok='{"status":"COMPLETED","conclusion":"SUCCESS"}'
run='{"status":"IN_PROGRESS"}'
bad='{"status":"COMPLETED","conclusion":"FAILURE"}'
pr() { printf '{"number":%s,"state":"%s","statusCheckRollup":[%s]}\n' "$1" "$2" "$3"; }
none() { echo "GraphQL: Could not resolve to a PullRequest with the number of $1." >&2; exit 1; }
case "$1 $2" in
"pr view")
	n=$3
	[ "$n" = --json ] && n=$(basename "$PWD")
	case "$n" in
	656|b-649) pr 656 OPEN "$ok,$ok,$run" ;;
	667|b-658) pr 667 OPEN "$ok,$ok,$ok" ;;
	663) pr 663 MERGED "$ok,$ok,$ok" ;;
	664) pr 664 OPEN "$ok,$ok,$bad" ;;
	b-*) echo 'no pull requests found for branch "agent"' >&2; exit 1 ;;
	*) none "$n" ;;
	esac ;;
"issue view")
	case "$3" in
	640|649|658|659|665) printf '{"number":%s,"state":"OPEN","title":"Issue %s"}\n' "$3" "$3" ;;
	*) none "$3" ;;
	esac ;;
*) exit 1 ;;
esac
`

// seeChatBlocks spawns lead and four builders, each in a repository of its own, puts a fake gh
// on PATH and writes a transcript with two status boards and a merge queue (D267).
func seeChatBlocks(t *testing.T, h *harness) {
	bin := filepath.Join(h.dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(blocksGH), 0o755); err != nil {
		t.Fatal(err)
	}
	h.env = append(h.env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	repo := h.repo(t, "demo", "main")
	lead, err := h.mgr.Spawn(h.ctx, agents.Spec{Name: "lead", Profile: "claude-working", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo})
	if err != nil {
		t.Fatalf("spawn lead: %v", err)
	}
	for _, sp := range []agents.Spec{
		{Name: "b-649", Profile: "claude-working", Issue: "649"},
		{Name: "b-665", Profile: "claude-working", Issue: "665"},
		{Name: "b-658", Profile: "claude-idle", Issue: "658"},
		{Name: "b-659", Profile: "claude-idle", Issue: "659"},
	} {
		sp.Role, sp.Project, sp.Parent = "builder", "demo", lead.Address
		sp.Worktree = h.repo(t, sp.Name, "agent/"+sp.Issue)
		if _, err := h.mgr.Spawn(h.ctx, sp); err != nil {
			t.Fatalf("spawn %s: %v", sp.Name, err)
		}
	}
	path := filepath.Join(h.home, ".claude", "projects", "demo", "blocks.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(blocksTranscript()), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &session.Session{UpdatedAt: uint64(time.Now().Unix())}
	s.SetTranscript("blocks", path)
	if err := session.Write(filepath.Join(h.dir, "state"), lead.Address, s); err != nil {
		t.Fatal(err)
	}
}

// blocksTranscript is a coordinator's session as Claude Code writes it: a prompt, an answer with
// a board, a later prompt, and an answer with a newer board and the merge queue.
func blocksTranscript() string {
	n := 0
	start := time.Now().Add(-6 * time.Minute)
	line := func(typ string, sec int, msg map[string]any) string {
		n++
		b, _ := json.Marshal(map[string]any{"type": typ, "uuid": fmt.Sprintf("u%d", n), "sessionId": "blocks",
			"timestamp": start.Add(time.Duration(sec) * time.Second).UTC().Format(time.RFC3339Nano), "message": msg})
		return string(b) + "\n"
	}
	user := func(sec int, text string) string {
		return line("user", sec, map[string]any{"role": "user", "content": text})
	}
	said := func(sec int, text string) string {
		return line("assistant", sec, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}})
	}
	return strings.Join([]string{
		user(0, "start builders for 649, 665, 658 and 659"),
		said(20, "Four builders are out.\n\n```contr-status\nb-649 | #649 | Rows and badge\nb-665 | #665 | Fewer lines\n"+
			"b-658 | #658 | First-run texts\nb-659 | #659 | Short transitions\n```\n\nI wait for their reports."),
		user(300, "how is it going?"),
		said(320, "Two PRs are open. The terminal of b-640 is gone; I start it again after the queue.\n\n```contr-status\n"+
			"b-649 | #649 | Rows and badge\nb-665 | #665 | Fewer lines\nb-658 | #658 | First-run texts\nb-659 | #659 | Short transitions\n"+
			"b-640 | #640 | Settings tab\n```\n\n"+
			"### Merge order\n\nAll of them change the sidebar, so I merge them one at a time.\n\n```contr-queue\n"+
			"#663 Status icons\n#656 Rows and badge\n#664 Badge colors -- the owner reads it first\n"+
			"#667 First-run texts -- waits for #656, then merges main\n#659 Short transitions -- waits for #656\n```"),
	}, "")
}
