package board

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func txt(s string) ReportLine  { return ReportLine{Kind: LineText, Text: s} }
func code(s string) ReportLine { return ReportLine{Kind: LineCode, Text: s} }
func head(level uint8, s string) ReportLine {
	return ReportLine{Kind: LineHeading, Level: level, Text: s}
}
func bullet(depth uint8, s string) ReportLine {
	return ReportLine{Kind: LineItem, Depth: depth, Marker: "•", Text: s}
}
func num(depth uint8, marker, s string) ReportLine {
	return ReportLine{Kind: LineItem, Depth: depth, Marker: marker, Text: s}
}

var (
	rule  = ReportLine{Kind: LineRule}
	blank = ReportLine{Kind: LineBlank}
)

func quote(s string) ReportLine { return ReportLine{Kind: LineQuote, Text: s} }

func lines(text string) []ReportLine { return ParseReport(text).Lines }

func wantLines(t *testing.T, got, want []ReportLine) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func scratchFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func contains(ls []ReportLine, want ReportLine) bool {
	for _, l := range ls {
		if l == want {
			return true
		}
	}
	return false
}

func TestHeadingsStripHashesAndKeepLevel(t *testing.T) {
	eq(t, lines("# One\n## Two\n### Three\n#### Four\n##### Five\n###### Six"), []ReportLine{
		head(1, "One"), head(2, "Two"), head(3, "Three"),
		head(4, "Four"), head(5, "Five"), head(6, "Six"),
	})
	eq(t, lines("#   Padded   "), []ReportLine{head(1, "Padded")})
}

func TestSevenHashesOrNoSpaceIsText(t *testing.T) {
	eq(t, lines("####### seven"), []ReportLine{txt("####### seven")})
	eq(t, lines("#nospace"), []ReportLine{txt("#nospace")})
	eq(t, lines("#"), []ReportLine{txt("#")})
}

func TestBulletsAndNumbersBecomeItemsWithDepth(t *testing.T) {
	text := "- a\n  * b\n    + c\n      - d\n        - e\n1. one\n2) two\n10) ten"
	eq(t, lines(text), []ReportLine{
		bullet(0, "a"), bullet(1, "b"), bullet(2, "c"), bullet(3, "d"), bullet(3, "e"),
		num(0, "1.", "one"), num(0, "2)", "two"), num(0, "10)", "ten"),
	})
	// More than 9 digits, or no space after the marker, is text.
	eq(t, lines("1234567890. too many\n1.no space"),
		[]ReportLine{txt("1234567890. too many"), txt("1.no space")})
}

func TestTabsCountAsFourSpacesAndDepthStopsAtThree(t *testing.T) {
	eq(t, lines("\t- a\n\t\t\t\t- deep"), []ReportLine{bullet(2, "a"), bullet(3, "deep")})
}

func TestAFenceKeepsItsLinesVerbatimAndHidesTheFence(t *testing.T) {
	text := "before\n```rust\n  let x = 1;\tend  \n\n```\n~~~\nplain\n~~~\nafter"
	eq(t, lines(text), []ReportLine{
		txt("before"), code("  let x = 1;    end"), code(""), code("plain"), txt("after"),
	})
}

func TestAClosingFenceNeedsTheSameCharAndLength(t *testing.T) {
	eq(t, lines("````\n```\nstill code\n````\nafter"),
		[]ReportLine{code("```"), code("still code"), txt("after")})
	eq(t, lines("```\nx\n~~~\n```"), []ReportLine{code("x"), code("~~~")})
	eq(t, lines("```\na\n```rust\nb\n```"),
		[]ReportLine{code("a"), code("```rust"), code("b")})
}

func TestAnUnclosedFenceRunsToTheEnd(t *testing.T) {
	eq(t, lines("```\na\n\nb"), []ReportLine{code("a"), code(""), code("b")})
}

func TestACommentLineIsHidden(t *testing.T) {
	eq(t, lines("<!-- report 6efb743a -->\n# Title\nbody <!-- kept -->"),
		[]ReportLine{head(1, "Title"), txt("body <!-- kept -->")})
}

func TestAMultiLineCommentIsHiddenToItsEnd(t *testing.T) {
	eq(t, lines("<!--\nhidden # not a heading\n- not an item\n-->\nvisible"),
		[]ReportLine{txt("visible")})
	// A fence-like line inside a comment does not open a fence.
	eq(t, lines("<!--\n```\n-->\n# after"), []ReportLine{head(1, "after")})
	eq(t, lines("a\n<!-- x -->\nb"), []ReportLine{txt("a"), txt("b")})
}

