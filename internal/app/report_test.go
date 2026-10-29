package app

import (
	_ "embed"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
)

//go:embed testdata/report_fixture.md
var reportFixture string

// reportRig is a view in a window as wide as the panel. It counts redraw
// requests and records the paths that Open in editor opens.
type reportRig struct {
	t       *testing.T
	v       *ReportView
	tt      *ui.Tester
	changes atomic.Int32
	mu      sync.Mutex
	opened  []string
	backs   int
}

func newReportRig(t *testing.T) *reportRig {
	t.Helper()
	r := &reportRig{t: t}
	r.v = NewReportView(func() { r.changes.Add(1) })
	r.v.Open = func(_ *ui.Context, path string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.opened = append(r.opened, path)
	}
	r.v.OnBack = func() { r.backs++ }
	r.tt = ui.NewTester(r.v.View, theme.PanelWidth, 700)
	return r
}

// show shows target, waits for the read it starts, and draws the result.
func (r *reportRig) show(target ReportTarget, rev uint64) {
	r.t.Helper()
	r.v.Show(target, rev)
	r.v.wg.Wait()
	r.tt.Frame()
}

func (r *reportRig) body() ReportBody { _, b := r.v.state(); return b }

func (r *reportRig) readCount() int {
	r.v.mu.Lock()
	defer r.v.mu.Unlock()
	return r.v.reads
}

func (r *reportRig) has(label string) bool {
	_, ok := r.tt.Find(label)
	return ok
}

func (r *reportRig) textLines() []board.ReportLine {
	r.t.Helper()
	b := r.body()
	if b.Kind != BodyText {
		r.t.Fatalf("expected a text body, got %+v", b)
	}
	return b.Text.Lines
}

func reportSource(report, by *string) ReportSource {
	return ReportSource{Key: "owner/name#640", Label: "#640", Title: "sort bags", Report: report, ReportBy: by}
}

func rowTarget(report, by *string) ReportTarget {
	return ReportTarget{Kind: TargetRow, Source: reportSource(report, by)}
}

// resultFile writes text as RESULT.md in a new temp dir and returns its
// absolute path.
func resultFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "RESULT.md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func heading(level uint8, text string) board.ReportLine {
	return board.ReportLine{Kind: board.LineHeading, Level: level, Text: text}
}

// Port of the Rust test the_title_joins_label_and_title_and_skips_empty_parts.
func TestTheTitleJoinsLabelAndTitleAndSkipsEmptyParts(t *testing.T) {
	if got := titleText(reportSource(nil, nil)); got != "#640  sort bags" {
		t.Errorf("title %q", got)
	}
	noLabel := reportSource(nil, nil)
	noLabel.Label = ""
	if got := titleText(noLabel); got != "sort bags" {
		t.Errorf("title %q", got)
	}
}

// Port of the Rust test the_aria_label_names_the_row_and_its_worker.
func TestTheAriaLabelNamesTheRowAndItsWorker(t *testing.T) {
	cases := []struct {
		target ReportTarget
		want   string
	}{
		{rowTarget(nil, strp("b-1151")), "report #640 sort bags, by b-1151"},
		{rowTarget(nil, nil), "report #640 sort bags"},
		{ReportTarget{Kind: TargetNone}, "report"},
		{ReportTarget{Kind: TargetGone}, "report"},
	}
	for _, c := range cases {
		if got := ariaLabel(c.target); got != c.want {
			t.Errorf("aria label %q, want %q", got, c.want)
		}
	}
}

// Port of the Rust test from_row_copies_the_fields_the_view_shows.
func TestFromRowCopiesTheFieldsTheViewShows(t *testing.T) {
	snap, err := board.Parse(boardFixture)
	if err != nil {
		t.Fatal(err)
	}
	src := SourceFromRow(snap.Rows[0])
	want := ReportSource{Key: "owner/name#640", Label: "#640", Title: "sort bags",
		Report: strp("/example/b-1151/RESULT.md"), ReportBy: strp("b-1151")}
	if !reflect.DeepEqual(src, want) {
		t.Errorf("source %+v, want %+v", src, want)
	}
}

