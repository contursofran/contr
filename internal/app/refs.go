package app

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// RefKind says what a ref of a question opens.
type RefKind int

const (
	RefText  RefKind = iota // nothing: a file path or free text
	RefPR                   // a pull request, in the PR pane
	RefIssue                // an issue, in the browser
	RefURL                  // an http or https address, in the browser
)

// Ref is a ref of a question, read by [ParseRef].
type Ref struct {
	Kind RefKind
	N    int    // the number of a PR or an issue
	URL  string // the address of a URL
}

var (
	prRef    = regexp.MustCompile(`(?i)^(?:pr|pull)\s*#?(\d+)$`)
	issueRef = regexp.MustCompile(`(?i)^(?:issue\s*)?#?(\d+)$`)
)

// ParseRef reads a ref of a question: "PR 12" or "pr#12" is a pull request;
// "#12", "issue 12" or "12" is an issue; an http or https address is a URL.
// Anything else (a file path, a mailto or file address) opens nothing.
func ParseRef(s string) Ref {
	s = strings.TrimSpace(s)
	if m := prRef.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return Ref{Kind: RefPR, N: n}
	}
	if m := issueRef.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return Ref{Kind: RefIssue, N: n}
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		if sc := strings.ToLower(u.Scheme); sc == "http" || sc == "https" {
			return Ref{Kind: RefURL, URL: s}
		}
	}
	return Ref{}
}
