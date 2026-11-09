package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The test binary is the fake codex when fakeCodexMode is set: it speaks the
// app-server protocol on stdio. fakeCodexDone names a file that it writes, with
// its PATH, once its stdin ends.
const (
	fakeCodexMode = "CONTR_TEST_FAKE_CODEX"
	fakeCodexDone = "CONTR_TEST_FAKE_CODEX_DONE"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeCodexMode); mode != "" {
		os.Exit(fakeCodex(mode))
	}
	os.Exit(m.Run())
}

// fakeResetsAt is the reset time of the fake's primary window.
const fakeResetsAt = 4_000_000_000

// fakeCodex answers as `codex app-server` does. mode is "good", "error",
// "badjson", "exit" or "hang".
func fakeCodex(mode string) int {
	if len(os.Args) < 2 || os.Args[1] != "app-server" {
		fmt.Fprintln(os.Stderr, "fake codex: want app-server, got", os.Args[1:])
		return 2
	}
	in := bufio.NewScanner(os.Stdin)
	next := func() map[string]any {
		if !in.Scan() {
			return nil
		}
		var msg map[string]any
		if err := json.Unmarshal(in.Bytes(), &msg); err != nil {
			return nil
		}
		return msg
	}
	say := func(line string) { fmt.Println(line) }

	init := next()
	params, _ := init["params"].(map[string]any)
	client, _ := params["clientInfo"].(map[string]any)
	if init["method"] != "initialize" || init["id"] != 1.0 || client["name"] != "contr" || client["version"] == "" {
		fmt.Fprintln(os.Stderr, "fake codex: bad initialize", init)
		return 3
	}
	switch mode {
	case "hang":
		time.Sleep(time.Hour)
		return 0
	case "badjson":
		say("not json")
		return 0
	}
	say(`{"method":"remoteControl/status/changed","params":{"status":"disabled"}}`)
	say(`{"id":1,"result":{"userAgent":"contr/0.0.0","platformOs":"macos"}}`)
	if msg := next(); msg["method"] != "initialized" || msg["id"] != nil {
		fmt.Fprintln(os.Stderr, "fake codex: want initialized, got", msg)
		return 3
	}
	if msg := next(); msg["method"] != "account/rateLimits/read" || msg["id"] != 2.0 {
		fmt.Fprintln(os.Stderr, "fake codex: want account/rateLimits/read, got", msg)
		return 3
	}
	switch mode {
	case "exit":
		return 0
	case "error":
		say(`{"error":{"code":-32600,"message":"codex account authentication required to read rate limits"},"id":2}`)
	default:
		say(`{"method":"account/rateLimits/updated","params":{}}`)
		say(fmt.Sprintf(`{"id":2,"result":{"rateLimits":{"limitId":"other","primary":{"usedPercent":99}},`+
			`"rateLimitsByLimitId":{"codex":{"limitId":"codex","primary":{"usedPercent":12,"windowDurationMins":300,"resetsAt":%d},`+
			`"secondary":{"usedPercent":40,"windowDurationMins":10080,"resetsAt":null}}}}}`, fakeResetsAt))
	}
	for in.Scan() { // Wait for the end of stdin.
	}
	if done := os.Getenv(fakeCodexDone); done != "" {
		_ = os.WriteFile(done, []byte(os.Getenv("PATH")), 0o644)
	}
	return 0
}

