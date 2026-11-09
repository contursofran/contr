package app

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/git"
)

// queryKind tells what the text in the projects modal field means.
type queryKind int

const (
	// queryFilter filters the project list.
	queryFilter queryKind = iota
	// queryRepo is a GitHub repository to clone.
	queryRepo
	// queryPath is a folder path to open.
	queryPath
)

// githubRE matches the GitHub forms that mean a repository: a web URL and an
// SSH URL. The groups are the owner and the name.
var githubRE = regexp.MustCompile(`^(?:https?://(?:www\.)?github\.com/|git@github\.com:)([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// ownerNameRE matches `owner/name` and `github.com/owner/name`.
var ownerNameRE = regexp.MustCompile(`^(?:github\.com/)?([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// classifyQuery tells what s is. A repository gives its owner and name. Text
// that git.ParseRemote rejects, or that is not a GitHub form, is a filter.
func classifyQuery(s string) (k queryKind, owner, name string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return queryFilter, "", ""
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") {
		return queryPath, "", ""
	}
	m := githubRE.FindStringSubmatch(s)
	if m == nil {
		m = ownerNameRE.FindStringSubmatch(s)
	}
	if m == nil {
		return queryFilter, "", ""
	}
	// Check the folder name with the same parser as the clone.
	in := strings.TrimSuffix(s, "/")
	if strings.HasPrefix(in, "github.com/") {
		in = "https://" + in
	}
	_, n, err := git.ParseRemote(in)
	if err != nil || n == "" {
		return queryFilter, "", ""
	}
	return queryRepo, m[1], n
}

// matchSpan gives the byte offsets of the first match of f in name, without
// regard to case. ok is false when f is empty or does not match.
func matchSpan(name, f string) (start, end int, ok bool) {
	if f == "" {
		return 0, 0, false
	}
	ln, lf := strings.ToLower(name), strings.ToLower(f)
	// Lowering can change the byte length of some letters. The offsets would
	// then be wrong for name, so give no match.
	if len(ln) != len(name) {
		return 0, 0, false
	}
	i := strings.Index(ln, lf)
	if i < 0 {
		return 0, 0, false
	}
	return i, i + len(lf), true
}

// agoText is the age of at, as shown in Recent: "now", "5m ago", "3h ago" or
// "2d ago". A time in the future is "now".
func agoText(now, at time.Time) string {
	d := now.Sub(at)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h ago"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d ago"
}

// openMeta is the meta line of an open project: "<path> · <branch> · <live>".
// The path starts with ~ when it is in home. The live part is "N need you"
// when N rows wait for the owner, else the Hold text of the main row. Parts
// that are empty are left out. needYou is N.
func openMeta(g spaces.RepoGroup, home string) (meta string, needYou int) {
	base, live, needYou := openMetaParts(g, home)
	return strings.Join(slices.DeleteFunc([]string{base, live}, func(s string) bool { return s == "" }), " · "), needYou
}

// openMetaParts is openMeta in two parts: "<path> · <branch>" and the live
// state. The view draws a dot between them for an orchestrator.
func openMetaParts(g spaces.RepoGroup, home string) (base, live string, needYou int) {
	path := g.Path
	for _, r := range g.Rows {
		if r.HoldYou {
			needYou++
		}
		if r.IsMain {
			if path == "" {
				path = r.Root
			}
			if r.Hold != nil {
				live = *r.Hold
			}
		}
	}
	if path == "" && g.CommonDir != "" {
		path = g.CommonDir
		if filepath.Base(path) == ".git" {
			path = filepath.Dir(path)
		}
	}
	if needYou > 0 {
		live = strconv.Itoa(needYou) + " need you"
	}
	if home != "" {
		if path == home {
			path = "~"
		} else if strings.HasPrefix(path, home+"/") {
			path = "~" + path[len(home):]
		}
	}
	var parts []string
	for _, p := range []string{path, g.Branch} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · "), live, needYou
}
