package app

import (
	"net/url"
	"strings"
)

// openableLink reports whether a terminal link may open without a question:
// only http, https and mailto addresses (D94). The scheme is case blind.
func openableLink(link string) bool {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

// terminalOpenLink is the terminal's OpenLink: it opens an allowed address
// with open and takes every other link, file paths included, so the plugin
// does not open it with the system. It runs on the UI thread; open must not
// block.
func terminalOpenLink(open func(string)) func(string) bool {
	return func(link string) bool {
		if openableLink(link) {
			open(link)
		}
		return true
	}
}