// Port of the Rust test no_target_shows_the_pick_line.
func TestNoTargetShowsThePickLine(t *testing.T) {
	r := newReportRig(t)
	r.show(ReportTarget{Kind: TargetNone}, 1)
	if got := r.body(); got != (ReportBody{Kind: BodyEmpty, Line: ReportNoRow}) {
		t.Errorf("body %+v", got)
	}
	if n := r.readCount(); n != 0 {
		t.Errorf("%d reads", n)
	}
	if !r.has(LabelReportEmpty) || !r.tt.HasText(ReportNoRow) {
		t.Errorf("no pick line: %q", r.tt.Texts())
	}
	if r.has(LabelReportTitle) {
		t.Error("a title shows without a row")
	}
	if r.has(LabelReportOpen) {
		t.Error("open shows without a row")
	}
	if !r.has(LabelReportBack) {
		t.Error("back is always shown")
	}
}

// Port of the Rust test a_gone_row_shows_the_gone_line.
func TestAGoneRowShowsTheGoneLine(t *testing.T) {
	r := newReportRig(t)
	r.show(ReportTarget{Kind: TargetGone}, 1)
	if got := r.body(); got != (ReportBody{Kind: BodyEmpty, Line: ReportRowGone}) {
		t.Errorf("body %+v", got)
	}
	if n := r.readCount(); n != 0 {
		t.Errorf("%d reads", n)
	}
	if r.has(LabelReportTitle) {
		t.Error("a title shows for a gone row")
	}
}

// Port of the Rust test a_row_without_report_shows_no_report_and_reads_nothing.
func TestARowWithoutReportShowsNoReportAndReadsNothing(t *testing.T) {
	r := newReportRig(t)
	r.show(rowTarget(nil, strp("b-1151")), 1)
	if got := r.body(); got != (ReportBody{Kind: BodyEmpty, Line: ReportNone}) {
		t.Errorf("body %+v", got)
	}
	if n := r.readCount(); n != 0 {
		t.Errorf("%d reads", n)
	}
	if r.has(LabelReportOpen) {
		t.Error("open shows without a report")
	}
	if !r.has(LabelReportTitle) {
		t.Error("no title")
	}
}

// Port of the Rust test a_bad_path_shows_the_path_line_and_hides_open.
func TestABadPathShowsThePathLineAndHidesOpen(t *testing.T) {
	r := newReportRig(t)
	for _, raw := range []string{"RESULT.md", "/example/notes.txt"} {
		r.show(rowTarget(strp(raw), nil), 1)
		if got := r.body(); got != (ReportBody{Kind: BodyFailed, Line: board.BadReportPath}) {
			t.Errorf("%s: body %+v", raw, got)
		}
		if r.has(LabelReportOpen) {
			t.Errorf("%s: open shows", raw)
		}
	}
	if n := r.readCount(); n != 0 {
		t.Errorf("%d reads", n)
	}
}

// Port of the Rust test a_row_reads_the_file_once_and_draws_its_lines.
func TestARowReadsTheFileOnceAndDrawsItsLines(t *testing.T) {
	path := resultFile(t, reportFixture)
	r := newReportRig(t)
	r.show(rowTarget(&path, strp("b-1151")), 42)
	if n := r.readCount(); n != 1 {
		t.Errorf("%d reads, want 1", n)
	}
	lines := r.textLines()
	// The comment line is hidden, so the heading is line 0.
	if want := heading(2, "Result: sort bags (#640)"); lines[0] != want {
		t.Errorf("line 0 %+v, want %+v", lines[0], want)
	}
	for _, label := range []string{"report-line-0", "report-heading-0", LabelReportOpen} {
		if !r.has(label) {
			t.Errorf("no %s", label)
		}
	}
	if r.changes.Load() == 0 {
		t.Error("the view did not ask for a redraw")
	}
}

// Port of the Rust test the_title_and_by_line_follow_the_row.
func TestTheTitleAndByLineFollowTheRow(t *testing.T) {
	r := newReportRig(t)
	r.show(rowTarget(nil, strp("b-1151")), 1)
	if !r.has(LabelReportTitle) || !r.has(LabelReportMeta) || !r.tt.HasText("by b-1151") {
		t.Errorf("title or by line missing: %q", r.tt.Texts())
	}
	r.show(rowTarget(nil, nil), 1)
	if !r.has(LabelReportTitle) {
		t.Error("no title")
	}
	if r.has(LabelReportMeta) {
		t.Error("a meta line shows with no by line and no problem")
	}
}

// Port of the Rust test the_same_path_and_rev_does_not_read_again.
func TestTheSamePathAndRevDoesNotReadAgain(t *testing.T) {
	path := resultFile(t, reportFixture)
	r := newReportRig(t)
	r.show(rowTarget(&path, nil), 42)
	r.show(rowTarget(&path, nil), 42)
	if n := r.readCount(); n != 1 {
		t.Errorf("%d reads, want 1", n)
	}
}

