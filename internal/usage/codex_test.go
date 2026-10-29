package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	codexPrimary   = `{"used_percent":4.0,"window_minutes":300,"resets_at":1777927196}`
	codexSecondary = `{"used_percent":25.0,"window_minutes":10080,"resets_at":1778500000}`
	codexMtime     = uint64(1_777_900_000)
)

// tokenLine is a token_count line whose rate_limits is the given JSON.
func tokenLine(rateLimits string) string {
	return `{"type":"event_msg","payload":{"type":"token_count","rate_limits":` + rateLimits + `}}`
}

// codexLimits is the usual rate_limits with both windows.
func codexLimits(primary, secondary string) string {
	return `{"limit_id":"codex","primary":` + primary + `,"secondary":` + secondary + `,"credits":null}`
}

// writeRollout writes a rollout file at home/sessions/<day>/<name> and sets its
// mtime.
func writeRollout(t *testing.T, home, day, name string, lines []string, mtime uint64) string {
	t.Helper()
	folder := filepath.Join(home, "sessions", filepath.FromSlash(day))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(int64(mtime), 0)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	return path
}

func primaryOnly(used float64) string {
	return tokenLine(fmt.Sprintf(`{"limit_id":"codex","primary":{"used_percent":%v,"resets_at":1777927196}}`, used))
}

func mustWindows(t *testing.T, tail string) []Window {
	t.Helper()
	w := ParseRolloutTail(tail, codexMtime)
	if w == nil {
		t.Fatal("no windows")
	}
	return w
}

func mustRead(t *testing.T, home string) *PlanUsage {
	t.Helper()
	u := ReadCodex(home)
	if u == nil {
		t.Fatal("no usage")
	}
	return u
}

func TestTheDocumentedShapeGivesTwoWindows(t *testing.T) {
	w := mustWindows(t, tokenLine(codexLimits(codexPrimary, codexSecondary)))
	want := []Window{
		{Kind: "primary", UsedPercent: 4.0, ResetsAt: u64(1_777_927_196)},
		{Kind: "secondary", UsedPercent: 25.0, ResetsAt: u64(1_778_500_000)},
	}
	if !reflect.DeepEqual(w, want) {
		t.Fatalf("got %+v, want %+v", w, want)
	}
}

func TestPrimaryAloneGivesOneWindow(t *testing.T) {
	w := mustWindows(t, tokenLine(codexLimits(codexPrimary, "null")))
	if len(w) != 1 || w[0].Kind != "primary" {
		t.Fatalf("got %+v", w)
	}
}

func TestASecondaryWithoutAPercentIsDroppedAndPrimaryKept(t *testing.T) {
	w := mustWindows(t, tokenLine(codexLimits(codexPrimary, `{"resets_at":1778500000}`)))
	if len(w) != 1 || w[0].Kind != "primary" {
		t.Fatalf("got %+v", w)
	}
}

func TestAMissingLimitIDIsAccepted(t *testing.T) {
	if ParseRolloutTail(tokenLine(`{"primary":`+codexPrimary+`}`), codexMtime) == nil {
		t.Fatal("want windows")
	}
}

func TestTheLastUsableLineWins(t *testing.T) {
	w := mustWindows(t, strings.Join([]string{primaryOnly(10), primaryOnly(30)}, "\n"))
	if w[0].UsedPercent != 30 {
		t.Fatalf("got %v", w[0].UsedPercent)
	}
}

func TestALineWithNullRateLimitsIsSkipped(t *testing.T) {
	w := mustWindows(t, strings.Join([]string{primaryOnly(10), tokenLine("null")}, "\n"))
	if w[0].UsedPercent != 10 {
		t.Fatalf("got %v", w[0].UsedPercent)
	}
}

func TestALineWithNullPrimaryIsSkipped(t *testing.T) {
	w := mustWindows(t, strings.Join([]string{primaryOnly(10), tokenLine(codexLimits("null", codexSecondary))}, "\n"))
	if w[0].UsedPercent != 10 {
		t.Fatalf("got %v", w[0].UsedPercent)
	}
}