func TestBlankRunsCollapseAndEdgesAreTrimmed(t *testing.T) {
	eq(t, lines("\n\n  \na\n\n\n\nb\n\n\n"), []ReportLine{txt("a"), blank, txt("b")})
	if got := lines("\n \n"); len(got) != 0 {
		t.Fatalf("want no lines, got %+v", got)
	}
}

func TestRulesQuotesAndTables(t *testing.T) {
	text := "a\n---\n- - -\n***\n___\n> one\n> > two\n|a|b|\n  |c|"
	eq(t, lines(text), []ReportLine{
		txt("a"), rule, rule, rule, rule,
		quote("one"), quote("> two"), code("|a|b|"), code("  |c|"),
	})
}

func TestCrlfIsAccepted(t *testing.T) {
	eq(t, lines("# A\r\n- b\r\n\r\nc\r\n"),
		[]ReportLine{head(1, "A"), bullet(0, "b"), blank, txt("c")})
}

func TestInlineMarkdownStaysAsWritten(t *testing.T) {
	eq(t, lines("**bold** and `code` [x](y) _em_\n- **item** `x`"), []ReportLine{
		txt("**bold** and `code` [x](y) _em_"), bullet(0, "**item** `x`"),
	})
}

func TestMoreThanMaxLinesIsCut(t *testing.T) {
	r := ParseReport(strings.Repeat("a\n", MaxReportLines))
	if len(r.Lines) != MaxReportLines || r.Cut {
		t.Fatalf("exact: %d lines, cut %v", len(r.Lines), r.Cut)
	}
	r = ParseReport(strings.Repeat("a\n", MaxReportLines+1))
	if len(r.Lines) != MaxReportLines || !r.Cut {
		t.Fatalf("over: %d lines, cut %v", len(r.Lines), r.Cut)
	}
	// Hidden lines are not drawn, so they never cut the report.
	r = ParseReport(strings.Repeat("<!-- x -->\n", MaxReportLines+5))
	if len(r.Lines) != 0 || r.Cut {
		t.Fatalf("hidden: %d lines, cut %v", len(r.Lines), r.Cut)
	}
}

func TestATrailingBlankAfterAFullListIsNotACut(t *testing.T) {
	r := ParseReport(strings.Repeat("a\n", MaxReportLines) + "\n")
	if len(r.Lines) != MaxReportLines || r.Cut {
		t.Fatalf("full: %d lines, cut %v", len(r.Lines), r.Cut)
	}
	r = ParseReport(strings.Repeat("a\n", MaxReportLines) + "\nb")
	if len(r.Lines) != MaxReportLines || !r.Cut {
		t.Fatalf("more: %d lines, cut %v", len(r.Lines), r.Cut)
	}
}

func TestReportPathNeedsAnAbsoluteMdPath(t *testing.T) {
	if p, ok := ReportPath("/example/b-1151/RESULT.md"); !ok || p != "/example/b-1151/RESULT.md" {
		t.Fatalf("got %q %v", p, ok)
	}
	if _, ok := ReportPath("/example/b-1151/RESULT.MD"); !ok {
		t.Fatal("upper-case extension should pass")
	}
	for _, bad := range []string{
		"RESULT.md", "example/RESULT.md", "/example/RESULT.txt",
		"/example/RESULT", "/example/a\x00b.md", "",
	} {
		if p, ok := ReportPath(bad); ok {
			t.Fatalf("%q should fail, got %q", bad, p)
		}
	}
}

func TestReadReportParsesAFile(t *testing.T) {
	path := scratchFile(t, "RESULT.md", []byte("# T\n- x\n"))
	r, err := ReadReport(path)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, r.Lines, []ReportLine{head(1, "T"), bullet(0, "x")})
	if r.Cut {
		t.Fatal("should not be cut")
	}
}

