//! The Report tab's view: the RESULT.md of the selected work row, drawn one block per line. The file
//! is read on the background executor. A read starts only when the row's path or the board revision
//! changes. Nothing here polls, sets a timer or watches the file. Only the finished read reaches the
//! view.

use crate::board_model::WorkRow;
use crate::report::{BAD_REPORT_PATH, ReportLine, ReportText, read_report, report_path};
use crate::theme::*;
use gpui::{
    App, ClickEvent, Context, Div, EventEmitter, FontWeight, IntoElement, ParentElement, Render,
    Role, Stateful, Styled, Task, Window, div, prelude::*, px, rgb,
};
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::Arc;

/// The line when no row is picked for the report.
const NO_ROW: &str = "Pick a work row to see its report.";
/// The line when the picked row is no longer on the board.
const ROW_GONE: &str = "That row is no longer on the board.";
/// The line when the picked row has no report yet.
const NO_REPORT: &str = "No report yet.";
/// The line while a read is in flight and no text is shown.
const LOADING: &str = "loading report…";
/// The line when the report has no lines.
const EMPTY_REPORT: &str = "The report is empty.";
/// The line after a report that was cut.
const REPORT_CUT: &str = "report cut · Open in editor for the rest";
/// The label of the action that opens the file.
const OPEN_IN_EDITOR: &str = "Open in editor";
/// The label of the action that returns to the board.
const BACK: &str = "back";

/// Opens a file with the system's default app. The app uses `open_in_system`; tests record paths.
pub type OpenPathFn = Rc<dyn Fn(PathBuf, &mut App)>;

/// Opens `path` with the system's default app for its type.
pub fn open_in_system(path: PathBuf, cx: &mut App) {
    cx.open_with_system(&path);
}

/// What the Report tab shows, built by BoardPanel from the board.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ReportTarget {
    /// No row is chosen for the report.
    None,
    /// The chosen row key is no longer on the board.
    Gone,
    Row(ReportSource),
}

/// The fields of a work row that the Report tab shows.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ReportSource {
    pub key: String,
    pub label: String,
    pub title: String,
    pub report: Option<String>,
    pub report_by: Option<String>,
}

impl ReportSource {
    pub fn from_row(row: &WorkRow) -> Self {
        Self {
            key: row.key.clone(),
            label: row.label.clone(),
            title: row.title.clone(),
            report: row.report.clone(),
            report_by: row.report_by.clone(),
        }
    }
}

/// What the body shows.
#[derive(Debug, Clone, PartialEq)]
pub enum ReportBody {
    /// A fixed line.
    Empty(&'static str),
    Loading,
    /// The first read failed: the error line.
    Failed(String),
    /// The text of the file. `problem` is set when a re-read failed; the old text is kept.
    Text {
        text: Arc<ReportText>,
        problem: Option<String>,
    },
}

/// What the view tells its parent.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ReportEvent {
    /// The owner chose `back`.
    Back,
}

impl EventEmitter<ReportEvent> for ReportView {}

/// The Report tab's view. The parent sets what it shows with `show`.
pub struct ReportView {
    target: ReportTarget,
    body: ReportBody,
    /// The path and revision of the last read started. The same key starts no new read.
    read_key: Option<(PathBuf, u64)>,
    /// The read in flight. A newer read drops the older one.
    reading: Option<Task<()>>,
    open: OpenPathFn,
    /// Reads started. Tests count them.
    #[cfg(test)]
    pub reads: usize,
}

impl ReportView {
    /// Opens files with the system's default app.
    pub fn new(cx: &mut Context<Self>) -> Self {
        Self::with_open(Rc::new(open_in_system), cx)
    }

    /// `new` with the open call given, so the tests can record the paths that are opened.
    pub(crate) fn with_open(open: OpenPathFn, _cx: &mut Context<Self>) -> Self {
        Self {
            target: ReportTarget::None,
            body: ReportBody::Empty(NO_ROW),
            read_key: None,
            reading: None,
            open,
            #[cfg(test)]
            reads: 0,
        }
    }

    /// Shows `target` for board revision `rev`. Starts a background read only when the valid path
    /// or `rev` differs from `read_key`. Notifies when anything shown changes.
    pub fn show(&mut self, target: ReportTarget, rev: u64, cx: &mut Context<Self>) {
        let before = self.shown();
        match readable_path(&target) {
            Some(path) => {
                self.target = target;
                self.follow(path, rev, cx);
            }
            None => {
                self.body = idle_body(&target);
                self.read_key = None;
                self.reading = None;
                self.target = target;
            }
        }
        if self.shown() != before {
            cx.notify();
        }
    }