func TestAPrimaryWithoutAPercentIsSkipped(t *testing.T) {
	noPercent := tokenLine(`{"primary":{"resets_at":1777927196}}`)
	w := mustWindows(t, strings.Join([]string{primaryOnly(10), noPercent}, "\n"))
	if w[0].UsedPercent != 10 {
		t.Fatalf("got %v", w[0].UsedPercent)
	}
}

func TestALimitIDOtherThanCodexIsSkipped(t *testing.T) {
	other := tokenLine(`{"limit_id":"other","primary":` + codexPrimary + `}`)
	w := mustWindows(t, strings.Join([]string{primaryOnly(10), other}, "\n"))
	if w[0].UsedPercent != 10 {
		t.Fatalf("got %v", w[0].UsedPercent)
	}
}

func TestRateLimitsOnAnotherEventAreIgnored(t *testing.T) {
	line := `{"type":"event_msg","payload":{"type":"agent_message","rate_limits":` + codexLimits(codexPrimary, codexSecondary) + `}}`
	if w := ParseRolloutTail(line, codexMtime); w != nil {
		t.Fatalf("got %+v", w)
	}
}

func TestATruncatedFirstLineIsIgnored(t *testing.T) {
	tail := strings.Join([]string{`ed_percent":4.0,"resets_at":1777927196}}`, primaryOnly(12)}, "\n")
	w := mustWindows(t, tail)
	if w[0].UsedPercent != 12 {
		t.Fatalf("got %v", w[0].UsedPercent)
	}
}

func TestResetsInSecondsCountsFromTheFileTime(t *testing.T) {
	line := tokenLine(`{"limit_id":"codex","primary":{"used_percent":4.0,"resets_in_seconds":3600}}`)
	w := mustWindows(t, line)
	if w[0].ResetsAt == nil || *w[0].ResetsAt != codexMtime+3600 {
		t.Fatalf("got %v", w[0].ResetsAt)
	}
}

func TestResetsAtWinsOverResetsInSeconds(t *testing.T) {
	line := tokenLine(`{"primary":{"used_percent":4.0,"resets_at":5,"resets_in_seconds":3600}}`)
	w := mustWindows(t, line)
	if w[0].ResetsAt == nil || *w[0].ResetsAt != 5 {
		t.Fatalf("got %v", w[0].ResetsAt)
	}
}

func TestAFloatResetsAtIsReadAsWholeSeconds(t *testing.T) {
	line := tokenLine(`{"primary":{"used_percent":4.0,"resets_at":1777927196.0,"resets_in_seconds":60}}`)
	w := mustWindows(t, line)
	if w[0].ResetsAt == nil || *w[0].ResetsAt != 1_777_927_196 {
		t.Fatalf("got %v", w[0].ResetsAt)
	}
}

func TestAFloatResetsInSecondsIsReadAndItsFractionDropped(t *testing.T) {
	line := tokenLine(`{"primary":{"used_percent":4.0,"resets_in_seconds":3600.9}}`)
	w := mustWindows(t, line)
	if w[0].ResetsAt == nil || *w[0].ResetsAt != codexMtime+3600 {
		t.Fatalf("got %v", w[0].ResetsAt)
	}
}

func TestANegativeResetsAtKeepsTheWindowWithoutAResetTime(t *testing.T) {
	w := mustWindows(t, tokenLine(`{"primary":{"used_percent":4.0,"resets_at":-5}}`))
	if w[0].UsedPercent != 4 || w[0].ResetsAt != nil {
		t.Fatalf("got %+v", w[0])
	}
}

func TestATailWithoutAUsableLineIsNone(t *testing.T) {
	tail := strings.Join([]string{
		`{"type":"response_item","payload":{"type":"message"}}`,
		tokenLine("null"),
		"not json",
	}, "\n")
	if w := ParseRolloutTail(tail, codexMtime); w != nil {
		t.Fatalf("got %+v", w)
	}
	if w := ParseRolloutTail("", codexMtime); w != nil {
		t.Fatalf("got %+v", w)
	}
}

