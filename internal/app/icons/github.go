package icons

import "github.com/egoist/mygo/ui"

// GitHub is the state of a pull request or an issue, as its Octicon shows it (D286).
type GitHub uint8

// The GitHub icons. GitHubNone draws nothing.
const (
	GitHubNone GitHub = iota
	PROpen
	PRDraft
	PRMerged
	PRClosed
	IssueOpen
	IssueClosed
)

// githubPrefix starts the key of a GitHub icon in parsed.
const githubPrefix = "github/"

var githubFiles = [...]string{"", "git-pull-request", "git-pull-request-draft", "git-merge",
	"git-pull-request-closed", "issue-opened", "issue-closed"}

var githubLabels = [...]string{"", "open pull request", "draft pull request", "merged pull request",
	"closed pull request", "open issue", "closed issue"}

// GitHubIcons lists the icons in order, for the tests and the sheet.
var GitHubIcons = [...]GitHub{PROpen, PRDraft, PRMerged, PRClosed, IssueOpen, IssueClosed}

// PRIcon is the icon of a pull request in state ("OPEN", "MERGED" or "CLOSED", gh's words).
// An unknown state is open.
func PRIcon(state string, draft bool) GitHub {
	switch {
	case state == "MERGED":
		return PRMerged
	case state == "CLOSED":
		return PRClosed
	case draft:
		return PRDraft
	}
	return PROpen
}

// IssueIcon is the icon of an issue in state ("OPEN" or "CLOSED"). An unknown state is open.
func IssueIcon(state string) GitHub {
	if state == "CLOSED" {
		return IssueClosed
	}
	return IssueOpen
}

// File is the base name of the icon's SVG in assets/icons/github, "" for none.
func (g GitHub) File() string { return githubFiles[g] }

// Label is the accessible label of the icon: "merged pull request".
func (g GitHub) Label() string { return githubLabels[g] }

// GitHubSVG returns the Octicon of g, nil for GitHubNone or a missing file. It has one fill, so it
// takes the color it is drawn in: an ink token, never GitHub's colors (D119, D261, D286).
func GitHubSVG(g GitHub) *ui.SVG {
	if g == GitHubNone {
		return nil
	}
	return svg(githubPrefix + g.File())
}

// GitHubIcon shows the icon of g at size DIPs in color col. GitHubNone and a missing file show an
// empty box.
func GitHubIcon(c *ui.Context, g GitHub, size float32, col ui.Color) ui.Element {
	if s := GitHubSVG(g); s != nil {
		return ui.Icon(c, s).Size(size, size).TextColor(col).Label(g.Label())
	}
	return ui.Box(c).Size(size, size)
}
