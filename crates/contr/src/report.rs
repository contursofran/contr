//! The report of a work row: its RESULT.md, read and parsed for the Report tab. The parser is
//! block-level only; inline markup stays as written. Nothing here uses GPUI. `read_report` blocks,
//! so the caller runs it on the background executor.

use std::io::{self, Read};
use std::path::{Path, PathBuf};

/// The largest part of a report that is read. A longer file shows this much and a "cut" line.
pub const MAX_REPORT_BYTES: u64 = 256 * 1024;
/// The most lines drawn. More lines are dropped and `cut` is set.
pub const MAX_REPORT_LINES: usize = 2000;
/// The problem line for a path that is not an absolute `.md` file path.
pub const BAD_REPORT_PATH: &str = "report path is not an absolute .md file";
/// The problem line for a report file that does not exist.
pub const NO_REPORT_FILE: &str = "no report file";
/// The problem line for a report that exists but cannot be read. The OS error text is not shown.
pub const REPORT_UNREADABLE: &str = "report unreadable";
/// The problem line for a path that names something other than a file.
pub const NOT_A_FILE: &str = "report is not a file";

/// One drawn line of a report. Block-level markdown only; inline text stays as written.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ReportLine {
    /// `#`..`######` + space. `level` 1..=6; `text` without the hashes, trimmed.
    Heading { level: u8, text: String },
    /// `- `, `* `, `+ ` (marker "•") or `<digits>. ` / `<digits>) ` (marker as written, e.g. "1.").
    /// `depth` = leading spaces / 2, max 3 (a tab counts as 4 spaces).
    Item {
        depth: u8,
        marker: String,
        text: String,
    },
    /// A line inside a ``` or ~~~ fence, or a table line (first non-space char `|`). Verbatim,
    /// tabs to 4 spaces, trailing whitespace trimmed. Drawn without wrap.
    Code(String),
    /// `> text` (one level; nested `>` stays in the text).
    Quote(String),
    /// `---`, `***` or `___` (3 or more of one char, spaces allowed) outside a fence.
    Rule,
    /// Any other non-blank line, trimmed at the end.
    Text(String),
    /// One blank line. Runs of blank lines collapse to one; no Blank at the start or end.
    Blank,
}

/// A parsed report.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ReportText {
    pub lines: Vec<ReportLine>,
    /// The file was longer than MAX_REPORT_BYTES or had more than MAX_REPORT_LINES lines.
    pub cut: bool,
}

/// The lines drawn so far. A full list refuses more lines and sets `full`.
#[derive(Default)]
struct Drawn {
    lines: Vec<ReportLine>,
    full: bool,
}

impl Drawn {
    /// Adds a line. Returns `false` when the line does not fit; the report is then cut.
    /// A blank line is dropped, not counted, when it would start the list, follow another blank
    /// line, or when the list is full. A dropped blank never cuts: a blank at the end is removed
    /// anyway, and a line after it that does not fit still cuts.
    fn push(&mut self, line: ReportLine) -> bool {
        if matches!(line, ReportLine::Blank) {
            let full = self.lines.len() >= MAX_REPORT_LINES;
            if full || matches!(self.lines.last(), None | Some(ReportLine::Blank)) {
                return true;
            }
        }
        if self.lines.len() >= MAX_REPORT_LINES {
            self.full = true;
            return false;
        }
        self.lines.push(line);
        true
    }
}