func TestCodexHomePrefersTheOverride(t *testing.T) {
	got := CodexHomeFrom(envOf(map[string]string{"CODEX_HOME": "/x/codex"}), "/h")
	if got != "/x/codex" {
		t.Fatalf("got %q", got)
	}
}

func TestCodexHomeDefaultsToDotCodexAndEmptyCountsAsUnset(t *testing.T) {
	if got := CodexHomeFrom(envOf(nil), "/h"); got != filepath.Join("/h", ".codex") {
		t.Fatalf("got %q", got)
	}
	if got := CodexHomeFrom(envOf(map[string]string{"CODEX_HOME": ""}), "/h"); got != filepath.Join("/h", ".codex") {
		t.Fatalf("got %q", got)
	}
	if got := CodexHomeFrom(envOf(nil), ""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestReadsTheShapeWithTheFileTimeAsReadAt(t *testing.T) {
	dir := t.TempDir()
	writeRollout(t, dir, "2026/10/23", "rollout-a.jsonl", []string{tokenLine(codexLimits(codexPrimary, codexSecondary))}, codexMtime)
	u := mustRead(t, dir)
	if u.Plan != PlanCodex || u.ReadAt != codexMtime || len(u.Windows) != 2 {
		t.Fatalf("got %+v", u)
	}
	if left, ok := u.LeftPercent(codexMtime); !ok || left != 75 {
		t.Fatalf("left %v %v", left, ok)
	}
}

func TestTheNewestFileByMtimeAcrossTwoDayFoldersWins(t *testing.T) {
	dir := t.TempDir()
	writeRollout(t, dir, "2026/10/22", "rollout-old.jsonl", []string{primaryOnly(90)}, codexMtime+50)
	writeRollout(t, dir, "2026/10/23", "rollout-new.jsonl", []string{primaryOnly(10)}, codexMtime)
	u := mustRead(t, dir)
	if u.Windows[0].UsedPercent != 90 || u.ReadAt != codexMtime+50 {
		t.Fatalf("got %+v", u)
	}
}

func TestAThirdDayFolderIsNotSearched(t *testing.T) {
	dir := t.TempDir()
	writeRollout(t, dir, "2026/10/21", "rollout-older.jsonl", []string{primaryOnly(90)}, codexMtime+99)
	writeRollout(t, dir, "2026/10/22", "rollout-a.jsonl", []string{primaryOnly(20)}, codexMtime+5)
	writeRollout(t, dir, "2026/10/23", "rollout-b.jsonl", []string{primaryOnly(10)}, codexMtime)
	if got := mustRead(t, dir).Windows[0].UsedPercent; got != 20 {
		t.Fatalf("got %v", got)
	}
}

func TestFolderNamesCompareAsNumbers(t *testing.T) {
	dir := t.TempDir()
	writeRollout(t, dir, "2026/10/1", "rollout-a.jsonl", []string{primaryOnly(10)}, codexMtime)
	writeRollout(t, dir, "2026/9/30", "rollout-b.jsonl", []string{primaryOnly(20)}, codexMtime-10)
	writeRollout(t, dir, "2026/2/28", "rollout-c.jsonl", []string{primaryOnly(30)}, codexMtime+10)
	if got := mustRead(t, dir).Windows[0].UsedPercent; got != 10 {
		t.Fatalf("got %v", got)
	}
}

func TestAFolderThatIsNotANumberIsIgnored(t *testing.T) {
	dir := t.TempDir()
	writeRollout(t, dir, "2026/10/23", "rollout-a.jsonl", []string{primaryOnly(10)}, codexMtime)
	writeRollout(t, dir, "archive/10/23", "rollout-b.jsonl", []string{primaryOnly(90)}, codexMtime+10)
	if got := mustRead(t, dir).Windows[0].UsedPercent; got != 10 {
		t.Fatalf("got %v", got)
	}
}

func TestFilesThatAreNotRolloutsAreNotRead(t *testing.T) {
	dir := t.TempDir()
	writeRollout(t, dir, "2026/10/23", "rollout-a.jsonl", []string{primaryOnly(10)}, codexMtime)
	writeRollout(t, dir, "2026/10/23", "notes.jsonl", []string{primaryOnly(90)}, codexMtime+10)
	writeRollout(t, dir, "2026/10/23", "rollout-b.txt", []string{primaryOnly(80)}, codexMtime+20)
	if got := mustRead(t, dir).Windows[0].UsedPercent; got != 10 {
		t.Fatalf("got %v", got)
	}
}

func TestANewestFileWithoutAUsableLineIsNone(t *testing.T) {
	dir := t.TempDir()
	writeRollout(t, dir, "2026/10/23", "rollout-a.jsonl", []string{primaryOnly(10)}, codexMtime)
	writeRollout(t, dir, "2026/10/23", "rollout-b.jsonl", []string{"{}"}, codexMtime+10)
	if u := ReadCodex(dir); u != nil {
		t.Fatalf("got %+v", u)
	}
}

func TestOnlyTheTailOfALargeFileIsSearched(t *testing.T) {
	dir := t.TempDir()
	filler := strings.Repeat("x", 1024)
	early := primaryOnly(10) + "\n" + strings.Repeat(filler, 300)
	late := strings.Repeat(filler, 300) + "\n" + primaryOnly(20)
	writeRollout(t, dir, "2026/10/22", "rollout-early.jsonl", []string{early}, codexMtime)
	if u := ReadCodex(dir); u != nil {
		t.Fatalf("got %+v", u)
	}
	writeRollout(t, dir, "2026/10/23", "rollout-late.jsonl", []string{late}, codexMtime+10)
	if got := mustRead(t, dir).Windows[0].UsedPercent; got != 20 {
		t.Fatalf("got %v", got)
	}
}

func TestMissingOrEmptySessionsIsNone(t *testing.T) {
	dir := t.TempDir()
	if u := ReadCodex(dir); u != nil {
		t.Fatalf("got %+v", u)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sessions/2026/10/23"), 0o755); err != nil {
		t.Fatal(err)
	}
	if u := ReadCodex(dir); u != nil {
		t.Fatalf("got %+v", u)
	}
}

func TestASessionsPathThatIsAFileIsNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sessions"), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if u := ReadCodex(dir); u != nil {
		t.Fatalf("got %+v", u)
	}
}

