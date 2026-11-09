package app

import "strings"

// textPart is a part of a card text: plain text, or the text of a backtick
// span (Code), without the backticks.
type textPart struct {
	Text string
	Code bool
}

// splitCode splits s at its backtick spans (D204). A backtick with no
// closing backtick after it stays plain text, as does an empty span (“).
func splitCode(s string) []textPart {
	var parts []textPart
	plain := func(t string) {
		if t == "" {
			return
		}
		if n := len(parts); n > 0 && !parts[n-1].Code {
			parts[n-1].Text += t
			return
		}
		parts = append(parts, textPart{Text: t})
	}
	for {
		open := strings.IndexByte(s, '`')
		if open < 0 {
			plain(s)
			return parts
		}
		end := strings.IndexByte(s[open+1:], '`')
		if end < 0 {
			plain(s)
			return parts
		}
		code := s[open+1 : open+1+end]
		if code == "" {
			plain(s[:open+2])
		} else {
			plain(s[:open])
			parts = append(parts, textPart{Text: code, Code: true})
		}
		s = s[open+2+end:]
	}
}

// plainText is s without the backticks of its code spans: the parts of
// splitCode joined. An unclosed backtick stays.
func plainText(s string) string {
	var b strings.Builder
	for _, t := range splitCode(s) {
		b.WriteString(t.Text)
	}
	return b.String()
}
