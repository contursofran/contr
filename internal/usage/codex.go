// Codex's session logs as a usage source. Codex writes the plan's limit
// windows into its rollout files under sessions/, on each token_count event.
// ReadCodex reads the tail of the newest rollout file and nothing else under
// CODEX_HOME: no credentials, config or transcript text is read.

package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	// dayFolders is how many of the newest day folders are searched for
	// rollout files.
	dayFolders = 2
	// tailBytes is the largest tail read from a rollout file. The newest
	// token_count line is searched for in it and nowhere earlier.
	tailBytes = 256 * 1024
)

// CodexHome is $CODEX_HOME, else ~/.codex. It returns "" when no folder can be
// found. See CodexHomeFrom.
func CodexHome() string {
	home, _ := os.UserHomeDir()
	return CodexHomeFrom(os.Getenv, home)
}

// CodexHomeFrom returns $CODEX_HOME when set and not empty, else ~/.codex. The
// environment and home are passed in so tests need no real variables. An empty
// home means no home; the result is "" when no folder can be found.
func CodexHomeFrom(env func(string) string, home string) string {
	if dir := env("CODEX_HOME"); dir != "" {
		return dir
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// ReadCodex returns Codex's usage from the newest rollout file under
// home/sessions, or nil when there is no rollout file, its tail holds no usable
// token_count line, or the folder cannot be read. It blocks: run it on a
// goroutine.
func ReadCodex(home string) *PlanUsage {
	rollout, ok := newestRollout(filepath.Join(home, "sessions"))
	if !ok {
		return nil
	}
	tail, err := readTail(rollout.path)
	if err != nil {
		slog.Warn("unable to read the Codex rollout file", "err", err)
		return nil
	}
	windows := ParseRolloutTail(tail, rollout.modified)
	if windows == nil {
		return nil
	}
	return &PlanUsage{Plan: PlanCodex, Windows: windows, ReadAt: rollout.modified}
}

// ParseRolloutTail returns the limit windows of the last usable token_count
// line in a rollout tail, or nil. A line is usable when rate_limits is present,
// its limit_id is absent or "codex", and its primary window has a percent.
// primary becomes kind "primary"; secondary becomes "secondary" when it has a
// percent too. A window resets at resets_at, or at fileMtime + resets_in_seconds
// when the line gives only that. Lines that do not parse are skipped.
func ParseRolloutTail(tail string, fileMtime uint64) []Window {
	lines := strings.Split(tail, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.Contains(lines[i], "token_count") {
			continue
		}
		if windows := windowsOfLine(lines[i], fileMtime); windows != nil {
			return windows
		}
	}
	return nil
}

// rolloutEntry holds the parts of a rollout line that contr reads.
type rolloutEntry struct {
	Payload *rolloutPayload `json:"payload"`
}

type rolloutPayload struct {
	Type       *string         `json:"type"`
	RateLimits *codexRateLimit `json:"rate_limits"`
}

type codexRateLimit struct {
	LimitID   *string         `json:"limit_id"`
	Primary   *codexRawWindow `json:"primary"`
	Secondary *codexRawWindow `json:"secondary"`
}

// codexRawWindow reads the reset fields as numbers, so a float such as
// 1777927196.0 parses. See wholeSeconds.
type codexRawWindow struct {
	UsedPercent     *float64 `json:"used_percent"`
	ResetsAt        *float64 `json:"resets_at"`
	ResetsInSeconds *float64 `json:"resets_in_seconds"`
}

// windowsOfLine returns the windows of one rollout line, or nil when the line
// is not usable.
func windowsOfLine(line string, fileMtime uint64) []Window {
	var entry rolloutEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return nil
	}
	p := entry.Payload
	if p == nil || p.Type == nil || *p.Type != "token_count" || p.RateLimits == nil {
		return nil
	}
	limits := p.RateLimits
	if limits.LimitID != nil && *limits.LimitID != "codex" {
		return nil
	}
	if limits.Primary == nil {
		return nil
	}
	primary, ok := usageWindow("primary", *limits.Primary, fileMtime)
	if !ok {
		return nil
	}
	windows := []Window{primary}
	if limits.Secondary != nil {
		if secondary, ok := usageWindow("secondary", *limits.Secondary, fileMtime); ok {
			windows = append(windows, secondary)
		}
	}
	return windows
}

// usageWindow returns one window; ok is false when it has no percent.
func usageWindow(kind string, raw codexRawWindow, fileMtime uint64) (Window, bool) {
	if raw.UsedPercent == nil {
		return Window{}, false
	}
	resetsAt := wholeSeconds(raw.ResetsAt)
	if resetsAt == nil {
		if secs := wholeSeconds(raw.ResetsInSeconds); secs != nil {
			sum := fileMtime + *secs
			if sum < fileMtime { // overflow: saturate
				sum = math.MaxUint64
			}
			resetsAt = &sum
		}
	}
	return Window{Kind: kind, UsedPercent: *raw.UsedPercent, ResetsAt: resetsAt}, true
}

// wholeSeconds returns whole seconds from a JSON number: the fraction is
// dropped. A negative or non-finite number is no time, so the window keeps no
// reset time. A number past the range saturates.
func wholeSeconds(v *float64) *uint64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
		return nil
	}
	var s uint64
	if *v >= math.MaxUint64 {
		s = math.MaxUint64
	} else {
		s = uint64(*v)
	}
	return &s
}

