// The report of a work row. Its RESULT.md is read and parsed
// for the Report tab. The parser is block-level only; inline markup stays as
// written. Nothing here uses the UI. ReadReport blocks, so the caller runs it
// on a goroutine.
//
// Ported to Go from the Rust contr report module.

package board

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// MaxReportBytes is the largest part of a report that is read. A longer file
// shows this much and a "cut" line.
const MaxReportBytes = 256 * 1024

// MaxReportLines is the most lines drawn. More lines are dropped and Cut is
// set.
const MaxReportLines = 2000

// Problem lines. The UI shows them as written. The OS error text is not shown.
const (
	// BadReportPath is for a path that is not an absolute .md file path.
	BadReportPath = "report path is not an absolute .md file"
	// NoReportFile is for a report file that does not exist.
	NoReportFile = "no report file"
	// ReportUnreadable is for a report that exists but cannot be read.
	ReportUnreadable = "report unreadable"
	// NotAFile is for a path that names something other than a file.
	NotAFile = "report is not a file"
)

// LineKind is the draw kind of a ReportLine.
type LineKind uint8

const (
	// LineHeading is "#" to "######" and a space. Level is 1 to 6; Text has
	// no hashes and is trimmed.
	LineHeading LineKind = iota
	// LineItem is "- ", "* ", "+ " (Marker "•") or "<digits>. " and
	// "<digits>) " (Marker as written, for example "1."). Depth is leading
	// spaces / 2, at most 3; a tab counts as 4 spaces.
	LineItem
	// LineCode is a line in a ``` or ~~~ fence, or a table line (first
	// non-space char "|"). Verbatim, tabs to 4 spaces, trailing space
	// trimmed. Drawn without wrap.
	LineCode
	// LineQuote is "> text" (one level; a nested ">" stays in the text).
	LineQuote
	// LineRule is "---", "***" or "___" (3 or more of one char, spaces
	// allowed) outside a fence.
	LineRule
	// LineText is any other non-blank line, trimmed at the end.
	LineText
	// LineBlank is one blank line. Runs of blank lines collapse to one; no
	// blank at the start or end.
	LineBlank
)

// ReportLine is one drawn line of a report. Only the fields of its Kind are
// set. It is comparable.
type ReportLine struct {
	Kind   LineKind
	Level  uint8  // LineHeading
	Depth  uint8  // LineItem
	Marker string // LineItem
	Text   string // all kinds except LineRule and LineBlank
}

// ReportText is a parsed report.
type ReportText struct {
	Lines []ReportLine
	// Cut is set when the file was longer than MaxReportBytes or had more
	// than MaxReportLines lines.
	Cut bool
}

// drawn is the lines drawn so far. A full list refuses more lines and sets
// full.
type drawn struct {
	lines []ReportLine
	full  bool
}

// push adds a line. It returns false when the line does not fit; the report
// is then cut. A blank line is dropped, not counted, when it would start the
// list, follow another blank line, or when the list is full. A dropped blank
// never cuts: a blank at the end is removed anyway, and a line after it that
// does not fit still cuts.
func (d *drawn) push(line ReportLine) bool {
	if line.Kind == LineBlank {
		full := len(d.lines) >= MaxReportLines
		if full || len(d.lines) == 0 || d.lines[len(d.lines)-1].Kind == LineBlank {
			return true
		}
	}
	if len(d.lines) >= MaxReportLines {
		d.full = true
		return false
	}
	d.lines = append(d.lines, line)
	return true
}

// splitLines splits like Rust's str::lines: on "\n", a final "\r" of each
// line removed, and no empty last line after a closing "\n".
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	for i, p := range parts {
		parts[i] = strings.TrimSuffix(p, "\r")
	}
	return parts
}

func trimEnd(s string) string   { return strings.TrimRightFunc(s, unicode.IsSpace) }
func trimStart(s string) string { return strings.TrimLeftFunc(s, unicode.IsSpace) }
func trim(s string) string      { return strings.TrimFunc(s, unicode.IsSpace) }

