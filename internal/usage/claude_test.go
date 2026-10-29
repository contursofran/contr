package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const (
	now = 1_761_200_000
	doc = `{"version":"2.1.309","rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1761210000},"seven_day":{"used_percentage":41.2,"resets_at":1761700000}}}`
)

func str(s string) *string { return &s }

func snapshotOf(windows ...Window) *ClaudeSnapshot {
	return &ClaudeSnapshot{Schema: schema, UpdatedAt: now, ClaudeVersion: str("2.1.309"), Windows: windows}
}

func docSnapshot() *ClaudeSnapshot {
	return snapshotOf(window("five_hour", 23.5, u64(1_761_210_000)), window("seven_day", 41.2, u64(1_761_700_000)))
}

// broken is a stdin that always fails, to test the read error path.
type broken struct{}

func (broken) Read([]byte) (int, error) { return 0, errors.New("stdin broke") }

// counting is a reader that counts the bytes it gives out.
type counting struct {
	inner io.Reader
	read  int64
}

func (c *counting) Read(p []byte) (int, error) {
	n, err := c.inner.Read(p)
	c.read += int64(n)
	return n, err
}

// repeatSpace gives endless spaces.
type repeatSpace struct{}

func (repeatSpace) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

func mustWrite(t *testing.T, dir string, snap *ClaudeSnapshot) bool {
	t.Helper()
	wrote, err := WriteSnapshot(dir, snap)
	if err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	return wrote
}

func mustRun(t *testing.T, in io.Reader, then []string, dir string, timeout time.Duration) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := runWith(in, &out, then, dir, now, timeout); err != nil {
		t.Fatalf("runWith: %v", err)
	}
	return out.Bytes()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestParsesBothWindowsAndTheVersion(t *testing.T) {
	snap := ParseStatusline(doc, now)
	if !reflect.DeepEqual(snap, docSnapshot()) {
		t.Fatalf("got %+v", snap)
	}
}

func TestParsesOnlyFiveHour(t *testing.T) {
	snap := ParseStatusline(`{"rate_limits":{"five_hour":{"used_percentage":10,"resets_at":5}}}`, now)
	if snap == nil {
		t.Fatal("want five_hour")
	}
	if !reflect.DeepEqual(snap.Windows, []Window{window("five_hour", 10, u64(5))}) || snap.ClaudeVersion != nil {
		t.Fatalf("got %+v", snap)
	}
}

func TestAWindowWithoutAPercentIsSkipped(t *testing.T) {
	snap := ParseStatusline(`{"rate_limits":{"five_hour":{"resets_at":5},"seven_day":{"used_percentage":7}}}`, now)
	if snap == nil || !reflect.DeepEqual(snap.Windows, []Window{window("seven_day", 7, nil)}) {
		t.Fatalf("got %+v", snap)
	}
}

func TestInputWithoutRateLimitsIsNone(t *testing.T) {
	if snap := ParseStatusline(`{"model":{}}`, now); snap != nil {
		t.Fatalf("got %+v", snap)
	}
}

func TestRateLimitsWithoutAUsableWindowIsNone(t *testing.T) {
	if snap := ParseStatusline(`{"rate_limits":{"seven_day":{"resets_at":5}}}`, now); snap != nil {
		t.Fatalf("got %+v", snap)
	}
}

func TestInvalidJSONIsNone(t *testing.T) {
	if snap := ParseStatusline("not json", now); snap != nil {
		t.Fatalf("got %+v", snap)
	}
}

