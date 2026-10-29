// Claude Code's status line as a usage source. Claude Code pipes a JSON
// document to the `statusLine` command after each update. RunStatusline keeps
// the plan's limit windows from it in contr's own file, and ReadClaude reads
// that file back. contr never reads Claude Code's files.

package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"time"
)

// ClaudeFile is the usage file, under the state dir.
const ClaudeFile = "usage/claude.json"

const (
	// schema of ClaudeFile. Other values are ignored.
	schema = 1
	// maxInput is the largest status line input used. The real input is a few
	// KB. A longer input is skipped whole: nothing is saved and the chained
	// command does not run.
	maxInput = 1024 * 1024
	// chainTimeout is the time the chained command gets before it is killed.
	chainTimeout = 2 * time.Second
	// drain is how long the output of a killed command is still collected.
	// Output the command wrote before the kill is already in the pipe.
	drain = 100 * time.Millisecond
)

// ClaudeSnapshot is what ClaudeFile holds.
type ClaudeSnapshot struct {
	Schema uint32 `json:"schema"`
	// UpdatedAt is the Unix second when the windows last changed on disk.
	UpdatedAt     uint64   `json:"updated_at"`
	ClaudeVersion *string  `json:"claude_version"`
	Windows       []Window `json:"windows"`
}

// statusLine holds the parts of Claude Code's status line input that contr
// reads.
type statusLine struct {
	Version    any         `json:"version"`
	RateLimits *rateLimits `json:"rate_limits"`
}

type rateLimits struct {
	FiveHour *rawWindow `json:"five_hour"`
	SevenDay *rawWindow `json:"seven_day"`
}

type rawWindow struct {
	UsedPercentage *float64 `json:"used_percentage"`
	ResetsAt       *uint64  `json:"resets_at"`
}

// ParseStatusline reads the limit windows from Claude Code's status line JSON.
// It returns nil when rate_limits is absent or no window has a usage percent.
func ParseStatusline(stdinJSON string, now uint64) *ClaudeSnapshot {
	var input statusLine
	if err := json.Unmarshal([]byte(stdinJSON), &input); err != nil || input.RateLimits == nil {
		return nil
	}
	var windows []Window
	for _, c := range []struct {
		kind string
		raw  *rawWindow
	}{
		{"five_hour", input.RateLimits.FiveHour},
		{"seven_day", input.RateLimits.SevenDay},
	} {
		if c.raw == nil || c.raw.UsedPercentage == nil {
			continue
		}
		windows = append(windows, Window{Kind: c.kind, UsedPercent: *c.raw.UsedPercentage, ResetsAt: c.raw.ResetsAt})
	}
	if len(windows) == 0 {
		return nil
	}
	snap := &ClaudeSnapshot{Schema: schema, UpdatedAt: now, Windows: windows}
	if v, ok := input.Version.(string); ok {
		snap.ClaudeVersion = &v
	}
	return snap
}

// WriteSnapshot writes snap to dir/ClaudeFile, unless the file already holds
// the same windows. It reports whether it wrote. The write goes through a temp
// file and a rename, so a reader never sees a half-written file.
func WriteSnapshot(dir string, snap *ClaudeSnapshot) (bool, error) {
	path := filepath.Join(dir, ClaudeFile)
	if old := readSnapshot(path); old != nil && reflect.DeepEqual(old.Windows, snap.Windows) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return false, err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// ReadClaude returns Claude's usage as last written, or nil when the file is
// missing, corrupt or of another schema. It blocks: run it on a goroutine.
func ReadClaude(dir string) *PlanUsage {
	snap := readSnapshot(filepath.Join(dir, ClaudeFile))
	if snap == nil {
		return nil
	}
	return &PlanUsage{Plan: PlanClaude, Windows: snap.Windows, ReadAt: snap.UpdatedAt}
}

// readSnapshot returns the snapshot at path, or nil. A missing file is quiet;
// a bad one is logged.
func readSnapshot(path string) *ClaudeSnapshot {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("unable to read the Claude usage file", "err", err)
		}
		return nil
	}
	var snap ClaudeSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		slog.Warn("Claude usage file is corrupt", "err", err)
		return nil
	}
	if snap.Schema != schema {
		slog.Warn("Claude usage file has another schema", "schema", snap.Schema)
		return nil
	}
	return &snap
}

