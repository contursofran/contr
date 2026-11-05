package agentstate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/contursofran/contr/internal/agents"
)

// programNames are titles that only name a program, not what it does.
var programNames = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "nu": true, "dash": true, "ksh": true,
	"tmux": true, "claude": true, "login": true, "node": true,
}

var versionTitle = regexp.MustCompile(`^v?\d+\.\d+\.\d+`)

// usableTitle tells if a terminal title can name the terminal: it is not
// empty, not a path, not a program name (D156).
func usableTitle(title string, w agents.Worker) bool {
	t := strings.TrimSpace(title)
	switch {
	case t == "":
		return false
	case strings.ContainsAny(t, "/\\") || strings.HasPrefix(t, "~"):
		return false
	case versionTitle.MatchString(t):
		return false
	}
	l := strings.ToLower(t)
	return !programNames[l] && l != strings.ToLower(w.Command) && l != strings.ToLower(filepath.Base(w.Command))
}

// nameForeign sets Shown on each foreign terminal of ws, which hold one
// role. The order (D156): the hc name when set; the title when usable; else
// the project name, with a number when a project has more of them
// ("contr", "contr 2"; numbers follow the address order).
func nameForeign(ws []agents.Worker) {
	var idx []int
	for i, w := range ws {
		if !w.Foreign {
			continue
		}
		switch {
		case w.Name != "" && w.Name != w.Address:
		case usableTitle(w.Title, w):
			ws[i].Shown = strings.TrimSpace(w.Title)
		default:
			idx = append(idx, i)
		}
	}
	slices.SortFunc(idx, func(a, b int) int { return strings.Compare(ws[a].Address, ws[b].Address) })
	seen := map[string]int{}
	for _, i := range idx {
		base := ws[i].Project
		if base == "" {
			base = filepath.Base(ws[i].Worktree)
		}
		if base == "" || base == "." || base == "/" {
			continue // the address stays
		}
		seen[base]++
		ws[i].Shown = base
		if n := seen[base]; n > 1 {
			ws[i].Shown = base + " " + strconv.Itoa(n)
		}
	}
}