// rollout is a rollout file and the Unix seconds it was last written.
type rollout struct {
	path     string
	modified uint64
}

// newestRollout returns the rollout file written last among the rollout files
// of the newest dayFolders day folders. Ties go to the name that sorts last.
func newestRollout(sessions string) (rollout, bool) {
	var best rollout
	found := false
	for _, day := range newestDayFolders(sessions, dayFolders) {
		for _, r := range rolloutsIn(day) {
			if !found || r.modified > best.modified || (r.modified == best.modified && r.path > best.path) {
				best, found = r, true
			}
		}
	}
	return best, found
}

// newestDayFolders returns the newest count day folders, as
// sessions/YEAR/MONTH/DAY. Folder names are numbers and are compared as
// numbers.
func newestDayFolders(sessions string, count int) []string {
	var days []string
	for _, year := range numberedSubfolders(sessions) {
		for _, month := range numberedSubfolders(year) {
			for _, day := range numberedSubfolders(month) {
				days = append(days, day)
				if len(days) == count {
					return days
				}
			}
		}
	}
	return days
}

// numberedSubfolders returns the subfolders of dir whose names are numbers,
// largest first. Files and other names are ignored.
func numberedSubfolders(dir string) []string {
	type folder struct {
		number uint32
		path   string
	}
	var folders []folder
	for _, e := range entriesOf(dir) {
		if !e.IsDir() {
			continue
		}
		n, err := strconv.ParseUint(e.Name(), 10, 32)
		if err != nil {
			continue
		}
		folders = append(folders, folder{uint32(n), filepath.Join(dir, e.Name())})
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].number > folders[j].number })
	paths := make([]string, len(folders))
	for i, f := range folders {
		paths[i] = f.path
	}
	return paths
}

// rolloutsIn returns the rollout-*.jsonl files directly in dir, with their
// modified times. A file whose time cannot be read is skipped.
func rolloutsIn(dir string) []rollout {
	var out []rollout
	for _, e := range entriesOf(dir) {
		name := e.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		secs := info.ModTime().Unix()
		if secs < 0 {
			continue
		}
		out = append(out, rollout{path: filepath.Join(dir, name), modified: uint64(secs)})
	}
	return out
}

// entriesOf returns the entries of dir. A missing folder is quiet; any other
// error is logged and reads as an empty folder.
func entriesOf(dir string) []fs.DirEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("unable to list a Codex sessions folder", "err", err)
		}
	}
	return entries // ReadDir returns the entries it read before an error.
}

// readTail returns the last tailBytes of the file as text. When the tail starts
// inside a line, that partial first line is dropped. When it starts at a line
// start, the first line is kept. The read begins one byte early to tell the two
// apart.
func readTail(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size <= tailBytes {
		b, err := io.ReadAll(io.LimitReader(f, tailBytes))
		return string(b), err
	}
	start := size - tailBytes
	if _, err := f.Seek(start-1, io.SeekStart); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(f, tailBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", nil
	}
	before, rest := b[0], b[1:]
	if before == '\n' {
		return string(rest), nil
	}
	at := bytes.IndexByte(rest, '\n')
	if at < 0 {
		return "", nil
	}
	return string(rest[at+1:]), nil
}