    /// Shows the file at `path` for revision `rev`. A read starts only when the path or the revision
    /// differs from the last read. The text of the same path stays on screen while the new read runs.
    fn follow(&mut self, path: PathBuf, rev: u64, cx: &mut Context<Self>) {
        let key = (path, rev);
        if self.read_key.as_ref() == Some(&key) {
            return;
        }
        let same_path = self
            .read_key
            .as_ref()
            .is_some_and(|(shown, _)| *shown == key.0);
        let keep_text = same_path && matches!(self.body, ReportBody::Text { .. });
        if !keep_text {
            self.body = ReportBody::Loading;
        }
        #[cfg(test)]
        {
            self.reads += 1;
        }
        self.read_key = Some(key.clone());
        self.reading = Some(cx.spawn(async move |this, cx| {
            let path = key.0.clone();
            let result = cx
                .background_executor()
                .spawn(async move { read_report(&path) })
                .await;
            let _ = this.update(cx, |view, cx| view.loaded(key, result, cx));
        }));
    }

    /// Applies a finished read. A result for a read that is no longer the current one is dropped.
    fn loaded(
        &mut self,
        key: (PathBuf, u64),
        result: Result<ReportText, String>,
        cx: &mut Context<Self>,
    ) {
        if self.read_key.as_ref() != Some(&key) {
            return;
        }
        let before = self.shown();
        self.body = match result {
            Ok(text) if text.lines.is_empty() => ReportBody::Empty(EMPTY_REPORT),
            Ok(text) => ReportBody::Text {
                text: Arc::new(text),
                problem: None,
            },
            // A text body is always for the path of the current read, so it stays.
            Err(line) => match &self.body {
                ReportBody::Text { text, .. } => ReportBody::Text {
                    text: text.clone(),
                    problem: Some(line),
                },
                _ => ReportBody::Failed(line),
            },
        };
        if self.shown() != before {
            cx.notify();
        }
    }

    /// What the view shows: the target and the body. A change in either one notifies.
    fn shown(&self) -> (ReportTarget, ReportBody) {
        (self.target.clone(), self.body.clone())
    }

    /// Opens the shown file with the app for its type. Does nothing without a valid path.
    fn open_in_editor(&mut self, cx: &mut Context<Self>) {
        if let Some(path) = readable_path(&self.target) {
            (self.open)(path, cx);
        }
    }
}

/// The path to read for `target`: a row's `report` when it is a valid path.
fn readable_path(target: &ReportTarget) -> Option<PathBuf> {
    match target {
        ReportTarget::Row(source) => source.report.as_deref().and_then(report_path),
        ReportTarget::None | ReportTarget::Gone => None,
    }
}

/// What the body shows for a `target` with no file to read.
fn idle_body(target: &ReportTarget) -> ReportBody {
    match target {
        ReportTarget::None => ReportBody::Empty(NO_ROW),
        ReportTarget::Gone => ReportBody::Empty(ROW_GONE),
        ReportTarget::Row(source) => match source.report {
            None => ReportBody::Empty(NO_REPORT),
            // Only a report path that `report_path` rejects gets here.
            Some(_) => ReportBody::Failed(BAD_REPORT_PATH.to_owned()),
        },
    }
}

/// The title: the label, two spaces, then the title. A part that is empty is left out.
fn title_text(source: &ReportSource) -> String {
    [source.label.as_str(), source.title.as_str()]
        .into_iter()
        .filter(|part| !part.is_empty())
        .collect::<Vec<_>>()
        .join("  ")
}

/// The accessible name of the view: `report {label} {title}, by {report_by}`, with the missing
/// parts left out.
fn aria_label(target: &ReportTarget) -> String {
    let ReportTarget::Row(source) = target else {
        return "report".to_owned();
    };
    let mut name = "report".to_owned();
    for part in [source.label.as_str(), source.title.as_str()] {
        if !part.is_empty() {
            name.push(' ');
            name.push_str(part);
        }
    }
    if let Some(by) = &source.report_by {
        name.push_str(", by ");
        name.push_str(by);
    }
    name
}

