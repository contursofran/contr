package agentstate

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/contursofran/contr/internal/agents"
)

// programNames are titles that only name a program, not what it does.
var programNames = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "nu": true, "dash": true, "ksh": true,
	"tmux": true, "claude": true, "claude code": true, "login": true, "node": true,
}

var versionTitle = regexp.MustCompile(`^v?\d+\.\d+\.\d+`)

// cleanTitle removes the status or spinner glyph that Claude Code puts before
// its title (a star, a Braille dot, a symbol) and the space after it.
func cleanTitle(title string) string {
	t := strings.TrimSpace(title)
	for {
		r, n := utf8.DecodeRuneInString(t)
		if n == 0 || !isGlyph(r) {
			return t
		}
		t = strings.TrimSpace(t[n:])
	}
}

// isGlyph tells if r is a symbol (Braille U+2800-U+28FF, dingbats like the
// star, arrows, shapes) and not a letter, digit or punctuation.
func isGlyph(r rune) bool {
	if r >= 0x2800 && r <= 0x28FF {
		return true
	}
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && (unicode.IsSymbol(r) || unicode.Is(unicode.So, r))
}

// usableTitle tells if a terminal title can name the terminal: after the
// glyph is removed, it is not empty, not a path, not a program name (D156).
func usableTitle(title string, w agents.Worker) bool {
	t := cleanTitle(title)
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

// Numbering keeps the number of each terminal that is named after its
// project: project name, then address, then number. A terminal keeps its
// number while it lives; a new one takes the lowest free number (D156).
type Numbering map[string]map[string]int

// nameForeign sets Shown on each foreign terminal of ws, which hold one
// role. The order (D156): the hc name when set; the title when usable; else
// the project name, with a number when a project has more of them
// ("contr", "contr 2"). num may be nil: then numbers are not kept.
func nameForeign(ws []agents.Worker, num Numbering) {
	if num == nil {
		num = Numbering{}
	}
	byBase := map[string][]int{}
	for i, w := range ws {
		if !w.Foreign {
			continue
		}
		switch {
		case w.Name != "" && w.Name != w.Address:
		case usableTitle(w.Title, w):
			ws[i].Shown = cleanTitle(w.Title)
		default:
			base := w.Project
			if base == "" {
				base = filepath.Base(w.Worktree)
			}
			if base == "" || base == "." || base == "/" {
				continue // the address stays
			}
			byBase[base] = append(byBase[base], i)
		}
	}
	for base := range num { // forget the project that has no terminal now
		if _, ok := byBase[base]; !ok {
			delete(num, base)
		}
	}
	for base, idx := range byBase {
		held := num[base]
		if held == nil {
			held = map[string]int{}
			num[base] = held
		}
		live := map[string]bool{}
		for _, i := range idx {
			live[ws[i].Address] = true
		}
		for addr := range held { // an ended terminal frees its number
			if !live[addr] {
				delete(held, addr)
			}
		}
		slices.SortFunc(idx, func(a, b int) int { return strings.Compare(ws[a].Address, ws[b].Address) })
		for _, i := range idx {
			if _, ok := held[ws[i].Address]; ok {
				continue
			}
			n := 1
			for slices.Contains(valuesOf(held), n) {
				n++
			}
			held[ws[i].Address] = n
		}
		for _, i := range idx {
			ws[i].Shown = base
			if n := held[ws[i].Address]; n > 1 {
				ws[i].Shown = base + " " + strconv.Itoa(n)
			}
		}
	}
}

func valuesOf(m map[string]int) []int {
	v := make([]int, 0, len(m))
	for _, n := range m {
		v = append(v, n)
	}
	return v
}