// fakeCodexIn links a program named codex in a new folder to the test binary
// and returns the folder.
func fakeCodexIn(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(exe, filepath.Join(dir, "codex")); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fakeApp returns a CodexApp that runs the test binary as codex in mode, and
// the path of the file the fake writes when its stdin ends.
func fakeApp(t *testing.T, mode string) (*CodexApp, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	done := filepath.Join(t.TempDir(), "done")
	t.Setenv(fakeCodexMode, mode)
	t.Setenv(fakeCodexDone, done)
	t.Setenv("GORACE", "atexit_sleep_ms=0") // a race build waits 1 s at exit
	return &CodexApp{Program: exe, Timeout: 20 * time.Second}, done
}

// failKind returns the kind of a failed read, or "" when err is nil.
func failKind(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var e *codexAppError
	if !errors.As(err, &e) {
		t.Fatalf("error %v has no kind", err)
	}
	return e.kind
}

func TestCodexAppReadsTheCodexLimitWindows(t *testing.T) {
	app, done := fakeApp(t, "good")
	u := app.Read()
	if u == nil {
		_, err := app.read()
		t.Fatalf("no usage: %v", err)
	}
	reset := uint64(fakeResetsAt)
	want := []Window{{Kind: "primary", UsedPercent: 12, ResetsAt: &reset}, {Kind: "secondary", UsedPercent: 40}}
	if u.Plan != PlanCodex || !reflect.DeepEqual(u.Windows, want) {
		t.Errorf("usage = %+v, windows %+v", u, u.Windows)
	}
	if now := UnixNow(); u.ReadAt+5 < now || u.ReadAt > now {
		t.Errorf("ReadAt = %d, now %d", u.ReadAt, now)
	}
	if _, err := os.Stat(done); err != nil {
		t.Errorf("codex did not see the end of stdin before Read returned: %v", err)
	}
}

func TestCodexAppFailures(t *testing.T) {
	cases := []struct {
		mode, kind, text string
	}{
		{"error", codexErrorAnswer, "authentication required"},
		{"badjson", codexBadAnswer, "invalid character"},
		{"exit", codexBadAnswer, "ended before it answered"},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			app, _ := fakeApp(t, c.mode)
			_, err := app.read()
			if got := failKind(t, err); got != c.kind || !strings.Contains(err.Error(), c.text) {
				t.Errorf("err = %v (kind %q), want kind %q with %q", err, got, c.kind, c.text)
			}
			if u := app.Read(); u != nil {
				t.Errorf("usage = %+v, want nil", u)
			}
		})
	}
}

func TestCodexAppKillsCodexAfterTheTimeLimit(t *testing.T) {
	app, _ := fakeApp(t, "hang")
	app.Timeout = 300 * time.Millisecond
	start := time.Now()
	_, err := app.read()
	if got := failKind(t, err); got != codexTimeout {
		t.Errorf("err = %v, want kind %q", err, codexTimeout)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("read took %v", took)
	}
}

func TestCodexAppMissingProgramDoesNotStart(t *testing.T) {
	app := &CodexApp{Program: filepath.Join(t.TempDir(), "codex")}
	_, err := app.read()
	if got := failKind(t, err); got != codexNoStart {
		t.Errorf("err = %v, want kind %q", err, codexNoStart)
	}
}

func TestCodexAppNotFoundOnPathOrShellPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	app := &CodexApp{shellPATH: func() string { return t.TempDir() }}
	_, err := app.read()
	if got := failKind(t, err); got != codexNotFound {
		t.Errorf("err = %v, want kind %q", err, codexNotFound)
	}
}

func TestCodexAppFindsCodexOnPathFirst(t *testing.T) {
	dir := fakeCodexIn(t)
	_, done := fakeApp(t, "good")
	t.Setenv("PATH", dir)
	app := &CodexApp{shellPATH: func() string {
		t.Error("the login shell is asked though codex is on PATH")
		return ""
	}}
	if u := app.Read(); u == nil {
		t.Fatal("no usage")
	}
	if b, _ := os.ReadFile(done); string(b) != dir {
		t.Errorf("codex PATH = %q, want %q", b, dir)
	}
}

func TestCodexAppFindsCodexOnTheLoginShellPath(t *testing.T) {
	dir := fakeCodexIn(t)
	_, done := fakeApp(t, "good")
	t.Setenv("PATH", t.TempDir())
	shellPATH := "relative:" + dir
	app := &CodexApp{shellPATH: func() string { return shellPATH }}
	if u := app.Read(); u == nil {
		t.Fatal("no usage")
	}
	if b, _ := os.ReadFile(done); string(b) != shellPATH {
		t.Errorf("codex PATH = %q, want the shell's %q", b, shellPATH)
	}
}

func TestCodexAppLogsEachKindOnceUntilAGoodRead(t *testing.T) {
	var logs []string
	app, _ := fakeApp(t, "error")
	app.warn = func(kind string, err error) { logs = append(logs, kind) }
	app.Read()
	app.Read()
	t.Setenv(fakeCodexMode, "exit")
	app.Read()
	app.Read()
	if want := []string{codexErrorAnswer, codexBadAnswer}; !reflect.DeepEqual(logs, want) {
		t.Fatalf("logs = %v, want %v", logs, want)
	}
	t.Setenv(fakeCodexMode, "good")
	if app.Read() == nil {
		t.Fatal("no usage")
	}
	t.Setenv(fakeCodexMode, "error")
	app.Read()
	if want := []string{codexErrorAnswer, codexBadAnswer, codexErrorAnswer}; !reflect.DeepEqual(logs, want) {
		t.Errorf("logs after a good read = %v, want %v", logs, want)
	}
}