impl Render for ReportView {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let open = readable_path(&self.target).is_some();
        let by = match &self.target {
            ReportTarget::Row(source) => source.report_by.clone(),
            ReportTarget::None | ReportTarget::Gone => None,
        };
        let problem = match &self.body {
            ReportBody::Text { problem, .. } => problem.clone(),
            ReportBody::Empty(_) | ReportBody::Loading | ReportBody::Failed(_) => None,
        };
        div()
            .id("report")
            .debug_selector(|| "report".into())
            .role(Role::Group)
            .aria_label(aria_label(&self.target))
            .size_full()
            .flex()
            .flex_col()
            .px(px(PANEL_PAD_X))
            .child(self.head(open, cx))
            .children(meta_line(by, problem))
            .child(div().h(px(1.)).flex_none().bg(rgb(LINE)))
            .child(body(&self.body))
    }
}

impl ReportView {
    /// The title on the left; `Open in editor` (when there is a valid path) and `back` on the right.
    fn head(&self, open: bool, cx: &mut Context<Self>) -> Div {
        // Without a row the title is an empty spacer, so `back` stays at the right edge.
        let title = match &self.target {
            ReportTarget::Row(source) => div()
                .debug_selector(|| "report-title".into())
                .child(title_text(source)),
            ReportTarget::None | ReportTarget::Gone => div(),
        };
        let title = title
            .flex_1()
            .min_w_0()
            .overflow_hidden()
            .text_ellipsis()
            .whitespace_nowrap()
            .font_weight(FontWeight::BOLD)
            .text_color(rgb(TEXT));
        let open_action = open.then(|| {
            action(
                "report-open",
                OPEN_IN_EDITOR,
                cx.listener(|this, _: &ClickEvent, _, cx| this.open_in_editor(cx)),
            )
        });
        let back = action(
            "report-back",
            BACK,
            cx.listener(|_, _: &ClickEvent, _, cx| cx.emit(ReportEvent::Back)),
        );
        div()
            .debug_selector(|| "report-head".into())
            .h(px(BOARD_LINE_HEIGHT))
            .flex_none()
            .flex()
            .items_center()
            .gap(px(REPORT_GAP))
            .child(title)
            .child(
                div()
                    .flex_none()
                    .flex()
                    .items_center()
                    .gap(px(REPORT_GAP))
                    .children(open_action)
                    .child(back),
            )
    }
}

/// A text action in the head: dim, lit on hover, and a button for the reader.
fn action(
    selector: &'static str,
    label: &'static str,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    div()
        .id(selector)
        .debug_selector(move || selector.to_owned())
        .role(Role::Button)
        .aria_label(label)
        .text_color(rgb(TEXT_DIM))
        .hover(|style| style.text_color(rgb(TEXT)))
        .cursor_pointer()
        .on_click(on_click)
        .child(label)
}

/// The line under the head: `by {report_by}`, then ` · {problem}` when a re-read failed. Absent when
/// there is neither.
fn meta_line(by: Option<String>, problem: Option<String>) -> Option<Div> {
    if by.is_none() && problem.is_none() {
        return None;
    }
    let mut line = div()
        .debug_selector(|| "report-meta".into())
        .h(px(BOARD_LINE_HEIGHT))
        .flex_none()
        .flex()
        .items_center()
        .text_color(rgb(TEXT_DIM));
    let has_by = by.is_some();
    if let Some(by) = by {
        line = line.child(format!("by {by}"));
    }
    if let Some(problem) = problem {
        if has_by {
            line = line.child(" · ");
        }
        line = line.child(
            div()
                .debug_selector(|| "report-problem".into())
                .child(problem),
        );
    }
    Some(line)
}

/// The body: the drawn lines in a scrolling column, or the one line that replaces them.
fn body(shown: &ReportBody) -> Stateful<Div> {
    let column = div()
        .id("report-body")
        .debug_selector(|| "report-body".into())
        .flex_1()
        .min_h_0()
        .overflow_y_scroll()
        .pt(px(REPORT_BODY_PAD_TOP))
        .flex()
        .flex_col();
    match shown {
        ReportBody::Text { text, .. } => {
            let column = column.children(
                text.lines
                    .iter()
                    .enumerate()
                    .map(|(index, line)| draw_line(index, line)),
            );
            if text.cut {
                column.child(note("report-cut", REPORT_CUT, TEXT_DIM_2))
            } else {
                column
            }
        }
        ReportBody::Empty(line) => column.child(note("report-empty", line, TEXT_DIM)),
        ReportBody::Loading => column.child(note("report-empty", LOADING, TEXT_DIM)),
        ReportBody::Failed(line) => column.child(note("report-empty", line, TEXT_DIM)),
    }
}