func TestANonTextVersionIsIgnored(t *testing.T) {
	snap := ParseStatusline(`{"version":2,"rate_limits":{"five_hour":{"used_percentage":1}}}`, now)
	if snap == nil || snap.ClaudeVersion != nil {
		t.Fatalf("got %+v", snap)
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	if !mustWrite(t, dir, docSnapshot()) {
		t.Fatal("want a write")
	}
	u := ReadClaude(dir)
	if u == nil {
		t.Fatal("no usage")
	}
	if u.Plan != PlanClaude || !reflect.DeepEqual(u.Windows, docSnapshot().Windows) || u.ReadAt != now {
		t.Fatalf("got %+v", u)
	}
}

func TestTheFileHasTheDocumentedShape(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, docSnapshot())
	text, err := os.ReadFile(filepath.Join(dir, ClaudeFile))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Schema        int    `json:"schema"`
		UpdatedAt     uint64 `json:"updated_at"`
		ClaudeVersion string `json:"claude_version"`
		Windows       []struct {
			Kind     string `json:"kind"`
			ResetsAt uint64 `json:"resets_at"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(text, &v); err != nil {
		t.Fatal(err)
	}
	if v.Schema != 1 || v.UpdatedAt != now || v.ClaudeVersion != "2.1.309" ||
		v.Windows[0].Kind != "five_hour" || v.Windows[1].ResetsAt != 1_761_700_000 {
		t.Fatalf("got %+v", v)
	}
}

func TestIdenticalWindowsAreNotRewritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ClaudeFile)
	if !mustWrite(t, dir, docSnapshot()) {
		t.Fatal("first write")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	later := docSnapshot()
	later.UpdatedAt = now + 60
	if mustWrite(t, dir, later) {
		t.Fatal("second write happened")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("file was touched")
	}
	if got := ReadClaude(dir).ReadAt; got != now {
		t.Fatalf("ReadAt = %d", got)
	}
}

func TestChangedWindowsAreWritten(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, docSnapshot())
	changed := snapshotOf(window("five_hour", 50, u64(1_761_210_000)))
	if !mustWrite(t, dir, changed) {
		t.Fatal("second write skipped")
	}
	if got := ReadClaude(dir).Windows; !reflect.DeepEqual(got, changed.Windows) {
		t.Fatalf("got %+v", got)
	}
}

func TestAWriteIntoABlockedFolderIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "usage"), []byte("a file, not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSnapshot(dir, docSnapshot()); err == nil {
		t.Fatal("want an error")
	}
}

func TestARenameOntoAFolderIsAnErrorAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ClaudeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSnapshot(dir, docSnapshot()); err == nil {
		t.Fatal("want an error")
	}
	// Only the folder itself is left in usage/; the temp file was removed.
	entries, err := os.ReadDir(filepath.Join(dir, "usage"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries %v, err %v", entries, err)
	}
}

func TestMissingFileIsNone(t *testing.T) {
	if u := ReadClaude(t.TempDir()); u != nil {
		t.Fatalf("got %+v", u)
	}
}

func TestCorruptFileIsNoneAndIsReplacedOnWrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "usage"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ClaudeFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ReadClaude(dir) != nil {
		t.Fatal("want none")
	}
	if !mustWrite(t, dir, docSnapshot()) {
		t.Fatal("want a write")
	}
	if ReadClaude(dir) == nil {
		t.Fatal("want usage")
	}
}

func TestAnotherSchemaIsNone(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "usage"), 0o755); err != nil {
		t.Fatal(err)
	}
	newer := docSnapshot()
	newer.Schema = 2
	data, err := json.Marshal(newer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ClaudeFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if ReadClaude(dir) != nil {
		t.Fatal("want none")
	}
}

func TestStatuslineSavesAndPrintsNothingWithoutACommand(t *testing.T) {
	dir := t.TempDir()
	if out := mustRun(t, bytes.NewReader([]byte(doc)), nil, dir, chainTimeout); len(out) != 0 {
		t.Fatalf("out %q", out)
	}
	if ReadClaude(dir) == nil {
		t.Fatal("not saved")
	}
}

func TestInputWithoutLimitsSavesNothing(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, bytes.NewReader([]byte(`{"model":{}}`)), nil, dir, chainTimeout)
	if exists(filepath.Join(dir, ClaudeFile)) {
		t.Fatal("file written")
	}
}

func TestChainedCommandGetsTheSameInputAndItsOutputIsCopied(t *testing.T) {
	dir := t.TempDir()
	out := mustRun(t, bytes.NewReader([]byte(doc)), []string{"cat"}, dir, chainTimeout)
	if string(out) != doc {
		t.Fatalf("out %q", out)
	}
	if ReadClaude(dir) == nil {
		t.Fatal("not saved")
	}
}

func TestAMissingCommandPrintsNothingAndStillSaves(t *testing.T) {
	dir := t.TempDir()
	out := mustRun(t, bytes.NewReader([]byte(doc)), []string{"contr-no-such-command"}, dir, chainTimeout)
	if len(out) != 0 {
		t.Fatalf("out %q", out)
	}
	if ReadClaude(dir) == nil {
		t.Fatal("not saved")
	}
}

func TestACommandPastItsLimitIsKilled(t *testing.T) {
	started := time.Now()
	out := mustRun(t, bytes.NewReader([]byte(doc)), []string{"sleep", "5"}, "", 200*time.Millisecond)
	if len(out) != 0 {
		t.Fatalf("out %q", out)
	}
	if time.Since(started) >= 4*time.Second {
		t.Fatal("too slow")
	}
}

func TestAForkingCommandReturnsWithinTheCap(t *testing.T) {
	started := time.Now()
	out := mustRun(t, bytes.NewReader([]byte(doc)), []string{"sh", "-c", "echo early; sleep 5; echo late"}, "", chainTimeout)
	if string(out) != "early\n" {
		t.Fatalf("out %q", out)
	}
	if time.Since(started) >= 3*time.Second {
		t.Fatal("too slow")
	}
}

func TestAForkingCommandIsKilledAtAShortLimit(t *testing.T) {
	started := time.Now()
	out := mustRun(t, bytes.NewReader([]byte(doc)), []string{"sh", "-c", "sleep 5; echo x"}, "", 200*time.Millisecond)
	if len(out) != 0 {
		t.Fatalf("out %q", out)
	}
	if time.Since(started) >= 2*time.Second {
		t.Fatal("too slow")
	}
}

func TestAnInputOverTheCapIsSkippedWhole(t *testing.T) {
	dir := t.TempDir()
	// A valid document followed by spaces, past the cap. Without the cap it
	// would be saved and passed on.
	in := &counting{inner: io.MultiReader(bytes.NewReader([]byte(doc)), io.LimitReader(repeatSpace{}, maxInput))}
	out := mustRun(t, in, []string{"cat"}, dir, chainTimeout)
	if in.read != maxInput+1 {
		t.Fatalf("read %d", in.read)
	}
	if len(out) != 0 || exists(filepath.Join(dir, ClaudeFile)) {
		t.Fatalf("out %d bytes, file %v", len(out), exists(filepath.Join(dir, ClaudeFile)))
	}
}

func TestAnInputOfExactlyTheCapIsUsed(t *testing.T) {
	dir := t.TempDir()
	input := append([]byte(doc), bytes.Repeat([]byte{' '}, maxInput-len(doc))...)
	out := mustRun(t, bytes.NewReader(input), []string{"cat"}, dir, chainTimeout)
	if !bytes.Equal(out, input) {
		t.Fatalf("out %d bytes", len(out))
	}
	if ReadClaude(dir) == nil {
		t.Fatal("not saved")
	}
}

func TestABlockedFolderDoesNotStopTheChainedCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "usage"), []byte("a file, not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := mustRun(t, bytes.NewReader([]byte(doc)), []string{"cat"}, dir, chainTimeout); string(out) != doc {
		t.Fatalf("out %q", out)
	}
}

func TestNoStateDirStillRunsTheChainedCommand(t *testing.T) {
	if out := mustRun(t, bytes.NewReader([]byte(doc)), []string{"cat"}, "", chainTimeout); string(out) != doc {
		t.Fatalf("out %q", out)
	}
}

func TestAFailingStdinIsAnError(t *testing.T) {
	var out bytes.Buffer
	if err := runWith(broken{}, &out, nil, "", now, chainTimeout); err == nil {
		t.Fatal("want an error")
	}
}
