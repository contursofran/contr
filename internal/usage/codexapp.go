// Codex's app-server as a usage source (D209). `codex app-server` speaks
// JSON-RPC over stdio, one JSON message per line. CodexApp starts it, sends
// initialize, the initialized notification and account/rateLimits/read, closes
// stdin and waits for the process to end. contr reads no Codex file for this.

package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	contr "github.com/contursofran/contr"
)

// CodexAppTimeout is the longest a read from codex app-server can take. Then
// the process is killed with the processes it forked.
const CodexAppTimeout = 10 * time.Second

// The kinds of failure of an app-server read. Each kind is logged once until a
// read succeeds.
const (
	codexNotFound    = "not found"
	codexNoStart     = "no start"
	codexErrorAnswer = "error answer"
	codexBadAnswer   = "bad answer"
	codexTimeout     = "timeout"
	codexNoLimits    = "no limits"
)

// codexAppError is a failed app-server read and its kind.
type codexAppError struct {
	kind string
	err  error
}

func (e *codexAppError) Error() string { return e.kind + ": " + e.err.Error() }

func (e *codexAppError) Unwrap() error { return e.err }

func failed(kind string, err error) error { return &codexAppError{kind: kind, err: err} }

// errCodexNotFound is the error when no codex program is found.
var errCodexNotFound = errors.New("codex: program not found on PATH or on the login shell's PATH")

// CodexApp reads Codex's plan limits from `codex app-server`. The zero value
// finds codex at each read and uses [CodexAppTimeout]. Use one CodexApp for
// all reads, so a failure is logged once and not at each read.
type CodexApp struct {
	// Program is the codex program; empty finds it (see findCodex).
	Program string
	// Timeout is the limit of one read; zero is CodexAppTimeout.
	Timeout time.Duration

	// shellPATH returns the PATH of the owner's login shell; nil is
	// loginShellPATH. Tests replace it.
	shellPATH func() string
	// warn logs a failure; nil is slog.Warn. Tests replace it.
	warn func(kind string, err error)

	mu     sync.Mutex
	logged map[string]bool
}

// Read returns Codex's usage, or nil when app-server gives none. It blocks:
// run it on a goroutine.
func (a *CodexApp) Read() *PlanUsage {
	windows, err := a.read()
	if err != nil {
		a.logOnce(err)
		return nil
	}
	a.mu.Lock()
	a.logged = nil
	a.mu.Unlock()
	return &PlanUsage{Plan: PlanCodex, Windows: windows, ReadAt: UnixNow()}
}

func (a *CodexApp) read() ([]Window, error) {
	program, env, err := a.find()
	if err != nil {
		return nil, err
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = CodexAppTimeout
	}
	return readAppServer(program, env, timeout)
}

// logOnce logs err when its kind was not logged since the last good read.
func (a *CodexApp) logOnce(err error) {
	kind := codexBadAnswer
	var e *codexAppError
	if errors.As(err, &e) {
		kind = e.kind
	}
	a.mu.Lock()
	seen := a.logged[kind]
	if !seen {
		if a.logged == nil {
			a.logged = map[string]bool{}
		}
		a.logged[kind] = true
	}
	a.mu.Unlock()
	if seen {
		return
	}
	if a.warn != nil {
		a.warn(kind, err)
		return
	}
	slog.Warn("unable to read the Codex plan limits from codex app-server", "kind", kind, "err", err)
}

// find returns the codex program and the environment to run it in. Program,
// when set, runs in this process's environment. Else codex is searched on
// PATH, then on the login shell's PATH: an app opened from Finder has only the
// short launchd PATH. A codex found on the shell's PATH runs with that PATH,
// so its interpreter (node) is found too.
func (a *CodexApp) find() (string, []string, error) {
	if a.Program != "" {
		return a.Program, os.Environ(), nil
	}
	if p := lookIn("codex", os.Getenv("PATH")); p != "" {
		return p, os.Environ(), nil
	}
	shellPATH := a.shellPATH
	if shellPATH == nil {
		shellPATH = loginShellPATH
	}
	list := shellPATH()
	if p := lookIn("codex", list); p != "" {
		return p, withPATH(os.Environ(), list), nil
	}
	return "", nil, failed(codexNotFound, errCodexNotFound)
}

// lookIn returns the first executable file name in the folders of list, a
// PATH value, or "". Relative folders are skipped.
func lookIn(name, list string) string {
	for _, dir := range filepath.SplitList(list) {
		if !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// withPATH returns env with PATH set to list.
func withPATH(env []string, list string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+list)
}

// pathMarker starts the line on which the login shell prints its PATH, so the
// output of the owner's shell files is skipped.
const pathMarker = "__CONTR_PATH__="

// loginShellPATH is the PATH of the owner's login shell ($SHELL, else
// /bin/zsh), read once per process. It is "" when the shell gives none.
var loginShellPATH = sync.OnceValue(func() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	return readShellPATH(shell, CodexAppTimeout)
})

// readShellPATH runs shell as an interactive login shell, which reads the
// files that set PATH (version managers often set it in the rc file), and
// returns the PATH it prints, or "".
func readShellPATH(shell string, timeout time.Duration) string {
	script := `printf '\n` + pathMarker + `%s\n' "$PATH"`
	cmd := exec.Command(shell, "-l", "-i", "-c", script)
	isolate(cmd)
	out, err := outputCapped(cmd, timeout)
	if err != nil {
		slog.Warn("unable to read the login shell's PATH", "shell", shell, "err", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), pathMarker); ok {
			return v
		}
	}
	return ""
}