/// One line of the body, one block per line. The wrapper is `report-line-{index}`; the block in it
/// is `report-{kind}-{index}`.
fn draw_line(index: usize, line: &ReportLine) -> Div {
    let (kind, block) = match line {
        ReportLine::Heading { level, text } => ("heading", heading(index, *level, text)),
        ReportLine::Item {
            depth,
            marker,
            text,
        } => ("item", item(*depth, marker, text)),
        ReportLine::Code(text) => ("code", code(text)),
        ReportLine::Quote(text) => ("quote", quote(text)),
        ReportLine::Rule => ("rule", rule()),
        ReportLine::Text(text) => ("text", text_block(text)),
        ReportLine::Blank => ("blank", blank()),
    };
    div()
        .debug_selector(move || format!("report-line-{index}"))
        .flex()
        .flex_col()
        .w_full()
        .flex_none()
        .child(block.debug_selector(move || format!("report-{kind}-{index}")))
}

/// A heading: bold. Levels 1 and 2 get a gap above them, except on the first line.
fn heading(index: usize, level: u8, text: &str) -> Div {
    let gap_above = index > 0 && level <= 2;
    div()
        .font_weight(FontWeight::BOLD)
        .text_color(rgb(TEXT))
        .whitespace_normal()
        .when(gap_above, |block| block.mt(px(REPORT_BLOCK_GAP)))
        .child(text.to_owned())
}

/// A list item: the marker in its column, then the text, indented by depth.
fn item(depth: u8, marker: &str, text: &str) -> Div {
    div()
        .flex()
        .items_start()
        .min_h(px(LINE_HEIGHT))
        .pl(px(f32::from(depth) * REPORT_INDENT))
        .child(
            div()
                .flex_none()
                .w(px(REPORT_MARKER_WIDTH))
                .text_color(rgb(TEXT_DIM))
                .child(marker.to_owned()),
        )
        .child(
            div()
                .flex_1()
                .min_w_0()
                .text_color(rgb(TEXT_2))
                .whitespace_normal()
                .child(text.to_owned()),
        )
}

/// A line of code: one line high, on the code background, clipped at the panel edge. The owner
/// opens the editor for the rest.
fn code(text: &str) -> Div {
    div()
        .h(px(LINE_HEIGHT))
        .flex_none()
        .px(px(REPORT_CODE_PAD_X))
        .bg(rgb(REPORT_CODE_BG))
        .text_color(rgb(TEXT_2))
        .whitespace_nowrap()
        .overflow_x_hidden()
        .child(text.to_owned())
}

/// A quote: dim, with a rule on the left.
fn quote(text: &str) -> Div {
    div()
        .min_h(px(LINE_HEIGHT))
        .border_l_1()
        .border_color(rgb(LINE))
        .pl(px(REPORT_QUOTE_PAD))
        .text_color(rgb(TEXT_DIM))
        .whitespace_normal()
        .child(text.to_owned())
}

/// A horizontal rule.
fn rule() -> Div {
    div()
        .h(px(1.))
        .flex_none()
        .my(px(REPORT_RULE_MARGIN))
        .bg(rgb(LINE))
}

/// A line of text: secondary colour, wrapped.
fn text_block(text: &str) -> Div {
    div()
        .text_color(rgb(TEXT_2))
        .whitespace_normal()
        .child(text.to_owned())
}

/// A blank line: half a line high.
fn blank() -> Div {
    div().h(px(REPORT_BLANK_HEIGHT)).flex_none()
}

/// A fixed line in the body, in `colour`.
fn note(selector: &'static str, text: &str, colour: u32) -> Div {
    div()
        .debug_selector(move || selector.to_owned())
        .flex_none()
        .text_color(rgb(colour))
        .whitespace_normal()
        .child(text.to_owned())
}

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used)]
mod tests {
    use super::*;
    use crate::board_model::parse_board;
    use crate::report::{MAX_REPORT_LINES, NO_REPORT_FILE, NOT_A_FILE};
    use gpui::{Bounds, Entity, Modifiers, Pixels, TestAppContext, VisualTestContext, size};
    use std::cell::RefCell;

    const FIXTURE: &str = include_str!("board_fixture_v1.json");
    const REPORT: &str = include_str!("report_fixture.md");
    /// The window height in the tests. The width is the panel's.
    const WINDOW_HEIGHT: f32 = 700.;