// ParseReport parses markdown text into lines. Rules, in order, per line:
//
//  1. Inside a fence: a closing fence (same char, at least as long as the
//     opening fence, nothing after it) ends it (not drawn); else Code.
//  2. An HTML comment: a line that starts with "<!--" is hidden up to and
//     including the line that contains "-->" (same line allowed). Inside a
//     comment, a fence-like line does not open a fence.
//  3. An opening fence (``` or ~~~, info string ignored) starts one (not
//     drawn).
//  4. Blank gives Blank; then Rule, Heading, Item, Quote, table (Code), else
//     Text.
//
// An unclosed fence runs to the end. "\r\n" is accepted.
func ParseReport(text string) ReportText {
	var d drawn
	// The char and the length of the opening fence, while inside a fence.
	var fenceCh rune
	fenceLen := 0
	inFence := false
	inComment := false
	for _, raw := range splitLines(text) {
		line := trimEnd(strings.ReplaceAll(raw, "\t", "    "))
		trimmed := trimStart(line)
		var shown ReportLine
		visible := true
		switch {
		case inFence:
			if closesFence(trimmed, fenceCh, fenceLen) {
				inFence = false
				visible = false
			} else {
				shown = ReportLine{Kind: LineCode, Text: line}
			}
		case inComment:
			inComment = !strings.Contains(line, "-->")
			visible = false
		default:
			if ch, n, ok := opensFence(trimmed); ok {
				fenceCh, fenceLen, inFence = ch, n, true
				visible = false
			} else if rest, ok := strings.CutPrefix(trimmed, "<!--"); ok {
				inComment = !strings.Contains(rest, "-->")
				visible = false
			} else if trimmed == "" {
				shown = ReportLine{Kind: LineBlank}
			} else {
				shown = classify(line, trimmed)
			}
		}
		if !visible {
			continue
		}
		if !d.push(shown) {
			break
		}
	}
	if n := len(d.lines); n > 0 && d.lines[n-1].Kind == LineBlank {
		d.lines = d.lines[:n-1]
	}
	return ReportText{Lines: d.lines, Cut: d.full}
}

// opensFence reports the char and the length of an opening fence: three or
// more "`" or "~".
func opensFence(trimmed string) (rune, int, bool) {
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	ch := rune(trimmed[0])
	run := countRun(trimmed, ch)
	return ch, run, run >= 3
}

// closesFence reports a closing fence: the same char, at least as long as the
// opening fence, and nothing after it.
func closesFence(trimmed string, ch rune, open int) bool {
	run := countRun(trimmed, ch)
	return run >= open && trim(trimmed[run:]) == ""
}

// countRun counts the leading chars of s that are ch. ch is ASCII.
func countRun(s string, ch rune) int {
	n := 0
	for n < len(s) && rune(s[n]) == ch {
		n++
	}
	return n
}

// classify gives the draw kind of a line that is not blank, not hidden and
// not in a fence. line has tabs expanded and trailing space trimmed; trimmed
// is line without leading space.
func classify(line, trimmed string) ReportLine {
	if isRule(trimmed) {
		return ReportLine{Kind: LineRule}
	}
	if level, text, ok := heading(trimmed); ok {
		return ReportLine{Kind: LineHeading, Level: level, Text: text}
	}
	if it, ok := parseItem(line); ok {
		return it
	}
	if rest, ok := strings.CutPrefix(trimmed, ">"); ok {
		rest = strings.TrimPrefix(rest, " ")
		return ReportLine{Kind: LineQuote, Text: rest}
	}
	if strings.HasPrefix(trimmed, "|") {
		return ReportLine{Kind: LineCode, Text: line}
	}
	return ReportLine{Kind: LineText, Text: line}
}

// isRule reports "---", "***" or "___": three or more of one char, spaces
// allowed, and nothing else.
func isRule(trimmed string) bool {
	var marks []rune
	for _, c := range trimmed {
		if !unicode.IsSpace(c) {
			marks = append(marks, c)
		}
	}
	if len(marks) < 3 {
		return false
	}
	first := marks[0]
	if first != '-' && first != '*' && first != '_' {
		return false
	}
	for _, c := range marks {
		if c != first {
			return false
		}
	}
	return true
}