func TestAnEmptyFileHasNoLines(t *testing.T) {
	r, err := ReadReport(scratchFile(t, "RESULT.md", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Lines) != 0 || r.Cut {
		t.Fatalf("got %+v", r)
	}
}

func TestReadReportCutsALongFileAtACharBoundary(t *testing.T) {
	// MAX - 1 ASCII bytes, then "é" (2 bytes): the cut falls inside the "é".
	data := append(bytes.Repeat([]byte("a"), MaxReportBytes-1), []byte("é")...)
	r, err := ReadReport(scratchFile(t, "RESULT.md", data))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Cut {
		t.Fatal("should be cut")
	}
	last := r.Lines[len(r.Lines)-1]
	if last.Kind != LineText {
		t.Fatalf("the long line should be text, got %+v", last.Kind)
	}
	if len(last.Text) != MaxReportBytes-1 {
		t.Fatalf("len %d", len(last.Text))
	}
	if strings.ContainsRune(last.Text, '�') {
		t.Fatal("partial char should be dropped, not replaced")
	}
}

func TestReadReportReplacesInvalidUtf8(t *testing.T) {
	r, err := ReadReport(scratchFile(t, "RESULT.md", []byte("# ok\n\xff\xfe end\n")))
	if err != nil {
		t.Fatal(err)
	}
	if r.Cut || len(r.Lines) != 2 {
		t.Fatalf("got %+v", r)
	}
	line := r.Lines[1]
	if line.Kind != LineText {
		t.Fatalf("the invalid line should be text, got %+v", line.Kind)
	}
	if !strings.ContainsRune(line.Text, '�') || !strings.HasSuffix(line.Text, "end") {
		t.Fatalf("got %q", line.Text)
	}
}

func TestReadReportReportsAMissingFile(t *testing.T) {
	_, err := ReadReport(filepath.Join(t.TempDir(), "missing.md"))
	if err == nil || err.Error() != NoReportFile {
		t.Fatalf("got %v", err)
	}
}

func TestReadReportReportsAnotherIoErrorWithoutTheOsText(t *testing.T) {
	// A path through a regular file fails with "not a directory", which is
	// not "not found".
	file := scratchFile(t, "RESULT.txt", []byte("x"))
	_, err := ReadReport(filepath.Join(file, "RESULT.md"))
	if err == nil || err.Error() != ReportUnreadable {
		t.Fatalf("got %v", err)
	}
}

func TestReadReportRefusesADirectory(t *testing.T) {
	sub := filepath.Join(t.TempDir(), "RESULT.md")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := ReadReport(sub)
	if err == nil || err.Error() != NotAFile {
		t.Fatalf("got %v", err)
	}
}

func TestARealResultMdParses(t *testing.T) {
	data, err := os.ReadFile("testdata/report_fixture.md")
	if err != nil {
		t.Fatal(err)
	}
	r := ParseReport(string(data))
	if r.Cut {
		t.Fatal("should not be cut")
	}
	if r.Lines[0] != head(2, "Result: sort bags (#640)") {
		t.Fatalf("first line %+v", r.Lines[0])
	}
	for _, l := range r.Lines {
		if l.Kind == LineText && strings.Contains(l.Text, "report 6efb743a") {
			t.Fatal("the report comment should be hidden")
		}
	}
	for _, want := range []ReportLine{
		bullet(0, "Added `sort_bags` in `/example/repo/src/bags.rs`."),
		bullet(1, "The sort is stable."),
		txt("DECISION: Keep ties in input order? | yes (recommended) | no"),
		code("fn sort_bags(bags: &mut [Bag]) {"),
		code("| Bag | Weight |"),
		num(0, "1.", "Read the spec."),
		num(0, "2)", "Wrote the code."),
		rule,
		quote("Note: the table is for the owner."),
	} {
		if !contains(r.Lines, want) {
			t.Fatalf("missing %+v", want)
		}
	}
	if r.Lines[len(r.Lines)-1] != bullet(0, "Open the PR.") {
		t.Fatalf("last line %+v", r.Lines[len(r.Lines)-1])
	}
}

func TestACutDropsOnlyAnIncompleteChar(t *testing.T) {
	// "ab" + the first 2 of 3 bytes of "€": the partial char goes.
	if got := dropPartialChar([]byte("ab€")[:4]); string(got) != "ab" {
		t.Fatalf("got %q", got)
	}
	// A whole char stays.
	if got := dropPartialChar([]byte("ab€")); string(got) != "ab€" {
		t.Fatalf("got %q", got)
	}
	// The first 3 of 4 bytes of an emoji: the partial char goes.
	if got := dropPartialChar([]byte("a😀")[:3]); string(got) != "a" {
		t.Fatalf("got %q", got)
	}
	// A lone lead byte of a 2-byte char goes.
	if got := dropPartialChar([]byte("é")[:1]); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}