    /// The paths that `Open in editor` asked to open, in order.
    type Opened = Rc<RefCell<Vec<PathBuf>>>;

    fn source(report: Option<String>, report_by: Option<&str>) -> ReportSource {
        ReportSource {
            key: "owner/name#640".into(),
            label: "#640".into(),
            title: "sort bags".into(),
            report,
            report_by: report_by.map(str::to_owned),
        }
    }

    /// A RESULT.md with `text`, in a new temp dir. The dir is returned so it lives as long as the
    /// test. The path is absolute and ends in `.md`.
    fn result_file(text: &[u8]) -> (tempfile::TempDir, String) {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("RESULT.md");
        std::fs::write(&path, text).unwrap();
        let raw = path.to_str().unwrap().to_owned();
        (dir, raw)
    }

    /// A view in a window as wide as the panel, with the paths it opens recorded.
    fn report_view(
        cx: &mut TestAppContext,
    ) -> (Entity<ReportView>, Opened, &mut VisualTestContext) {
        let opened: Opened = Rc::new(RefCell::new(Vec::new()));
        let log = opened.clone();
        let open: OpenPathFn =
            Rc::new(move |path: PathBuf, _: &mut App| log.borrow_mut().push(path));
        let (view, cx) = cx.add_window_view(|_, cx| ReportView::with_open(open, cx));
        cx.simulate_resize(size(px(PANEL_WIDTH), px(WINDOW_HEIGHT)));
        cx.run_until_parked();
        (view, opened, cx)
    }

    fn show_target(
        cx: &mut VisualTestContext,
        view: &Entity<ReportView>,
        target: ReportTarget,
        rev: u64,
    ) {
        cx.update(|_, app| view.update(app, |view, cx| view.show(target, rev, cx)));
    }

    fn body_of(cx: &mut VisualTestContext, view: &Entity<ReportView>) -> ReportBody {
        cx.update(|_, app| view.read(app).body.clone())
    }

    fn reads_of(cx: &mut VisualTestContext, view: &Entity<ReportView>) -> usize {
        cx.update(|_, app| view.read(app).reads)
    }

    fn text_lines(body: &ReportBody) -> Vec<ReportLine> {
        match body {
            ReportBody::Text { text, .. } => text.lines.clone(),
            other => panic!("expected a text body, got {other:?}"),
        }
    }

    fn bounds(cx: &mut VisualTestContext, selector: &'static str) -> Option<Bounds<Pixels>> {
        cx.debug_bounds(selector)
    }

    /// Clicks the centre of the element with the given selector, as the mouse would.
    fn click(cx: &mut VisualTestContext, selector: &'static str) {
        let bounds = cx.debug_bounds(selector).unwrap();
        cx.simulate_click(bounds.center(), Modifiers::none());
        cx.run_until_parked();
    }

    #[test]
    fn the_title_joins_label_and_title_and_skips_empty_parts() {
        assert_eq!(title_text(&source(None, None)), "#640  sort bags");
        let no_label = ReportSource {
            label: String::new(),
            ..source(None, None)
        };
        assert_eq!(title_text(&no_label), "sort bags");
    }

    #[test]
    fn the_aria_label_names_the_row_and_its_worker() {
        let row = ReportTarget::Row(source(None, Some("b-1151")));
        assert_eq!(aria_label(&row), "report #640 sort bags, by b-1151");
        let no_worker = ReportTarget::Row(source(None, None));
        assert_eq!(aria_label(&no_worker), "report #640 sort bags");
        assert_eq!(aria_label(&ReportTarget::None), "report");
        assert_eq!(aria_label(&ReportTarget::Gone), "report");
    }

    #[test]
    fn from_row_copies_the_fields_the_view_shows() {
        let snapshot = parse_board(FIXTURE.as_bytes()).unwrap();
        let source = ReportSource::from_row(&snapshot.rows[0]);
        assert_eq!(source.key, "owner/name#640");
        assert_eq!(source.label, "#640");
        assert_eq!(source.title, "sort bags");
        assert_eq!(source.report.as_deref(), Some("/example/b-1151/RESULT.md"));
        assert_eq!(source.report_by.as_deref(), Some("b-1151"));
    }