// heading parses "#" to "######" followed by one space. It returns the level
// and the trimmed text.
func heading(trimmed string) (uint8, string, bool) {
	level := countRun(trimmed, '#')
	if level < 1 || level > 6 {
		return 0, "", false
	}
	rest, ok := strings.CutPrefix(trimmed[level:], " ")
	if !ok {
		return 0, "", false
	}
	return uint8(level), trim(rest), true
}

// parseItem parses a bullet or a numbered item. line keeps its leading spaces,
// which set the depth.
func parseItem(line string) (ReportLine, bool) {
	body := strings.TrimLeft(line, " ")
	indent := len(line) - len(body)
	depth := uint8(min(indent/2, 3))
	for _, bullet := range []string{"-", "*", "+"} {
		if rest, ok := strings.CutPrefix(body, bullet); ok {
			if text, ok := strings.CutPrefix(rest, " "); ok {
				return ReportLine{Kind: LineItem, Depth: depth, Marker: "•", Text: trimStart(text)}, true
			}
		}
	}
	digits := 0
	for digits < len(body) && body[digits] >= '0' && body[digits] <= '9' {
		digits++
	}
	if digits < 1 || digits > 9 {
		return ReportLine{}, false
	}
	after := body[digits:]
	text, ok := strings.CutPrefix(after, ". ")
	if !ok {
		text, ok = strings.CutPrefix(after, ") ")
	}
	if !ok {
		return ReportLine{}, false
	}
	return ReportLine{Kind: LineItem, Depth: depth, Marker: body[:digits+1], Text: trimStart(text)}, true
}

// ReportPath gives the path to read for a row's report value. It reports true
// only when the path is absolute, has no NUL, and its extension is "md" (any
// case). No other check here; ReadReport checks the file.
func ReportPath(raw string) (string, bool) {
	if !filepath.IsAbs(raw) || strings.ContainsRune(raw, 0) {
		return "", false
	}
	base := filepath.Base(raw)
	ext := filepath.Ext(base)
	// A name like ".md" has no extension (a hidden file).
	if ext == base || !strings.EqualFold(ext, ".md") {
		return "", false
	}
	return raw, true
}

// ReadReport reads at most MaxReportBytes of path and parses it. It blocks:
// call it on a goroutine only. Errors are the fixed problem lines, never OS
// text: a non-file is NotAFile; a missing file is NoReportFile; any other
// error is ReportUnreadable. Invalid UTF-8 is replaced, never an error. A cut
// in the middle of a UTF-8 char drops the partial char.
func ReadReport(path string) (ReportText, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ReportText{}, unreadable(err)
	}
	if !info.Mode().IsRegular() {
		return ReportText{}, errors.New(NotAFile)
	}
	f, err := os.Open(path)
	if err != nil {
		return ReportText{}, unreadable(err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxReportBytes+1))
	if err != nil {
		return ReportText{}, unreadable(err)
	}
	cut := false
	if len(data) > MaxReportBytes {
		data = dropPartialChar(data[:MaxReportBytes])
		cut = true
	}
	report := ParseReport(lossy(data))
	report.Cut = report.Cut || cut
	return report, nil
}

// lossy converts bytes to a string and replaces each invalid byte with U+FFFD.
func lossy(b []byte) string {
	return string([]rune(string(b)))
}

// unreadable gives the fixed line for an error. A missing file has its own
// line; the other errors share one.
func unreadable(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New(NoReportFile)
	}
	return errors.New(ReportUnreadable)
}

// dropPartialChar drops the bytes of a UTF-8 char that a cut split: the lead
// byte and the continuation bytes after it, when the char needs more bytes
// than are left. A whole char is kept.
func dropPartialChar(b []byte) []byte {
	n := len(b)
	for back := 1; back <= min(n, 4); back++ {
		lead := b[n-back]
		if lead&0xC0 == 0x80 {
			continue
		}
		need := 1
		switch {
		case lead&0xE0 == 0xC0:
			need = 2
		case lead&0xF0 == 0xE0:
			need = 3
		case lead&0xF8 == 0xF0:
			need = 4
		}
		if need > back {
			return b[:n-back]
		}
		return b
	}
	return b
}