// gate holds reads until release is called, so a test sees the state while a
// read is in flight.
func gate(r *reportRig) (release func()) {
	ch := make(chan struct{})
	inner := r.v.read
	r.v.read = func(path string) (board.ReportText, error) {
		<-ch
		return inner(path)
	}
	return func() { close(ch); r.v.wg.Wait(); r.tt.Frame() }
}

// Port of the Rust test a_new_rev_reads_again.
func TestANewRevReadsAgain(t *testing.T) {
	path := resultFile(t, reportFixture)
	r := newReportRig(t)
	r.show(rowTarget(&path, nil), 42)
	first := slices.Clone(r.textLines())

	release := gate(r)
	r.v.Show(rowTarget(&path, nil), 43)
	if r.body().Kind != BodyText {
		t.Error("the text of the same path does not stay while the re-read runs")
	}
	release()
	if n := r.readCount(); n != 2 {
		t.Errorf("%d reads, want 2", n)
	}
	if !slices.Equal(r.textLines(), first) {
		t.Error("the text changed")
	}
}

// Port of the Rust test a_new_path_shows_loading_until_it_is_read.
func TestANewPathShowsLoadingUntilItIsRead(t *testing.T) {
	first, second := resultFile(t, "# First\n"), resultFile(t, "# Second\n")
	r := newReportRig(t)
	r.show(rowTarget(&first, nil), 1)

	release := gate(r)
	r.v.Show(rowTarget(&second, nil), 1)
	r.tt.Frame()
	if r.body().Kind != BodyLoading {
		t.Errorf("body %+v, want loading", r.body())
	}
	if !r.has(LabelReportEmpty) || !r.tt.HasText(ReportLoading) {
		t.Errorf("no loading line: %q", r.tt.Texts())
	}
	release()
	if lines := r.textLines(); lines[0] != heading(1, "Second") {
		t.Errorf("line 0 %+v", lines[0])
	}
}

// Port of the Rust test a_failed_reread_keeps_the_text_and_shows_the_problem.
func TestAFailedRereadKeepsTheTextAndShowsTheProblem(t *testing.T) {
	path := resultFile(t, reportFixture)
	r := newReportRig(t)
	target := rowTarget(&path, strp("b-1151"))
	r.show(target, 42)
	before := slices.Clone(r.textLines())

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	r.show(target, 43)

	if !slices.Equal(r.textLines(), before) {
		t.Error("the old text is not kept")
	}
	if !r.has("report-line-0") || !r.has(LabelReportProblem) || !r.has(LabelReportMeta) {
		t.Errorf("text, problem or meta line missing: %q", r.tt.Texts())
	}
	if got := r.body().Problem; got != board.NoReportFile {
		t.Errorf("problem %q, want %q", got, board.NoReportFile)
	}
}

// Port of the Rust test a_stale_read_is_dropped_when_the_target_changes. A Go
// goroutine cannot be dropped, so the late result is dropped by its key.
func TestAStaleReadIsDroppedWhenTheTargetChanges(t *testing.T) {
	first, second := resultFile(t, reportFixture), resultFile(t, "# Second\n")
	r := newReportRig(t)

	// The target goes to no row before the read lands. The late result is
	// passed to loaded by hand, as the goroutine would.
	release := gate(r)
	r.v.Show(rowTarget(&first, nil), 42)
	r.v.Show(ReportTarget{Kind: TargetNone}, 42)
	release()
	if got := r.body(); got != (ReportBody{Kind: BodyEmpty, Line: ReportNoRow}) {
		t.Errorf("body %+v", got)
	}
	if n := r.readCount(); n != 1 {
		t.Errorf("%d reads, want 1: the read started, then was dropped", n)
	}
	late := readKey{first, 42}
	text, err := board.ReadReport(first)
	r.v.loaded(late, text, err)
	if got := r.body(); got != (ReportBody{Kind: BodyEmpty, Line: ReportNoRow}) {
		t.Errorf("a late result changed the body: %+v", got)
	}

	// The same holds when the path changes: a late result for the old path
	// is dropped while the new path's read is current.
	release = gate(r)
	r.v.Show(rowTarget(&second, nil), 42)
	r.v.loaded(late, text, err)
	if r.body().Kind != BodyLoading {
		t.Errorf("body %+v, want loading", r.body())
	}
	release()
	if lines := r.textLines(); lines[0] != heading(1, "Second") {
		t.Errorf("line 0 %+v", lines[0])
	}
}