/// Parses markdown text into lines. Rules, in order, per line:
/// 1. Inside a fence: a closing fence (same char, at least as long as the opening fence, nothing
///    after it) ends it (not drawn); else `Code`.
/// 2. An opening fence (```` ``` ```` or `~~~`, info string ignored) starts one (not drawn).
/// 3. An HTML comment: a line that starts with `<!--` is hidden up to and including the line
///    that contains `-->` (same line allowed).
/// 4. Blank → `Blank`; then Rule, Heading, Item, Quote, table → `Code`, else `Text`.
///
/// An unclosed fence runs to the end. `\r\n` is accepted.
pub fn parse_report(text: &str) -> ReportText {
    let mut drawn = Drawn::default();
    // The char and the length of the opening fence, while inside a fence.
    let mut fence: Option<(char, usize)> = None;
    let mut in_comment = false;
    for raw in text.lines() {
        let expanded = raw.replace('\t', "    ");
        let line = expanded.trim_end();
        let trimmed = line.trim_start();
        let shown = if let Some((ch, open)) = fence {
            if closes_fence(trimmed, ch, open) {
                fence = None;
                None
            } else {
                Some(ReportLine::Code(line.to_owned()))
            }
        } else if in_comment {
            // Checked before the opening fence on purpose: rule 3 hides a fence-like line inside a
            // comment. The Contract lists the fence first; this order is the intended one.
            in_comment = !line.contains("-->");
            None
        } else if let Some(opened) = opens_fence(trimmed) {
            fence = Some(opened);
            None
        } else if let Some(rest) = trimmed.strip_prefix("<!--") {
            in_comment = !rest.contains("-->");
            None
        } else if trimmed.is_empty() {
            Some(ReportLine::Blank)
        } else {
            Some(classify(line, trimmed))
        };
        let Some(shown) = shown else { continue };
        if !drawn.push(shown) {
            break;
        }
    }
    if matches!(drawn.lines.last(), Some(ReportLine::Blank)) {
        drawn.lines.pop();
    }
    ReportText {
        lines: drawn.lines,
        cut: drawn.full,
    }
}

/// The char and the length of an opening fence: three or more `` ` `` or `~`.
fn opens_fence(trimmed: &str) -> Option<(char, usize)> {
    let ch = trimmed.chars().next().filter(|&c| matches!(c, '`' | '~'))?;
    let run = trimmed.chars().take_while(|&c| c == ch).count();
    (run >= 3).then_some((ch, run))
}

/// A closing fence: the same char, at least as long as the opening fence, and nothing after it.
fn closes_fence(trimmed: &str, ch: char, open: usize) -> bool {
    let run = trimmed.chars().take_while(|&c| c == ch).count();
    run >= open && trimmed.chars().skip(run).all(char::is_whitespace)
}

/// The draw kind of a line that is not blank, not hidden and not in a fence.
/// `line` has tabs expanded and trailing space trimmed; `trimmed` is `line` without leading space.
fn classify(line: &str, trimmed: &str) -> ReportLine {
    if is_rule(trimmed) {
        return ReportLine::Rule;
    }
    if let Some((level, text)) = heading(trimmed) {
        return ReportLine::Heading {
            level,
            text: text.to_owned(),
        };
    }
    if let Some(item) = item(line) {
        return item;
    }
    if let Some(rest) = trimmed.strip_prefix('>') {
        return ReportLine::Quote(rest.strip_prefix(' ').unwrap_or(rest).to_owned());
    }
    if trimmed.starts_with('|') {
        return ReportLine::Code(line.to_owned());
    }
    ReportLine::Text(line.to_owned())
}

/// `---`, `***` or `___`: three or more of one char, spaces allowed, and nothing else.
fn is_rule(trimmed: &str) -> bool {
    let marks: Vec<char> = trimmed.chars().filter(|c| !c.is_whitespace()).collect();
    match marks.first() {
        Some(&first) => {
            marks.len() >= 3
                && matches!(first, '-' | '*' | '_')
                && marks.iter().all(|&c| c == first)
        }
        None => false,
    }
}

/// `#`..`######` followed by one space. Returns the level and the trimmed text.
fn heading(trimmed: &str) -> Option<(u8, &str)> {
    let rest = trimmed.trim_start_matches('#');
    let level = trimmed.len() - rest.len();
    if !(1..=6).contains(&level) {
        return None;
    }
    rest.strip_prefix(' ')
        .map(|text| (level as u8, text.trim()))
}

/// A bullet or a numbered item. `line` keeps its leading spaces, which set the depth.
fn item(line: &str) -> Option<ReportLine> {
    let body = line.trim_start_matches(' ');
    let indent = line.len() - body.len();
    let depth = (indent / 2).min(3) as u8;
    for bullet in ['-', '*', '+'] {
        if let Some(text) = body
            .strip_prefix(bullet)
            .and_then(|rest| rest.strip_prefix(' '))
        {
            return Some(ReportLine::Item {
                depth,
                marker: "•".to_owned(),
                text: text.trim_start().to_owned(),
            });
        }
    }
    let after = body.trim_start_matches(|c: char| c.is_ascii_digit());
    let digits = body.len() - after.len();
    if !(1..=9).contains(&digits) {
        return None;
    }
    let text = after
        .strip_prefix(". ")
        .or_else(|| after.strip_prefix(") "))?;
    let marker = body.get(..=digits)?;
    Some(ReportLine::Item {
        depth,
        marker: marker.to_owned(),
        text: text.trim_start().to_owned(),
    })
}

