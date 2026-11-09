package apptest

import "github.com/contursofran/contr/internal/owner"

// The addresses of an orchestrator terminal and a worker terminal in the outline tests.
const (
	OrchAddr = "w1:t1"
	WorkAddr = "w1:t2"
)

// Questions gives n questions of project, with the ids D1, D2, ….
func Questions(project string, n int) []owner.Question {
	var out []owner.Question
	for i := 0; i < n; i++ {
		out = append(out, owner.Question{ID: "D" + string(rune('1'+i)), Project: project, Title: "b-250 checks"})
	}
	return out
}