// Port of the Rust test a_missing_file_and_a_directory_show_their_lines.
func TestAMissingFileAndADirectoryShowTheirLines(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "RESULT.md")
	folder := filepath.Join(dir, "sub.md")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	r := newReportRig(t)

	r.show(rowTarget(&missing, nil), 1)
	if got := r.body(); got != (ReportBody{Kind: BodyFailed, Line: board.NoReportFile}) {
		t.Errorf("body %+v", got)
	}
	if !r.has(LabelReportOpen) {
		t.Error("the path is valid, so open shows")
	}

	r.show(rowTarget(&folder, nil), 2)
	if got := r.body(); got != (ReportBody{Kind: BodyFailed, Line: board.NotAFile}) {
		t.Errorf("body %+v", got)
	}
}

// Port of the Rust test open_in_editor_opens_the_path.
func TestOpenInEditorOpensThePath(t *testing.T) {
	path := resultFile(t, reportFixture)
	r := newReportRig(t)
	r.show(rowTarget(&path, nil), 1)
	if err := r.tt.Click("Open in editor"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.opened, []string{path}) {
		t.Errorf("opened %q, want %q", r.opened, path)
	}
}

// The default open call asks the system to open a file: URL of the path.
func TestTheDefaultOpenCallOpensAFileURL(t *testing.T) {
	path := resultFile(t, reportFixture)
	r := newReportRig(t)
	r.v.Open = nil
	r.show(rowTarget(&path, nil), 1)
	if err := r.tt.Click("Open in editor"); err != nil {
		t.Fatal(err)
	}
	urls := r.tt.OpenedURLs()
	if len(urls) != 1 {
		t.Fatalf("opened %q", urls)
	}
	u, err := url.Parse(urls[0])
	if err != nil || u.Scheme != "file" || u.Path != path {
		t.Errorf("opened %q, want a file URL of %q", urls[0], path)
	}
}

// Port of the Rust test back_emits_the_back_event.
func TestBackEmitsTheBackEvent(t *testing.T) {
	r := newReportRig(t)
	if err := r.tt.Click("back"); err != nil {
		t.Fatal(err)
	}
	if r.backs != 1 {
		t.Errorf("%d back calls, want 1", r.backs)
	}
	if r.has(LabelReportOpen) {
		t.Error("an open action shows without a row")
	}
}

// Port of the Rust test an_empty_file_shows_the_empty_line.
func TestAnEmptyFileShowsTheEmptyLine(t *testing.T) {
	path := resultFile(t, "")
	r := newReportRig(t)
	r.show(rowTarget(&path, nil), 1)
	if got := r.body(); got != (ReportBody{Kind: BodyEmpty, Line: ReportEmpty}) {
		t.Errorf("body %+v", got)
	}
	if !r.has(LabelReportEmpty) {
		t.Error("no empty line")
	}
	if r.has("report-line-0") {
		t.Error("a line is drawn")
	}
}

// Port of the Rust test a_cut_report_shows_the_cut_line.
func TestACutReportShowsTheCutLine(t *testing.T) {
	path := resultFile(t, strings.Repeat("a\n", board.MaxReportLines+1))
	r := newReportRig(t)
	r.show(rowTarget(&path, nil), 1)
	if !r.tt.HasText(ReportCutLine) || !r.has(LabelReportCut) {
		t.Error("no cut line")
	}
}

// Port of the Rust test the_body_scrolls_inside_the_panel. Only part of the
// lines is in the window, and the wheel scrolls the rest into it.
func TestTheBodyScrollsInsideThePanel(t *testing.T) {
	var sb strings.Builder
	for i := range 300 {
		sb.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	path := resultFile(t, sb.String())
	r := newReportRig(t)
	r.show(rowTarget(&path, nil), 1)

	body, ok := r.tt.Find(LabelReportBody)
	if !ok {
		t.Fatal("no body")
	}
	if body.Y+body.H > 700 {
		t.Errorf("the body ends at %v, below the window", body.Y+body.H)
	}
	last, ok := r.tt.Find("report-line-299")
	if !ok {
		t.Fatal("no last line")
	}
	if last.Y < body.Y+body.H {
		t.Errorf("the last line is at %v inside the body (ends %v): it is squeezed, not scrolled", last.Y, body.Y+body.H)
	}
	before, _ := r.tt.Find("report-line-0")
	r.tt.Scroll(body.X+10, body.Y+10, 0, 400)
	after, ok := r.tt.Find("report-line-0")
	if !ok {
		t.Fatal("line 0 vanished")
	}
	if after.Y >= before.Y {
		t.Errorf("line 0 stayed at %v after a scroll", after.Y)
	}
}
