package app

import (
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/spaces"
)

func TestClassifyQuery(t *testing.T) {
	tests := []struct {
		in          string
		kind        queryKind
		owner, name string
	}{
		{"", queryFilter, "", ""},
		{"   ", queryFilter, "", ""},
		{"rune", queryFilter, "", ""},
		{"contursofran/contr-site", queryRepo, "contursofran", "contr-site"},
		{"https://github.com/o/n", queryRepo, "o", "n"},
		{"https://github.com/o/n.git", queryRepo, "o", "n"},
		{"https://www.github.com/o/n/", queryRepo, "o", "n"},
		{"github.com/o/n/", queryRepo, "o", "n"},
		{"git@github.com:o/n.git", queryRepo, "o", "n"},
		{"/tmp/x", queryPath, "", ""},
		{"~/code/x", queryPath, "", ""},
		{"a/b/c", queryFilter, "", ""},
		{"https://gitlab.com/o/n", queryFilter, "", ""},
		{"o/n x", queryFilter, "", ""},
		{"o/..", queryFilter, "", ""},
	}
	for _, tt := range tests {
		k, o, n := classifyQuery(tt.in)
		if k != tt.kind || o != tt.owner || n != tt.name {
			t.Errorf("classifyQuery(%q) = %v %q %q, want %v %q %q", tt.in, k, o, n, tt.kind, tt.owner, tt.name)
		}
	}
}

func TestMatchSpan(t *testing.T) {
	if s, e, ok := matchSpan("contr-rune", "RUNE"); !ok || s != 6 || e != 10 {
		t.Errorf("matchSpan RUNE = %d %d %v, want 6 10 true", s, e, ok)
	}
	if _, _, ok := matchSpan("contr-rune", "zzz"); ok {
		t.Error("matchSpan with no match: ok = true")
	}
	if _, _, ok := matchSpan("contr-rune", ""); ok {
		t.Error("matchSpan with empty filter: ok = true")
	}
	// U+0130 changes length when lowered: no wrong offsets.
	if _, _, ok := matchSpan("İx", "x"); ok {
		t.Error("matchSpan with length-changing case: ok = true")
	}
}

func TestAgoText(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{-time.Hour, "now"},
		{10 * time.Minute, "10m ago"},
		{3 * time.Hour, "3h ago"},
		{48 * time.Hour, "2d ago"},
	}
	for _, tt := range tests {
		if got := agoText(now, now.Add(-tt.ago)); got != tt.want {
			t.Errorf("agoText(%v) = %q, want %q", tt.ago, got, tt.want)
		}
	}
}

func TestOpenMeta(t *testing.T) {
	hold := "orchestrator idle"
	g := spaces.RepoGroup{
		CommonDir: "/home/me/code/contr/.git",
		Branch:    "main",
		Rows:      []spaces.WorktreeRow{{Root: "/home/me/code/contr", IsMain: true}},
	}
	meta, n := openMeta(g, "/home/me")
	if meta != "~/code/contr · main" || n != 0 {
		t.Errorf("no holders: %q %d", meta, n)
	}

	g.Rows[0].Hold = &hold
	if meta, _ = openMeta(g, "/home/me"); meta != "~/code/contr · main · orchestrator idle" {
		t.Errorf("with hold: %q", meta)
	}
	if strings.Contains(meta, "0 worktrees") || strings.Contains(meta, "0 need you") {
		t.Errorf("zero count shown: %q", meta)
	}

	g.Rows = append(g.Rows,
		spaces.WorktreeRow{Root: "/w/a", HoldYou: true},
		spaces.WorktreeRow{Root: "/w/b", HoldYou: true})
	meta, n = openMeta(g, "/home/me")
	if n != 2 || meta != "~/code/contr · main · 2 need you" {
		t.Errorf("need you: %q %d", meta, n)
	}

	base, live, _ := openMetaParts(g, "/home/me")
	if base != "~/code/contr · main" || live != "2 need you" {
		t.Errorf("parts: %q %q", base, live)
	}

	// A path outside home stays as it is, and a missing branch is left out.
	if meta, _ = openMeta(spaces.RepoGroup{Path: "/srv/x"}, "/home/me"); meta != "/srv/x" {
		t.Errorf("no branch: %q", meta)
	}
	// A group with no rows takes its path from the common dir.
	if meta, _ = openMeta(spaces.RepoGroup{CommonDir: "/home/me/p/.git", Branch: "dev"}, "/home/me"); meta != "~/p · dev" {
		t.Errorf("no rows: %q", meta)
	}
}