/// The path to read for a row's `report` value: `Some` only when it is absolute, has no NUL, and
/// its extension is `md` (any case). No other check here; the reader checks the file.
pub fn report_path(raw: &str) -> Option<PathBuf> {
    let path = Path::new(raw);
    let is_md = path
        .extension()
        .is_some_and(|ext| ext.eq_ignore_ascii_case("md"));
    if path.is_absolute() && is_md && !raw.contains('\0') {
        Some(path.to_path_buf())
    } else {
        None
    }
}

/// Reads at most MAX_REPORT_BYTES of `path` and parses it. Blocking: call it on the background
/// executor only. Errors are the panel's fixed lines, never OS text: a non-file is `NOT_A_FILE`; a
/// missing file is `NO_REPORT_FILE`; any other io error is `REPORT_UNREADABLE`. Invalid UTF-8 is
/// replaced (lossy), never an error. A cut in the middle of a UTF-8 char drops the partial char.
pub fn read_report(path: &Path) -> Result<ReportText, String> {
    let metadata = std::fs::metadata(path).map_err(|error| unreadable(&error))?;
    if !metadata.is_file() {
        return Err(NOT_A_FILE.to_owned());
    }
    let file = std::fs::File::open(path).map_err(|error| unreadable(&error))?;
    let mut bytes = Vec::new();
    file.take(MAX_REPORT_BYTES + 1)
        .read_to_end(&mut bytes)
        .map_err(|error| unreadable(&error))?;
    let mut cut = false;
    if bytes.len() as u64 > MAX_REPORT_BYTES {
        bytes.truncate(MAX_REPORT_BYTES as usize);
        drop_partial_char(&mut bytes);
        cut = true;
    }
    let mut report = parse_report(&String::from_utf8_lossy(&bytes));
    report.cut |= cut;
    Ok(report)
}

/// The panel's line for an io error. A missing file has its own line; the other errors share one.
/// The OS text is not shown (D75, as board.rs does).
fn unreadable(error: &io::Error) -> String {
    if error.kind() == io::ErrorKind::NotFound {
        NO_REPORT_FILE
    } else {
        REPORT_UNREADABLE
    }
    .to_owned()
}

