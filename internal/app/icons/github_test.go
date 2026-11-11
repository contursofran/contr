package icons

import "testing"

func TestPRIconMap(t *testing.T) {
	for _, tc := range []struct {
		state string
		draft bool
		want  GitHub
	}{
		{"OPEN", false, PROpen}, {"OPEN", true, PRDraft}, {"MERGED", false, PRMerged},
		{"MERGED", true, PRMerged}, {"CLOSED", false, PRClosed}, {"CLOSED", true, PRClosed},
		{"", false, PROpen},
	} {
		if got := PRIcon(tc.state, tc.draft); got != tc.want {
			t.Errorf("PRIcon(%q, %v) = %v, want %v", tc.state, tc.draft, got, tc.want)
		}
	}
}

func TestIssueIconMap(t *testing.T) {
	for state, want := range map[string]GitHub{"OPEN": IssueOpen, "CLOSED": IssueClosed, "": IssueOpen} {
		if got := IssueIcon(state); got != want {
			t.Errorf("IssueIcon(%q) = %v, want %v", state, got, want)
		}
	}
}

func TestEveryGitHubIconLoads(t *testing.T) {
	want := []string{"git-pull-request", "git-pull-request-draft", "git-merge",
		"git-pull-request-closed", "issue-opened", "issue-closed"}
	for i, g := range GitHubIcons {
		if g.File() != want[i] {
			t.Errorf("%d: file %q, want %q", g, g.File(), want[i])
		}
		if GitHubSVG(g) == nil {
			t.Errorf("%s does not load", g.File())
		}
		if g.Label() == "" {
			t.Errorf("%s has no label", g.File())
		}
	}
	if GitHubSVG(GitHubNone) != nil {
		t.Error("GitHubNone has an icon")
	}
}
