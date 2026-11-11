package diagramspike

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
)

// The examples of the report, in both syntaxes.
const (
	ownFlow = "Claude Code -> transcript.jsonl: writes\n" +
		"Claude Code -> Stop hook: turn ends\n" +
		"Stop hook -> session file: saves path\n" +
		"session file -> reader: path\n" +
		"transcript.jsonl -> reader: new lines\n" +
		"reader -> chat rows: entries\n"
	ownFlowState = ownFlow + "reader [running]\nchat rows [done]\n"
	mmFlow       = "flowchart LR\n" +
		"  cc[Claude Code] -->|writes| tj[transcript.jsonl]\n" +
		"  cc -->|turn ends| hook[Stop hook]\n" +
		"  hook -->|saves path| sf[session file]\n" +
		"  sf -->|path| rd[reader]\n" +
		"  tj -->|new lines| rd\n" +
		"  rd -->|entries| rows[chat rows]\n"
	// A branch and a loop back: review fails, the builder fixes.
	ownBranch = "issue -> plan: owner picks\n" +
		"plan -> build\n" +
		"build -> review: PR\n" +
		"review -> merge: approved\n" +
		"review -> build: blockers\n" +
		"merge -> release\n" +
		"merge [done]\nreview [waiting]\n"
	ownSeq = "owner -> orchestrator: keep the ring in Light\n" +
		"orchestrator -> b-649: message: keep a 1px ring\n" +
		"b-649 -> GitHub: push 77ab310\n" +
		"GitHub --> orchestrator: checks: 3 pass\n" +
		"orchestrator --> owner: the ring is back in Light\n"
	mmSeq = "sequenceDiagram\n" +
		"  participant owner\n  participant lead as orchestrator\n  participant b as b-649\n  participant GitHub\n" +
		"  owner->>lead: keep the ring in Light\n" +
		"  lead->>b: message: keep a 1px ring\n" +
		"  b->>GitHub: push 77ab310\n" +
		"  GitHub-->>lead: checks: 3 pass\n" +
		"  lead-->>owner: the ring is back in Light\n"
	bigFlow = "a -> b\nb -> c\nc -> d\nd -> e\ne -> f\nf -> g\ng -> h\nh -> i\ni -> j\nj -> k\nk -> l\nl -> m\n"
)

func lines(s string) []string { return strings.Split(strings.TrimRight(s, "\n"), "\n") }

func TestParseOwnAndMermaidAgree(t *testing.T) {
	a, ok := ParseOwnFlow(lines(ownFlow))
	if !ok {
		t.Fatal("own flow does not parse")
	}
	b, ok := ParseMermaidFlow(lines(mmFlow))
	if !ok {
		t.Fatal("mermaid flow does not parse")
	}
	if len(a.Nodes) != 6 || len(b.Nodes) != 6 || len(a.Edges) != 6 || len(b.Edges) != 6 {
		t.Fatalf("own %d/%d, mermaid %d/%d", len(a.Nodes), len(a.Edges), len(b.Nodes), len(b.Edges))
	}
	for i := range a.Edges {
		ea, eb := a.Edges[i], b.Edges[i]
		if a.Nodes[ea.From].Label != b.Nodes[eb.From].Label || a.Nodes[ea.To].Label != b.Nodes[eb.To].Label || ea.Label != eb.Label {
			t.Errorf("edge %d: own %+v, mermaid %+v", i, ea, eb)
		}
	}
	sa, ok1 := ParseOwnSeq(lines(ownSeq))
	sb, ok2 := ParseMermaidSeq(lines(mmSeq))
	if !ok1 || !ok2 || strings.Join(sa.Actors, ",") != strings.Join(sb.Actors, ",") || len(sa.Msgs) != len(sb.Msgs) {
		t.Fatalf("seq: own %v %+v, mermaid %v %+v", ok1, sa, ok2, sb)
	}
	for i := range sa.Msgs {
		if sa.Msgs[i] != sb.Msgs[i] {
			t.Errorf("msg %d: own %+v, mermaid %+v", i, sa.Msgs[i], sb.Msgs[i])
		}
	}
}

func TestParseMermaidForms(t *testing.T) {
	ok := []string{
		"graph TD\nA --> B",
		"flowchart LR\nA[One] -- a label --> B(Two)",
		"flowchart LR\nA{Ask?} -->|yes| B([Done]):::done",
		"flowchart LR\n%% a comment\nclassDef x fill:#f00\nA --> B\nstyle A fill:#f00",
	}
	for _, s := range ok {
		if _, good := ParseMermaidFlow(lines(s)); !good {
			t.Errorf("does not parse: %q", s)
		}
	}
	bad := []string{
		"A --> B",                          // no head
		"flowchart LR\nA --> B --> C",      // a chain
		"flowchart LR\nA & B --> C",        // &
		"sequenceDiagram\nloop every 5s",   // blocks
		"flowchart LR\nA -- x --> B --> C", // a chain with a label
	}
	for _, s := range bad {
		_, f := ParseMermaidFlow(lines(s))
		_, q := ParseMermaidSeq(lines(s))
		if f || q {
			t.Errorf("parses: %q", s)
		}
	}
}