/// Drops the bytes of a UTF-8 char that the cut split: the lead byte and the continuation bytes
/// after it, when the char needs more bytes than are left. A whole char is kept.
fn drop_partial_char(bytes: &mut Vec<u8>) {
    let len = bytes.len();
    for back in 1..=len.min(4) {
        let Some(&lead) = bytes.get(len - back) else {
            return;
        };
        if (lead & 0xC0) == 0x80 {
            continue;
        }
        let need = match lead {
            b if (b & 0xE0) == 0xC0 => 2,
            b if (b & 0xF0) == 0xE0 => 3,
            b if (b & 0xF8) == 0xF0 => 4,
            _ => 1,
        };
        if need > back {
            bytes.truncate(len - back);
        }
        return;
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;

    fn txt(s: &str) -> ReportLine {
        ReportLine::Text(s.to_owned())
    }

    fn code(s: &str) -> ReportLine {
        ReportLine::Code(s.to_owned())
    }

    fn head(level: u8, s: &str) -> ReportLine {
        ReportLine::Heading {
            level,
            text: s.to_owned(),
        }
    }

    fn bullet(depth: u8, s: &str) -> ReportLine {
        ReportLine::Item {
            depth,
            marker: "•".to_owned(),
            text: s.to_owned(),
        }
    }

    fn num(depth: u8, marker: &str, s: &str) -> ReportLine {
        ReportLine::Item {
            depth,
            marker: marker.to_owned(),
            text: s.to_owned(),
        }
    }

    fn lines(text: &str) -> Vec<ReportLine> {
        parse_report(text).lines
    }

    /// A file in a new temp dir. The dir is returned so it lives as long as the test.
    fn scratch_file(name: &str, bytes: &[u8]) -> (tempfile::TempDir, PathBuf) {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join(name);
        std::fs::write(&path, bytes).unwrap();
        (dir, path)
    }

    #[test]
    fn headings_strip_hashes_and_keep_level() {
        assert_eq!(
            lines("# One\n## Two\n### Three\n#### Four\n##### Five\n###### Six"),
            vec![
                head(1, "One"),
                head(2, "Two"),
                head(3, "Three"),
                head(4, "Four"),
                head(5, "Five"),
                head(6, "Six"),
            ]
        );
        assert_eq!(lines("#   Padded   "), vec![head(1, "Padded")]);
    }

    #[test]
    fn seven_hashes_or_no_space_is_text() {
        assert_eq!(lines("####### seven"), vec![txt("####### seven")]);
        assert_eq!(lines("#nospace"), vec![txt("#nospace")]);
        assert_eq!(lines("#"), vec![txt("#")]);
    }

    #[test]
    fn bullets_and_numbers_become_items_with_depth() {
        let text = "- a\n  * b\n    + c\n      - d\n        - e\n1. one\n2) two\n10) ten";
        assert_eq!(
            lines(text),
            vec![
                bullet(0, "a"),
                bullet(1, "b"),
                bullet(2, "c"),
                bullet(3, "d"),
                bullet(3, "e"),
                num(0, "1.", "one"),
                num(0, "2)", "two"),
                num(0, "10)", "ten"),
            ]
        );
        // More than 9 digits, or no space after the marker, is text.
        assert_eq!(
            lines("1234567890. too many\n1.no space"),
            vec![txt("1234567890. too many"), txt("1.no space")]
        );
    }

    #[test]
    fn tabs_count_as_four_spaces_and_depth_stops_at_three() {
        assert_eq!(
            lines("\t- a\n\t\t\t\t- deep"),
            vec![bullet(2, "a"), bullet(3, "deep")]
        );
    }

    #[test]
    fn a_fence_keeps_its_lines_verbatim_and_hides_the_fence() {
        let text = "before\n```rust\n  let x = 1;\tend  \n\n```\n~~~\nplain\n~~~\nafter";
        assert_eq!(
            lines(text),
            vec![
                txt("before"),
                code("  let x = 1;    end"),
                code(""),
                code("plain"),
                txt("after"),
            ]
        );
    }

    #[test]
    fn a_closing_fence_needs_the_same_char_and_length() {
        assert_eq!(
            lines("````\n```\nstill code\n````\nafter"),
            vec![code("```"), code("still code"), txt("after")]
        );
        assert_eq!(lines("```\nx\n~~~\n```"), vec![code("x"), code("~~~")]);
        assert_eq!(
            lines("```\na\n```rust\nb\n```"),
            vec![code("a"), code("```rust"), code("b")]
        );
    }

    #[test]
    fn an_unclosed_fence_runs_to_the_end() {
        assert_eq!(lines("```\na\n\nb"), vec![code("a"), code(""), code("b")]);
    }

    #[test]
    fn a_comment_line_is_hidden() {
        assert_eq!(
            lines("<!-- report 6efb743a -->\n# Title\nbody <!-- kept -->"),
            vec![head(1, "Title"), txt("body <!-- kept -->")]
        );
    }

    #[test]
    fn a_multi_line_comment_is_hidden_to_its_end() {
        assert_eq!(
            lines("<!--\nhidden # not a heading\n- not an item\n-->\nvisible"),
            vec![txt("visible")]
        );
        // A fence-like line inside a comment does not open a fence.
        assert_eq!(lines("<!--\n```\n-->\n# after"), vec![head(1, "after")]);
        assert_eq!(lines("a\n<!-- x -->\nb"), vec![txt("a"), txt("b")]);
    }

    #[test]
    fn blank_runs_collapse_and_edges_are_trimmed() {
        assert_eq!(
            lines("\n\n  \na\n\n\n\nb\n\n\n"),
            vec![txt("a"), ReportLine::Blank, txt("b")]
        );
        assert!(lines("\n \n").is_empty());
    }

    #[test]
    fn rules_quotes_and_tables() {
        let text = "a\n---\n- - -\n***\n___\n> one\n> > two\n|a|b|\n  |c|";
        assert_eq!(
            lines(text),
            vec![
                txt("a"),
                ReportLine::Rule,
                ReportLine::Rule,
                ReportLine::Rule,
                ReportLine::Rule,
                ReportLine::Quote("one".to_owned()),
                ReportLine::Quote("> two".to_owned()),
                code("|a|b|"),
                code("  |c|"),
            ]
        );
    }

    #[test]
    fn crlf_is_accepted() {
        assert_eq!(
            lines("# A\r\n- b\r\n\r\nc\r\n"),
            vec![head(1, "A"), bullet(0, "b"), ReportLine::Blank, txt("c")]
        );
    }

    #[test]
    fn inline_markdown_stays_as_written() {
        assert_eq!(
            lines("**bold** and `code` [x](y) _em_\n- **item** `x`"),
            vec![
                txt("**bold** and `code` [x](y) _em_"),
                bullet(0, "**item** `x`"),
            ]
        );
    }

    #[test]
    fn more_than_max_lines_is_cut() {
        let exact = "a\n".repeat(MAX_REPORT_LINES);
        let report = parse_report(&exact);
        assert_eq!(report.lines.len(), MAX_REPORT_LINES);
        assert!(!report.cut);

        let over = "a\n".repeat(MAX_REPORT_LINES + 1);
        let report = parse_report(&over);
        assert_eq!(report.lines.len(), MAX_REPORT_LINES);
        assert!(report.cut);

        // Hidden lines are not drawn, so they never cut the report.
        let hidden = "<!-- x -->\n".repeat(MAX_REPORT_LINES + 5);
        let report = parse_report(&hidden);
        assert!(report.lines.is_empty());
        assert!(!report.cut);
    }

    #[test]
    fn a_trailing_blank_after_a_full_list_is_not_a_cut() {
        // Exactly MAX lines, then one blank line and nothing more: the blank is removed, not cut.
        let full = format!("{}\n", "a\n".repeat(MAX_REPORT_LINES));
        let report = parse_report(&full);
        assert_eq!(report.lines.len(), MAX_REPORT_LINES);
        assert!(!report.cut);

        // The same list, then a blank and a line: that line does not fit, so the report is cut.
        let more = format!("{}\nb", "a\n".repeat(MAX_REPORT_LINES));
        let report = parse_report(&more);
        assert_eq!(report.lines.len(), MAX_REPORT_LINES);
        assert!(report.cut);
    }

    #[test]
    fn report_path_needs_an_absolute_md_path() {
        assert_eq!(
            report_path("/example/b-1151/RESULT.md"),
            Some(PathBuf::from("/example/b-1151/RESULT.md"))
        );
        assert!(report_path("/example/b-1151/RESULT.MD").is_some());
        assert_eq!(report_path("RESULT.md"), None);
        assert_eq!(report_path("example/RESULT.md"), None);
        assert_eq!(report_path("/example/RESULT.txt"), None);
        assert_eq!(report_path("/example/RESULT"), None);
        assert_eq!(report_path("/example/a\0b.md"), None);
        assert_eq!(report_path(""), None);
    }

    #[test]
    fn read_report_parses_a_file() {
        let (_dir, path) = scratch_file("RESULT.md", b"# T\n- x\n");
        let report = read_report(&path).unwrap();
        assert_eq!(report.lines, vec![head(1, "T"), bullet(0, "x")]);
        assert!(!report.cut);
    }

    #[test]
    fn an_empty_file_has_no_lines() {
        let (_dir, path) = scratch_file("RESULT.md", b"");
        let report = read_report(&path).unwrap();
        assert!(report.lines.is_empty());
        assert!(!report.cut);
    }

    #[test]
    fn read_report_cuts_a_long_file_at_a_char_boundary() {
        // MAX - 1 ASCII bytes, then "é" (2 bytes): the cut falls inside the "é".
        let mut bytes = vec![b'a'; (MAX_REPORT_BYTES - 1) as usize];
        bytes.extend_from_slice("é".as_bytes());
        let (_dir, path) = scratch_file("RESULT.md", &bytes);
        let report = read_report(&path).unwrap();
        assert!(report.cut);
        let Some(ReportLine::Text(last)) = report.lines.last() else {
            panic!("the long line should be text");
        };
        assert_eq!(last.len() as u64, MAX_REPORT_BYTES - 1);
        assert!(!last.contains('\u{FFFD}'));
    }

    #[test]
    fn read_report_replaces_invalid_utf8() {
        let (_dir, path) = scratch_file("RESULT.md", b"# ok\n\xff\xfe end\n");
        let report = read_report(&path).unwrap();
        assert!(!report.cut);
        assert_eq!(report.lines.len(), 2);
        let ReportLine::Text(line) = &report.lines[1] else {
            panic!("the invalid line should be text");
        };
        assert!(line.contains('\u{FFFD}'));
        assert!(line.ends_with("end"));
    }

    #[test]
    fn read_report_reports_a_missing_file() {
        let dir = tempfile::tempdir().unwrap();
        assert_eq!(
            read_report(&dir.path().join("missing.md")),
            Err(NO_REPORT_FILE.to_owned())
        );
    }

    #[test]
    fn read_report_reports_another_io_error_without_the_os_text() {
        // A path through a regular file fails with "not a directory", which is not "not found".
        let (_dir, file) = scratch_file("RESULT.txt", b"x");
        let through = file.join("RESULT.md");
        assert_eq!(read_report(&through), Err(REPORT_UNREADABLE.to_owned()));
    }

    #[test]
    fn read_report_refuses_a_directory() {
        let dir = tempfile::tempdir().unwrap();
        let sub = dir.path().join("RESULT.md");
        std::fs::create_dir(&sub).unwrap();
        assert_eq!(read_report(&sub), Err(NOT_A_FILE.to_owned()));
    }

    #[test]
    fn a_real_result_md_parses() {
        let report = parse_report(include_str!("report_fixture.md"));
        assert!(!report.cut);
        assert_eq!(report.lines[0], head(2, "Result: sort bags (#640)"));
        let shows_comment = report
            .lines
            .iter()
            .any(|line| matches!(line, ReportLine::Text(t) if t.contains("report 6efb743a")));
        assert!(!shows_comment);
        assert!(report.lines.contains(&bullet(
            0,
            "Added `sort_bags` in `/example/repo/src/bags.rs`."
        )));
        assert!(report.lines.contains(&bullet(1, "The sort is stable.")));
        assert!(report.lines.contains(&txt(
            "DECISION: Keep ties in input order? | yes (recommended) | no"
        )));
        assert!(
            report
                .lines
                .contains(&code("fn sort_bags(bags: &mut [Bag]) {"))
        );
        assert!(report.lines.contains(&code("| Bag | Weight |")));
        assert!(report.lines.contains(&num(0, "1.", "Read the spec.")));
        assert!(report.lines.contains(&num(0, "2)", "Wrote the code.")));
        assert!(report.lines.contains(&ReportLine::Rule));
        assert!(report.lines.contains(&ReportLine::Quote(
            "Note: the table is for the owner.".to_owned()
        )));
        assert_eq!(report.lines.last(), Some(&bullet(0, "Open the PR.")));
    }

    #[test]
    fn a_cut_drops_only_an_incomplete_char() {
        // "ab" + the first 2 of 3 bytes of "€": the partial char goes.
        let mut bytes = "ab€".as_bytes().to_vec();
        bytes.truncate(4);
        drop_partial_char(&mut bytes);
        assert_eq!(bytes, b"ab");

        // A whole char stays.
        let mut bytes = "ab€".as_bytes().to_vec();
        drop_partial_char(&mut bytes);
        assert_eq!(bytes, "ab€".as_bytes());

        // The first 3 of 4 bytes of an emoji: the partial char goes.
        let mut bytes = "a😀".as_bytes().to_vec();
        bytes.truncate(3);
        drop_partial_char(&mut bytes);
        assert_eq!(bytes, b"a");

        // A lone lead byte of a 2-byte char goes.
        let mut bytes = "é".as_bytes()[..1].to_vec();
        drop_partial_char(&mut bytes);
        assert!(bytes.is_empty());
    }
}
