// Package diagramspike is the throwaway proof of issue #696, step 1: flow and sequence diagrams
// drawn with MyGo's Painter from a small text syntax. It is not used by the app.
package diagramspike

import (
	"regexp"
	"strings"
)

// State is the state of a node: none, or one of the content palette (D261).
type State string

// Flow is a flow diagram: nodes in order of first use, edges in the order written.
type Flow struct {
	Nodes []Node
	Edges []Edge
}

// Node is a box of a flow.
type Node struct {
	ID, Label string
	State     State
}

// Edge is an arrow of a flow, with an optional label.
type Edge struct {
	From, To int
	Label    string
}

// Seq is a sequence diagram: actors left to right, messages top to bottom.
type Seq struct {
	Actors []string
	Msgs   []Msg
}

// Msg is one arrow of a sequence; Dashed is a reply.
type Msg struct {
	From, To int
	Label    string
	Dashed   bool
}

func (f *Flow) node(id, label string) int {
	for i, n := range f.Nodes {
		if n.ID == id {
			if label != "" {
				f.Nodes[i].Label = label
			}
			return i
		}
	}
	if label == "" {
		label = id
	}
	f.Nodes = append(f.Nodes, Node{ID: id, Label: label})
	return len(f.Nodes) - 1
}

func (s *Seq) actor(name string) int {
	for i, a := range s.Actors {
		if a == name {
			return i
		}
	}
	s.Actors = append(s.Actors, name)
	return len(s.Actors) - 1
}

// ---- Own syntax: contr-flow and contr-seq ----

var (
	// "Claude Code -> transcript.jsonl: writes"; a node name is any text without "->" or ":".
	ownEdgeRe = regexp.MustCompile(`^(.+?)\s*->\s*(.+?)(?:\s*:\s*(.+?))?\s*$`)
	// "reader [running]": a node with a state.
	ownStateRe = regexp.MustCompile(`^(.+?)\s*\[(done|running|waiting|failed|info)\]\s*$`)
	// "lead -> b-649: start" or "b-649 --> lead: done" (dashed: a reply).
	ownMsgRe = regexp.MustCompile(`^(.+?)\s*(-->|->)\s*(.+?)\s*:\s*(.+?)\s*$`)
)

// ParseOwnFlow reads the lines of a contr-flow block. A line is "A -> B", "A -> B: label" or
// "A [state]"; a chain "A -> B -> C" is not part of the syntax (the second "->" is in B's name, so
// it fails below).
func ParseOwnFlow(lines []string) (Flow, bool) {
	var f Flow
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if m := ownStateRe.FindStringSubmatch(l); m != nil {
			f.Nodes[f.node(m[1], "")].State = State(m[2])
			continue
		}
		m := ownEdgeRe.FindStringSubmatch(l)
		if m == nil || strings.Contains(m[2], "->") {
			return Flow{}, false
		}
		from, to := f.node(m[1], ""), f.node(m[2], "")
		f.Edges = append(f.Edges, Edge{From: from, To: to, Label: m[3]})
	}
	return f, len(f.Edges) > 0
}

// ParseOwnSeq reads the lines of a contr-seq block: "a -> b: message", "b --> a: reply".
func ParseOwnSeq(lines []string) (Seq, bool) {
	var s Seq
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		m := ownMsgRe.FindStringSubmatch(l)
		if m == nil {
			return Seq{}, false
		}
		from, to := s.actor(m[1]), s.actor(m[3])
		s.Msgs = append(s.Msgs, Msg{From: from, To: to, Label: m[4], Dashed: m[2] == "-->"})
	}
	return s, len(s.Msgs) > 0
}

// ---- Mermaid subset ----