    #[gpui::test]
    fn no_target_shows_the_pick_line(cx: &mut TestAppContext) {
        let (view, _opened, cx) = report_view(cx);
        show_target(cx, &view, ReportTarget::None, 1);
        cx.run_until_parked();
        assert_eq!(body_of(cx, &view), ReportBody::Empty(NO_ROW));
        assert_eq!(reads_of(cx, &view), 0);
        assert!(bounds(cx, "report-empty").is_some());
        assert!(
            bounds(cx, "report-title").is_none(),
            "no title without a row"
        );
        assert!(bounds(cx, "report-open").is_none());
        assert!(bounds(cx, "report-back").is_some(), "back always shows");
    }

    #[gpui::test]
    fn a_gone_row_shows_the_gone_line(cx: &mut TestAppContext) {
        let (view, _opened, cx) = report_view(cx);
        show_target(cx, &view, ReportTarget::Gone, 1);
        cx.run_until_parked();
        assert_eq!(body_of(cx, &view), ReportBody::Empty(ROW_GONE));
        assert_eq!(reads_of(cx, &view), 0);
        assert!(bounds(cx, "report-title").is_none());
    }

    #[gpui::test]
    fn a_row_without_report_shows_no_report_and_reads_nothing(cx: &mut TestAppContext) {
        let (view, _opened, cx) = report_view(cx);
        show_target(
            cx,
            &view,
            ReportTarget::Row(source(None, Some("b-1151"))),
            1,
        );
        cx.run_until_parked();
        assert_eq!(body_of(cx, &view), ReportBody::Empty(NO_REPORT));
        assert_eq!(reads_of(cx, &view), 0);
        assert!(bounds(cx, "report-open").is_none());
        assert!(bounds(cx, "report-title").is_some());
    }

    #[gpui::test]
    fn a_bad_path_shows_the_path_line_and_hides_open(cx: &mut TestAppContext) {
        let (view, _opened, cx) = report_view(cx);
        for raw in ["RESULT.md", "/example/notes.txt"] {
            let target = ReportTarget::Row(source(Some(raw.to_owned()), None));
            show_target(cx, &view, target, 1);
            cx.run_until_parked();
            assert_eq!(
                body_of(cx, &view),
                ReportBody::Failed(BAD_REPORT_PATH.to_owned()),
                "{raw}"
            );
            assert!(bounds(cx, "report-open").is_none(), "{raw}");
        }
        assert_eq!(reads_of(cx, &view), 0);
    }

    #[gpui::test]
    fn a_row_reads_the_file_once_and_draws_its_lines(cx: &mut TestAppContext) {
        let (_dir, raw) = result_file(REPORT.as_bytes());
        let (view, _opened, cx) = report_view(cx);
        show_target(
            cx,
            &view,
            ReportTarget::Row(source(Some(raw), Some("b-1151"))),
            42,
        );
        cx.run_until_parked();
        assert_eq!(reads_of(cx, &view), 1);

        let lines = text_lines(&body_of(cx, &view));
        assert_eq!(
            lines[0],
            ReportLine::Heading {
                level: 2,
                text: "Result: sort bags (#640)".to_owned()
            },
            "the comment line is hidden, so the heading is line 0"
        );
        assert!(bounds(cx, "report-line-0").is_some());
        assert!(bounds(cx, "report-heading-0").is_some());
        assert!(bounds(cx, "report-open").is_some());
    }

    #[gpui::test]
    fn the_title_and_by_line_follow_the_row(cx: &mut TestAppContext) {
        let (view, _opened, cx) = report_view(cx);
        show_target(
            cx,
            &view,
            ReportTarget::Row(source(None, Some("b-1151"))),
            1,
        );
        cx.run_until_parked();
        assert!(bounds(cx, "report-title").is_some());
        assert!(bounds(cx, "report-meta").is_some(), "by b-1151 shows");

        show_target(cx, &view, ReportTarget::Row(source(None, None)), 1);
        cx.run_until_parked();
        assert!(bounds(cx, "report-title").is_some());
        assert!(
            bounds(cx, "report-meta").is_none(),
            "no by line and no problem: no meta line"
        );
    }

    #[gpui::test]
    fn the_same_path_and_rev_does_not_read_again(cx: &mut TestAppContext) {
        let (_dir, raw) = result_file(REPORT.as_bytes());
        let (view, _opened, cx) = report_view(cx);
        let target = || ReportTarget::Row(source(Some(raw.clone()), None));
        show_target(cx, &view, target(), 42);
        cx.run_until_parked();
        show_target(cx, &view, target(), 42);
        cx.run_until_parked();
        assert_eq!(reads_of(cx, &view), 1);
    }

