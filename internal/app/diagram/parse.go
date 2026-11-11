// Package diagram draws the flow diagrams of Claude's answers (D284): a fenced contr-flow block,
// one arrow per line, drawn with MyGo's Painter.
package diagram

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Lang is the language of the fenced block that holds a flow.
const Lang = "contr-flow"

// State is the state of a node: none, or one of the content palette (D261).
type State string

// Flow is a flow diagram: nodes in order of first use, edges in the order written.
type Flow struct {
	Nodes []Node
	Edges []Edge
}

// Node is a box of a flow.
type Node struct {
	Label string
	State State
}

// Edge is an arrow of a flow, with an optional label.
type Edge struct {
	From, To int
	Label    string
}

// Errors of Parse.
var (
	ErrEmpty = errors.New("no arrow")
	ErrChain = errors.New("more than one arrow on a line")
)

var (
	// "Claude Code -> transcript.jsonl: writes"; a node name is any text without "->" or ":".
	edgeRe = regexp.MustCompile(`^(.+?)\s*->\s*(.+?)(?:\s*:\s*(.+?))?\s*$`)
	// "reader [running]": a node with a state.
	stateRe = regexp.MustCompile(`^(.+?)\s*\[(done|running|waiting|failed|info)\]\s*$`)
)

func (f *Flow) node(label string) int {
	for i, n := range f.Nodes {
		if n.Label == label {
			return i
		}
	}
	f.Nodes = append(f.Nodes, Node{Label: label})
	return len(f.Nodes) - 1
}

// Parse reads the text of a contr-flow block. A line is "A -> B", "A -> B: label" or "A [state]";
// empty lines are skipped. A chain "A -> B -> C" is an error.
func Parse(src string) (Flow, error) {
	var f Flow
	for k, l := range strings.Split(src, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if m := stateRe.FindStringSubmatch(l); m != nil && !strings.Contains(m[1], "->") {
			f.Nodes[f.node(m[1])].State = State(m[2])
			continue
		}
		m := edgeRe.FindStringSubmatch(l)
		if m == nil {
			return Flow{}, fmt.Errorf("line %d: not \"A -> B\" or \"A [state]\"", k+1)
		}
		if strings.Contains(m[2], "->") {
			return Flow{}, fmt.Errorf("line %d: %w", k+1, ErrChain)
		}
		from, to := f.node(m[1]), f.node(m[2])
		f.Edges = append(f.Edges, Edge{From: from, To: to, Label: m[3]})
	}
	if len(f.Edges) == 0 {
		return Flow{}, ErrEmpty
	}
	return f, nil
}
