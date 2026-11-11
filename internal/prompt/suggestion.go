package prompt

import (
	"bytes"
	"strconv"
	"strings"
)

// promptMark is the mark at the start of Claude Code's input line.
const promptMark = "❯"

// Suggestion is the next prompt that Claude Code suggests in its input line
// when a turn ends (D281), read from the terminal's screen as escape
// sequences (the MyGo terminal's Snapshot). Claude Code writes the
// suggestion as faint text (SGR 2) after the prompt mark. Only the last line
// with the mark counts. It is "" when that line has no text after the mark,
// or when any of that text is not faint: typed text, a sent prompt or a menu
// choice.
func Suggestion(screen []byte) string {
	lines := bytes.Split(screen, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if bytes.Contains(lines[i], []byte(promptMark)) {
			return faintAfterMark(string(lines[i]))
		}
	}
	return ""
}

// faintAfterMark reads line after its first prompt mark: the text if all
// of it is faint, else "".
func faintAfterMark(line string) string {
	faint, seen := false, false
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			n, f, ok := csi(line[i:], faint)
			if !ok {
				return ""
			}
			faint, i = f, i+n
			continue
		}
		if !seen {
			if strings.HasPrefix(line[i:], promptMark) {
				seen = true
				i += len(promptMark)
			} else {
				i++
			}
			continue
		}
		c := line[i]
		i++
		if c == '\r' || c < 0x20 {
			continue
		}
		if c != ' ' && !faint {
			return ""
		}
		b.WriteByte(c)
	}
	return strings.TrimSpace(b.String())
}

// csi reads the escape sequence at the start of s: its length and the faint
// state after it. Only SGR changes the state. ok is false for an escape that
// is not a CSI sequence.
func csi(s string, faint bool) (n int, after bool, ok bool) {
	if len(s) < 2 || s[1] != '[' {
		return 0, faint, false
	}
	end := 2
	for end < len(s) && (s[end] < 0x40 || s[end] > 0x7e) {
		end++
	}
	if end == len(s) {
		return 0, faint, false
	}
	if s[end] != 'm' {
		return end + 1, faint, true
	}
	params := strings.Split(s[2:end], ";")
	for j := 0; j < len(params); j++ {
		p := params[j]
		if k := strings.IndexByte(p, ':'); k >= 0 {
			p = p[:k] // a colon form (38:2:r:g:b) is one parameter
		}
		v, err := strconv.Atoi(p)
		if p == "" {
			v, err = 0, nil
		}
		if err != nil {
			continue
		}
		switch {
		case v == 0 || v == 22:
			faint = false
		case v == 2:
			faint = true
		case (v == 38 || v == 48 || v == 58) && !strings.Contains(params[j], ":") && j+1 < len(params):
			// Skip the color's own numbers: 5;n or 2;r;g;b.
			switch params[j+1] {
			case "5":
				j += 2
			case "2":
				j += 4
			}
		}
	}
	return end + 1, faint, true
}