var (
	mmHeadRe = regexp.MustCompile(`^(flowchart|graph)\s+(LR|TD|TB|RL|BT)\s*;?$`)
	// A node: an id, then an optional shape with a label: A, A[x], A(x), A{x}, A([x]), A((x)), A>x].
	mmNode = `([A-Za-z0-9_\-.]+)\s*(?:\[\[(.+?)\]\]|\(\[(.+?)\]\)|\(\((.+?)\)\)|\[(.+?)\]|\((.+?)\)|\{(.+?)\}|>(.+?)\])?`
	// An arrow, with a label as |x| after it or as "-- x -->" around it.
	mmArrow  = `\s*(?:--\s*([^-|>][^|>]*?)\s*-->|-->\s*\|([^|]*)\|\s*|-->|---|-\.->|==>|-\.-)\s*`
	mmEdgeRe = regexp.MustCompile(`^` + mmNode + mmArrow + mmNode + `\s*;?$`)
	mmOneRe  = regexp.MustCompile(`^` + mmNode + `\s*;?$`)
	mmSkipRe = regexp.MustCompile(`^(%%|classDef\s|class\s|style\s|linkStyle\s|click\s|subgraph\b|end$|direction\s)`)
	// ":::done" after a node: Mermaid's class shorthand, used here for the state.
	mmClassRe = regexp.MustCompile(`^(.+?):::(done|running|waiting|failed|info)\s*;?$`)

	mmSeqHeadRe = regexp.MustCompile(`^sequenceDiagram\s*$`)
	mmPartRe    = regexp.MustCompile(`^(?:participant|actor)\s+(\S+)(?:\s+as\s+(.+?))?\s*$`)
	mmMsgRe     = regexp.MustCompile(`^([^-\s>]+)\s*(->>|-->>|->|-->|-x|--x|-\))\s*([+-]?)([^:\s]+)\s*:\s*(.*?)\s*$`)
	mmSeqSkipRe = regexp.MustCompile(`^(%%|autonumber|activate\s|deactivate\s|Note\s|note\s)`)
)

func mmLabel(m []string, at int) string {
	for _, s := range m[at : at+7] {
		if s != "" {
			return strings.Trim(s, `"`)
		}
	}
	return ""
}

// ParseMermaidFlow reads a subset of Mermaid's flowchart: the "flowchart LR" head, nodes with the
// shapes [x] (x) {x} ([x]) ((x)) [[x]] >x], one arrow per line (-->, ---, -.->, ==>), edge labels
// as -->|x| or -- x -->. classDef, style, click and comments are skipped; a subgraph is read as its
// nodes. Chains "A --> B --> C", "&" and other arrows fail, and the block stays code.
func ParseMermaidFlow(lines []string) (Flow, bool) {
	var f Flow
	head := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if !head {
			if !mmHeadRe.MatchString(l) {
				return Flow{}, false
			}
			head = true
			continue
		}
		if mmSkipRe.MatchString(l) {
			continue
		}
		state := State("")
		if m := mmClassRe.FindStringSubmatch(l); m != nil {
			l, state = m[1], State(m[2])
		}
		if m := mmEdgeRe.FindStringSubmatch(l); m != nil {
			from := f.node(m[1], mmLabel(m, 2))
			label := m[9]
			if label == "" {
				label = m[10]
			}
			to := f.node(m[11], mmLabel(m, 12))
			if state != "" {
				f.Nodes[to].State = state
			}
			f.Edges = append(f.Edges, Edge{From: from, To: to, Label: strings.Trim(strings.TrimSpace(label), `"`)})
			continue
		}
		if m := mmOneRe.FindStringSubmatch(l); m != nil {
			i := f.node(m[1], mmLabel(m, 2))
			if state != "" {
				f.Nodes[i].State = state
			}
			continue
		}
		return Flow{}, false
	}
	return f, len(f.Edges) > 0
}

// ParseMermaidSeq reads a subset of Mermaid's sequenceDiagram: participant and actor lines (with
// "as"), messages with ->> -->> -> --> -x --x -) and their activation marks. Notes, activations
// and autonumber are skipped; loop, alt, opt and par fail, and the block stays code.
func ParseMermaidSeq(lines []string) (Seq, bool) {
	var s Seq
	alias := map[string]string{}
	name := func(id string) string {
		if a, ok := alias[id]; ok {
			return a
		}
		return id
	}
	head := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if !head {
			if !mmSeqHeadRe.MatchString(l) {
				return Seq{}, false
			}
			head = true
			continue
		}
		if mmSeqSkipRe.MatchString(l) {
			continue
		}
		if m := mmPartRe.FindStringSubmatch(l); m != nil {
			if m[2] != "" {
				alias[m[1]] = m[2]
			}
			s.actor(name(m[1]))
			continue
		}
		m := mmMsgRe.FindStringSubmatch(l)
		if m == nil {
			return Seq{}, false
		}
		from, to := s.actor(name(m[1])), s.actor(name(m[4]))
		s.Msgs = append(s.Msgs, Msg{From: from, To: to, Label: m[5], Dashed: strings.HasPrefix(m[2], "--")})
	}
	return s, len(s.Msgs) > 0
}