// RunStatusline is `contr statusline`. It reads Claude Code's status line JSON
// from stdin, keeps the windows in the state dir, then copies to stdout what
// then prints for the same input (nothing when then is empty). Input over
// 1 MiB is skipped: nothing is saved and then does not run, so no part of it is
// used. Bad input and failed saves are logged, not returned, so the status line
// keeps working. It returns an error only when stdin or stdout fails.
func RunStatusline(stdin io.Reader, stdout io.Writer, then []string) error {
	return runWith(stdin, stdout, then, StateDir(), uint64(time.Now().Unix()), chainTimeout)
}

// runWith is RunStatusline with the state dir (empty for none), the time and
// the chain limit passed in.
func runWith(stdin io.Reader, stdout io.Writer, then []string, dir string, now uint64, timeout time.Duration) error {
	// One byte past the cap tells an over-long input from one of exactly the cap.
	input, err := io.ReadAll(io.LimitReader(stdin, maxInput+1))
	if err != nil {
		return err
	}
	if len(input) > maxInput {
		slog.Warn("status line input is over 1 MiB; skipped")
		return nil
	}
	if snap := ParseStatusline(string(input), now); snap != nil {
		if dir == "" {
			slog.Warn("no state dir; Claude usage not saved")
		} else if _, err := WriteSnapshot(dir, snap); err != nil {
			slog.Warn("unable to write the Claude usage file", "err", err)
		}
	}
	if len(then) > 0 {
		if _, err := stdout.Write(runCapped(then[0], then[1:], input, timeout)); err != nil {
			return err
		}
	}
	return nil
}

// runCapped runs program args… with input on its stdin and returns its stdout,
// whatever its exit status. A command that runs past timeout is killed with the
// processes it forked, and returns what it wrote so far. A command that cannot
// start, or whose pipes cannot be served, returns nothing.
func runCapped(program string, args []string, input []byte, timeout time.Duration) []byte {
	cmd := exec.Command(program, args...)
	isolate(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		slog.Warn("unable to serve the status line command's pipes", "program", program, "err", err)
		return nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		slog.Warn("unable to serve the status line command's pipes", "program", program, "err", err)
		return nil
	}
	cmd.Stdout = w
	if err := cmd.Start(); err != nil {
		slog.Warn("status line command did not start", "program", program, "err", err)
		_ = w.Close()
		_ = r.Close()
		return nil
	}
	_ = w.Close() // The child holds its own copy.
	out := collectPipe(r)
	go func() {
		_, _ = stdin.Write(input)
		_ = stdin.Close()
	}()
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-exited:
	case <-timer.C:
		slog.Warn("status line command ran past its limit; killed", "program", program)
		killGroup(cmd)
		_ = cmd.Process.Kill()
		<-exited
	}
	return out.finish()
}

// pipeOutput gathers what a pipe delivers, on its own goroutine, so the caller
// never blocks on it.
type pipeOutput struct {
	r      *os.File
	mu     sync.Mutex
	buf    bytes.Buffer
	closed chan struct{}
}

func collectPipe(r *os.File) *pipeOutput {
	p := &pipeOutput{r: r, closed: make(chan struct{})}
	go func() {
		defer close(p.closed)
		chunk := make([]byte, 8192)
		for {
			n, err := r.Read(chunk)
			if n > 0 {
				p.mu.Lock()
				p.buf.Write(chunk[:n])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return p
}

// finish returns the output so far. It waits for the pipe to close, but no
// longer than drain: a process that escaped the kill may hold the pipe open.
func (p *pipeOutput) finish() []byte {
	timer := time.NewTimer(drain)
	defer timer.Stop()
	select {
	case <-p.closed:
	case <-timer.C:
	}
	_ = p.r.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.buf.Bytes()...)
}
