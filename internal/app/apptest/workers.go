package apptest

import "github.com/contursofran/contr/internal/agents"

// OrchW is a live orchestrator.
func OrchW(addr, name, project string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Project: project, Role: agents.RoleOrchestrator, Live: true}
}

// WorkW is a live builder.
func WorkW(addr, name, project string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Project: project, Role: "builder", Live: true}
}

// TermW is a live plain terminal in worktree.
func TermW(addr, name, project, worktree string) agents.Worker {
	return agents.Worker{Address: addr, Name: name, Project: project, Worktree: worktree, Role: agents.RoleTerminal, Live: true}
}