func TestParseOwnForms(t *testing.T) {
	if _, ok := ParseOwnFlow([]string{"A -> B -> C"}); ok {
		t.Error("a chain parses")
	}
	if _, ok := ParseOwnFlow([]string{"just words"}); ok {
		t.Error("a line without an arrow parses")
	}
	f, ok := ParseOwnFlow([]string{"a -> b: x: y"})
	if !ok || f.Edges[0].Label != "x: y" {
		t.Errorf("label with a colon: %+v", f)
	}
}

func TestLayoutCycleAndLimits(t *testing.T) {
	f, _ := ParseOwnFlow(lines(ownBranch))
	l := Layout(f)
	back := 0
	for _, b := range l.Back {
		if b {
			back++
		}
	}
	if back != 1 || len(l.Layers) != 6 {
		t.Errorf("back edges %d, layers %d: %+v", back, len(l.Layers), l)
	}
	big, _ := ParseOwnFlow(lines(bigFlow))
	if ok, _ := big.Fits(); ok {
		t.Error("13 nodes fit")
	}
}

const sheetW = 920

func block(name, src string, fn func(c *ui.Context)) apptest.SheetPart {
	return apptest.SheetPart{Name: name, W: sheetW + 32, H: 0, View: func(c *ui.Context) {
		pal := theme.Current()
		ui.Box(c).Fill().Background(pal.Stage).Border(1, pal.PanelLine).Radius(theme.Radius12).Padding(16).Children(func() {
			ui.Column(c).Gap(theme.Space10).Children(func() {
				ui.Text(c, "Claude's answer, before the block.").FontSize(theme.StyleProse.Size).TextColor(pal.Ink88)
				fn(c)
				ui.Text(c, "As plain text in a terminal:").FontSize(theme.StyleSmall.Size).TextColor(pal.Ink48)
				Fallback(c, src, "")
			})
		})
	}}
}

// TestSpecSheet renders the experiments in Dark and Light. With CONTR_SHEET=all it writes
// tmp/sheet/Spike*-<theme>.png.
func TestSpecSheet(t *testing.T) {
	ownF, _ := ParseOwnFlow(lines(ownFlow))
	ownFS, _ := ParseOwnFlow(lines(ownFlowState))
	mmF, _ := ParseMermaidFlow(lines(mmFlow))
	br, _ := ParseOwnFlow(lines(ownBranch))
	ownS, _ := ParseOwnSeq(lines(ownSeq))
	mmS, _ := ParseMermaidSeq(lines(mmSeq))
	big, _ := ParseOwnFlow(lines(bigFlow))
	fence := func(info, s string) string { return "```" + info + "\n" + s + "```" }
	parts := []apptest.SheetPart{
		block("SpikeFlowOwn", fence("contr-flow", ownFlow), func(c *ui.Context) { FlowView(c, ownF, ownFlow, sheetW) }),
		block("SpikeFlowState", fence("contr-flow", ownFlowState), func(c *ui.Context) { FlowView(c, ownFS, ownFlowState, sheetW) }),
		block("SpikeFlowMermaid", fence("mermaid", mmFlow), func(c *ui.Context) { FlowView(c, mmF, mmFlow, sheetW) }),
		block("SpikeFlowBranch", fence("contr-flow", ownBranch), func(c *ui.Context) { FlowView(c, br, ownBranch, sheetW) }),
		block("SpikeSeqOwn", fence("contr-seq", ownSeq), func(c *ui.Context) { SeqView(c, ownS, ownSeq, sheetW) }),
		block("SpikeSeqMermaid", fence("mermaid", mmSeq), func(c *ui.Context) { SeqView(c, mmS, mmSeq, sheetW) }),
		block("SpikeFallback", fence("contr-flow", bigFlow), func(c *ui.Context) { FlowView(c, big, fence("contr-flow", bigFlow), sheetW) }),
	}
	heights := []int{340, 360, 360, 380, 380, 460, 760}
	names := make([]string, len(parts))
	for i := range parts {
		parts[i].H = heights[i]
		names[i] = parts[i].Name
	}
	apptest.SpecSheet(t, parts, names)
}