func TestAMissingRolloutIsAnErrorForTheTailReader(t *testing.T) {
	if _, err := readTail(filepath.Join(t.TempDir(), "rollout-gone.jsonl")); err == nil {
		t.Fatal("want error")
	}
}

// A junk line, then one token_count line padded to exactly tailBytes with its
// newline. The tail starts on that line, so the line is whole.
func TestATailThatStartsOnALineStartKeepsItsFirstLine(t *testing.T) {
	line := primaryOnly(33)
	padded := line + strings.Repeat(" ", tailBytes-1-len(line))
	path := filepath.Join(t.TempDir(), "rollout-aligned.jsonl")
	if err := os.WriteFile(path, []byte("junk\n"+padded+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tail, err := readTail(path)
	if err != nil {
		t.Fatal(err)
	}
	if w := mustWindows(t, tail); w[0].UsedPercent != 33 {
		t.Fatalf("got %v", w[0].UsedPercent)
	}
}

// A long run of p fills the file's first tailBytes, so the tail starts inside
// it. Only the run's partial line goes; the complete line after it stays.
func TestATailThatStartsInsideALineDropsThatPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-inside.jsonl")
	body := strings.Repeat("p", tailBytes) + "\n" + primaryOnly(44) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tail, err := readTail(path)
	if err != nil {
		t.Fatal(err)
	}
	if w := mustWindows(t, tail); w[0].UsedPercent != 44 {
		t.Fatalf("got %v", w[0].UsedPercent)
	}
	if strings.HasPrefix(tail, "p") {
		t.Fatal("tail starts with the partial line")
	}
}