    #[gpui::test]
    fn a_new_rev_reads_again(cx: &mut TestAppContext) {
        let (_dir, raw) = result_file(REPORT.as_bytes());
        let (view, _opened, cx) = report_view(cx);
        let target = || ReportTarget::Row(source(Some(raw.clone()), None));
        show_target(cx, &view, target(), 42);
        cx.run_until_parked();
        let first = text_lines(&body_of(cx, &view));

        show_target(cx, &view, target(), 43);
        assert!(
            matches!(body_of(cx, &view), ReportBody::Text { .. }),
            "the text of the same path stays while the re-read runs"
        );
        cx.run_until_parked();
        assert_eq!(reads_of(cx, &view), 2);
        assert_eq!(text_lines(&body_of(cx, &view)), first);
    }

    #[gpui::test]
    fn a_new_path_shows_loading_until_it_is_read(cx: &mut TestAppContext) {
        let (_dir_a, first) = result_file(b"# First\n");
        let (_dir_b, second) = result_file(b"# Second\n");
        let (view, _opened, cx) = report_view(cx);
        show_target(cx, &view, ReportTarget::Row(source(Some(first), None)), 1);
        cx.run_until_parked();

        show_target(cx, &view, ReportTarget::Row(source(Some(second), None)), 1);
        assert_eq!(body_of(cx, &view), ReportBody::Loading);
        assert!(bounds(cx, "report-empty").is_some());
        cx.run_until_parked();
        let lines = text_lines(&body_of(cx, &view));
        assert_eq!(
            lines[0],
            ReportLine::Heading {
                level: 1,
                text: "Second".to_owned()
            }
        );
    }

    #[gpui::test]
    fn a_failed_reread_keeps_the_text_and_shows_the_problem(cx: &mut TestAppContext) {
        let (dir, raw) = result_file(REPORT.as_bytes());
        let (view, _opened, cx) = report_view(cx);
        let target = || ReportTarget::Row(source(Some(raw.clone()), Some("b-1151")));
        show_target(cx, &view, target(), 42);
        cx.run_until_parked();
        let before = text_lines(&body_of(cx, &view));

        std::fs::remove_file(dir.path().join("RESULT.md")).unwrap();
        show_target(cx, &view, target(), 43);
        cx.run_until_parked();

        assert_eq!(
            text_lines(&body_of(cx, &view)),
            before,
            "the old text is kept"
        );
        assert!(bounds(cx, "report-line-0").is_some());
        assert!(bounds(cx, "report-problem").is_some());
        assert!(bounds(cx, "report-meta").is_some());
        match body_of(cx, &view) {
            ReportBody::Text { problem, .. } => {
                assert_eq!(problem.as_deref(), Some(NO_REPORT_FILE));
            }
            other => panic!("expected the old text, got {other:?}"),
        }
    }

    #[gpui::test]
    fn a_stale_read_is_dropped_when_the_target_changes(cx: &mut TestAppContext) {
        let (_dir_a, first) = result_file(REPORT.as_bytes());
        let (_dir_b, second) = result_file(b"# Second\n");
        let (view, _opened, cx) = report_view(cx);

        // The target goes to no row before the read lands, so the read's task is dropped. The
        // late results below are passed to `loaded` by hand, as a task that was not dropped would.
        show_target(
            cx,
            &view,
            ReportTarget::Row(source(Some(first.clone()), None)),
            42,
        );
        show_target(cx, &view, ReportTarget::None, 42);
        cx.run_until_parked();
        assert_eq!(body_of(cx, &view), ReportBody::Empty(NO_ROW));
        assert_eq!(reads_of(cx, &view), 1, "the read started, then was dropped");

        // A late result of the first read reaches `loaded`. Its key no longer matches, so it is
        // dropped and the pick line stays.
        let late = (PathBuf::from(&first), 42);
        let result = read_report(&late.0);
        cx.update(|_, app| view.update(app, |view, cx| view.loaded(late, result, cx)));
        cx.run_until_parked();
        assert_eq!(body_of(cx, &view), ReportBody::Empty(NO_ROW));

        // The same holds when the path changes: a late result for the old path is dropped while
        // the new path's read is still current.
        show_target(
            cx,
            &view,
            ReportTarget::Row(source(Some(second.clone()), None)),
            42,
        );
        let late = (PathBuf::from(&first), 42);
        let result = read_report(&late.0);
        cx.update(|_, app| view.update(app, |view, cx| view.loaded(late, result, cx)));
        assert_eq!(body_of(cx, &view), ReportBody::Loading);
        cx.run_until_parked();
        let lines = text_lines(&body_of(cx, &view));
        assert_eq!(
            lines[0],
            ReportLine::Heading {
                level: 1,
                text: "Second".to_owned()
            }
        );
    }