func TestParseRateLimits(t *testing.T) {
	at := func(v uint64) *uint64 { return &v }
	cases := []struct {
		name, result string
		want         []Window
	}{
		{"codex bucket wins", `{"rateLimits":{"primary":{"usedPercent":90}},"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":5,"resetsAt":100}}}}`,
			[]Window{{Kind: "primary", UsedPercent: 5, ResetsAt: at(100)}}},
		{"rateLimits without limitId", `{"rateLimits":{"primary":{"usedPercent":7},"secondary":{"usedPercent":30,"resetsAt":200}}}`,
			[]Window{{Kind: "primary", UsedPercent: 7}, {Kind: "secondary", UsedPercent: 30, ResetsAt: at(200)}}},
		{"rateLimits of codex", `{"rateLimits":{"limitId":"codex","secondary":{"usedPercent":3}},"rateLimitsByLimitId":null}`,
			[]Window{{Kind: "secondary", UsedPercent: 3}}},
		{"negative reset is no time", `{"rateLimits":{"primary":{"usedPercent":7,"resetsAt":-1}}}`,
			[]Window{{Kind: "primary", UsedPercent: 7}}},
		{"window without a percent", `{"rateLimits":{"primary":{"resetsAt":100},"secondary":{"usedPercent":8}}}`,
			[]Window{{Kind: "secondary", UsedPercent: 8}}},
	}
	for _, c := range cases {
		got, err := parseRateLimits(json.RawMessage(c.result))
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}
	for name, result := range map[string]string{
		"other limit":  `{"rateLimits":{"limitId":"other","primary":{"usedPercent":7}}}`,
		"no windows":   `{"rateLimits":{"primary":null,"secondary":null}}`,
		"empty result": `{}`,
	} {
		if _, err := parseRateLimits(json.RawMessage(result)); failKind(t, err) != codexNoLimits {
			t.Errorf("%s: err = %v, want kind %q", name, err, codexNoLimits)
		}
	}
	if _, err := parseRateLimits(json.RawMessage(`[1]`)); failKind(t, err) != codexBadAnswer {
		t.Errorf("not an object: err = %v, want kind %q", err, codexBadAnswer)
	}
}

func TestReadShellPATHTakesTheMarkedLine(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "sh")
	script := "#!/bin/sh\n[ \"$1 $2 $3\" = '-l -i -c' ] || exit 1\necho 'motd: hello'\neval \"$4\"\necho after\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/x/bin:/y/bin")
	if got := readShellPATH(shell, 10*time.Second); got != "/x/bin:/y/bin" {
		t.Errorf("PATH = %q", got)
	}
}

func TestReadShellPATHGivesNothingForASlowOrMissingShell(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "sh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if got := readShellPATH(shell, 300*time.Millisecond); got != "" {
		t.Errorf("slow shell: PATH = %q", got)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("slow shell took %v", took)
	}
	if got := readShellPATH(filepath.Join(t.TempDir(), "none"), time.Second); got != "" {
		t.Errorf("missing shell: PATH = %q", got)
	}
}

func TestReadPlansTakesCodexFromAppServerElseTheRollout(t *testing.T) {
	home := t.TempDir()
	folder := filepath.Join(home, "sessions/2026/10/23")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "rollout-a.jsonl"), []byte(codexLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromApp := &PlanUsage{Plan: PlanCodex, Windows: []Window{{Kind: "primary", UsedPercent: 70}}}
	none := func() *PlanUsage { return nil }
	app := func() *PlanUsage { return fromApp }

	if l, _ := leftIn(ReadPlans("", home, app), PlanCodex, UnixNow()); l != 30 {
		t.Errorf("app-server data: left = %v, want 30", l)
	}
	if l, _ := leftIn(ReadPlans("", home, none), PlanCodex, UnixNow()); l != 96 {
		t.Errorf("rollout fallback: left = %v, want 96", l)
	}
	if l, _ := leftIn(ReadPlans("", "", app), PlanCodex, UnixNow()); l != 30 {
		t.Errorf("no codex home: left = %v, want 30", l)
	}
	if _, ok := leftIn(ReadPlans("", "", none), PlanCodex, UnixNow()); ok {
		t.Error("no source: want no data")
	}
}