// outputCapped runs cmd with no stdin and returns its stdout. A command that
// runs past timeout is killed with the processes it forked.
func outputCapped(cmd *exec.Cmd, timeout time.Duration) ([]byte, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	out := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(stdout)
		out <- b
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case b := <-out:
		return b, cmd.Wait()
	case <-timer.C:
		kill(cmd)
		_ = cmd.Wait()
		return <-out, fmt.Errorf("ran past %v; killed", timeout)
	}
}

func kill(cmd *exec.Cmd) {
	killGroup(cmd)
	_ = cmd.Process.Kill()
}

// readAppServer runs `program app-server` in env and returns the plan's limit
// windows. It closes stdin after the answer and waits for the process to end.
// After timeout the process is killed; an answer that came before is kept.
func readAppServer(program string, env []string, timeout time.Duration) ([]Window, error) {
	cmd := exec.Command(program, "app-server")
	cmd.Env = env
	isolate(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, failed(codexNoStart, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, failed(codexNoStart, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, failed(codexNoStart, err)
	}
	type result struct {
		windows []Window
		err     error
	}
	done := make(chan result, 1)
	go func() {
		w, err := talk(stdin, stdout)
		done <- result{w, err}
	}()
	exited := func() chan struct{} {
		c := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(c)
		}()
		return c
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	select {
	case r := <-done:
		_ = stdin.Close()
		end := exited()
		select {
		case <-end:
		case <-deadline.C:
			kill(cmd)
			<-end
		}
		return r.windows, r.err
	case <-deadline.C:
		_ = stdin.Close()
		kill(cmd)
		<-exited() // Wait closes stdout, so talk ends.
		return nil, failed(codexTimeout, fmt.Errorf("no answer in %v; killed", timeout))
	}
}

// rpcMessage is any message from app-server: an answer, a notification or a
// request.
type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int64  `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// talk does the handshake, asks for the rate limits and returns the windows.
func talk(w io.Writer, r io.Reader) ([]Window, error) {
	enc := json.NewEncoder(w) // Encode ends each message with a newline.
	dec := json.NewDecoder(r)
	send := func(msg any) error {
		if err := enc.Encode(msg); err != nil {
			return failed(codexBadAnswer, err)
		}
		return nil
	}
	initialize := map[string]any{
		"id": 1, "method": "initialize",
		"params": map[string]any{"clientInfo": map[string]string{"name": "contr", "title": "contr", "version": contr.Version()}},
	}
	if err := send(initialize); err != nil {
		return nil, err
	}
	if _, err := answerTo(dec, 1); err != nil {
		return nil, err
	}
	if err := send(map[string]string{"method": "initialized"}); err != nil {
		return nil, err
	}
	if err := send(map[string]any{"id": 2, "method": "account/rateLimits/read"}); err != nil {
		return nil, err
	}
	result, err := answerTo(dec, 2)
	if err != nil {
		return nil, err
	}
	return parseRateLimits(result)
}

// answerTo reads messages until the answer to request id and returns its
// result. Notifications and requests from app-server are skipped.
func answerTo(dec *json.Decoder, id int) (json.RawMessage, error) {
	want := strconv.Itoa(id)
	for {
		var msg rpcMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("app-server ended before it answered")
			}
			return nil, failed(codexBadAnswer, err)
		}
		if msg.Method != "" || string(msg.ID) != want {
			continue
		}
		if msg.Error != nil {
			return nil, failed(codexErrorAnswer, fmt.Errorf("%s (code %d)", msg.Error.Message, msg.Error.Code))
		}
		return msg.Result, nil
	}
}

// appRateLimits is the result of account/rateLimits/read.
type appRateLimits struct {
	RateLimits *appSnapshot            `json:"rateLimits"`
	ByLimitID  map[string]*appSnapshot `json:"rateLimitsByLimitId"`
}

type appSnapshot struct {
	LimitID   *string    `json:"limitId"`
	Primary   *appWindow `json:"primary"`
	Secondary *appWindow `json:"secondary"`
}

// appWindow reads the numbers as floats, so wholeSeconds can check them.
type appWindow struct {
	UsedPercent *float64 `json:"usedPercent"`
	ResetsAt    *float64 `json:"resetsAt"`
}

// parseRateLimits returns the windows of the "codex" limit: the entry
// "codex" of rateLimitsByLimitId, else rateLimits when its limitId is absent
// or "codex". primary and secondary become the kinds of the same names when
// they have a percent; resetsAt is Unix seconds.
func parseRateLimits(result json.RawMessage) ([]Window, error) {
	var limits appRateLimits
	if err := json.Unmarshal(result, &limits); err != nil {
		return nil, failed(codexBadAnswer, err)
	}
	snap := limits.ByLimitID["codex"]
	if snap == nil && limits.RateLimits != nil && (limits.RateLimits.LimitID == nil || *limits.RateLimits.LimitID == "codex") {
		snap = limits.RateLimits
	}
	var windows []Window
	if snap != nil {
		for _, w := range []struct {
			kind string
			raw  *appWindow
		}{{"primary", snap.Primary}, {"secondary", snap.Secondary}} {
			if w.raw == nil || w.raw.UsedPercent == nil {
				continue
			}
			windows = append(windows, Window{Kind: w.kind, UsedPercent: *w.raw.UsedPercent, ResetsAt: wholeSeconds(w.raw.ResetsAt)})
		}
	}
	if len(windows) == 0 {
		return nil, failed(codexNoLimits, errors.New("the answer has no codex limit window"))
	}
	return windows, nil
}