    #[gpui::test]
    fn a_missing_file_and_a_directory_show_their_lines(cx: &mut TestAppContext) {
        let dir = tempfile::tempdir().unwrap();
        let missing = dir.path().join("RESULT.md").to_str().unwrap().to_owned();
        let folder = dir.path().join("sub.md");
        std::fs::create_dir(&folder).unwrap();
        let folder = folder.to_str().unwrap().to_owned();
        let (view, _opened, cx) = report_view(cx);

        show_target(cx, &view, ReportTarget::Row(source(Some(missing), None)), 1);
        cx.run_until_parked();
        assert_eq!(
            body_of(cx, &view),
            ReportBody::Failed(NO_REPORT_FILE.to_owned())
        );
        assert!(
            bounds(cx, "report-open").is_some(),
            "the path is valid: open shows"
        );

        show_target(cx, &view, ReportTarget::Row(source(Some(folder), None)), 2);
        cx.run_until_parked();
        assert_eq!(
            body_of(cx, &view),
            ReportBody::Failed(NOT_A_FILE.to_owned())
        );
    }

    #[gpui::test]
    fn open_in_editor_opens_the_path(cx: &mut TestAppContext) {
        let (_dir, raw) = result_file(REPORT.as_bytes());
        let (view, opened, cx) = report_view(cx);
        show_target(
            cx,
            &view,
            ReportTarget::Row(source(Some(raw.clone()), None)),
            1,
        );
        cx.run_until_parked();
        click(cx, "report-open");
        assert_eq!(*opened.borrow(), vec![PathBuf::from(&raw)]);
    }

    #[gpui::test]
    fn back_emits_the_back_event(cx: &mut TestAppContext) {
        let (view, _opened, cx) = report_view(cx);
        let events: Rc<RefCell<Vec<ReportEvent>>> = Rc::new(RefCell::new(Vec::new()));
        let log = events.clone();
        let _subscription = cx.update(|_, app| {
            app.subscribe(&view, move |_, event: &ReportEvent, _| {
                log.borrow_mut().push(*event)
            })
        });
        click(cx, "report-back");
        assert_eq!(*events.borrow(), vec![ReportEvent::Back]);
        assert!(
            cx.debug_bounds("report-open").is_none(),
            "no open action without a row"
        );
    }

    #[gpui::test]
    fn an_empty_file_shows_the_empty_line(cx: &mut TestAppContext) {
        let (_dir, raw) = result_file(b"");
        let (view, _opened, cx) = report_view(cx);
        show_target(cx, &view, ReportTarget::Row(source(Some(raw), None)), 1);
        cx.run_until_parked();
        assert_eq!(body_of(cx, &view), ReportBody::Empty(EMPTY_REPORT));
        assert!(bounds(cx, "report-empty").is_some());
        assert!(bounds(cx, "report-line-0").is_none());
    }

    #[gpui::test]
    fn a_cut_report_shows_the_cut_line(cx: &mut TestAppContext) {
        let text = "a\n".repeat(MAX_REPORT_LINES + 1);
        let (_dir, raw) = result_file(text.as_bytes());
        let (view, _opened, cx) = report_view(cx);
        show_target(cx, &view, ReportTarget::Row(source(Some(raw), None)), 1);
        cx.run_until_parked();
        assert!(bounds(cx, "report-cut").is_some());
    }

    #[gpui::test]
    fn the_body_scrolls_inside_the_panel(cx: &mut TestAppContext) {
        let text: String = (0..300).map(|i| format!("line {i}\n")).collect();
        let (_dir, raw) = result_file(text.as_bytes());
        let (view, _opened, cx) = report_view(cx);
        show_target(cx, &view, ReportTarget::Row(source(Some(raw), None)), 1);
        cx.run_until_parked();

        let body = bounds(cx, "report-body").unwrap();
        let window_bottom = px(WINDOW_HEIGHT);
        assert!(body.origin.y + body.size.height <= window_bottom);
        let last = bounds(cx, "report-line-299").unwrap();
        assert!(
            last.origin.y >= body.origin.y + body.size.height,
            "the last line lies below the visible body, so it scrolls and is not squeezed"
        );
    }
}
