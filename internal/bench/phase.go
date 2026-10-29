package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Phase is the result of one phase for a later comparison. Frames and CPU
// seconds move much less with other load on the machine than CPU percent.
// The JSON names are the format of result.json (docs/BENCHMARKS.md).
type Phase struct {
	Frames       int     `json:"frames"`
	FramesPerSec float64 `json:"frames_per_s"`
	CPUSeconds   float64 `json:"cpu_seconds"`
	CPUMed       float64 `json:"cpu_med"`
	FootprintMax float64 `json:"footprint_max"` // MB
	Working      bool    `json:"working"`       // a working item was on screen
}

// NewPhase joins the frame count, the phase length and the CPU seconds with
// the sampled Stats.
func NewPhase(frames int, seconds, cpuSeconds float64, st Stats, working bool) Phase {
	p := Phase{
		Frames:       frames,
		CPUSeconds:   round3(cpuSeconds),
		CPUMed:       st.CPUMed,
		FootprintMax: round3(st.FootprintMaxMB),
		Working:      working,
	}
	if seconds > 0 {
		p.FramesPerSec = round3(float64(frames) / seconds)
	}
	return p
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// MarshalResult gives the JSON of result.json: phase name to Phase.
func MarshalResult(r map[string]Phase) ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("bench: result json: %w", err)
	}
	return append(b, '\n'), nil
}

// ParseCPUTime parses `ps -o time=`: [[dd-]hh:]mm:ss.cc, and gives seconds
// (user plus system).
func ParseCPUTime(out string) (float64, error) {
	s := strings.TrimSpace(out)
	if s == "" {
		return 0, fmt.Errorf("bench: cpu time: empty")
	}
	days := 0.0
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("bench: cpu time %q: bad days", s)
		}
		days, s = float64(n), rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("bench: cpu time %q: want mm:ss.cc", out)
	}
	total := 0.0
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("bench: cpu time %q: bad field %q", strings.TrimSpace(out), p)
		}
		if i < len(parts)-1 && v != float64(int(v)) {
			return 0, fmt.Errorf("bench: cpu time %q: bad field %q", strings.TrimSpace(out), p)
		}
		total = total*60 + v
	}
	return total + days*86400, nil
}

// ReadCPUTime gives the total CPU seconds (user plus system) the process has
// used so far, from ps. The resolution is 10 ms.
func ReadCPUTime(pid int) (float64, error) {
	out, err := run("ps", "-o", "time=", "-p", strconv.Itoa(pid))
	if err != nil {
		return 0, fmt.Errorf("bench: ps time: %w", err)
	}
	return ParseCPUTime(string(out))
}

// StartMark and EndMark are the lines the test writes to the app log where
// the samples of a phase start and end. CountFrames reads them.
func StartMark(phase string) string { return fmt.Sprintf("bench: phase %q: samples start", phase) }

// EndMark: see StartMark.
func EndMark(phase string) string { return fmt.Sprintf("bench: phase %q: samples end", phase) }

// frameLine marks a line of MyGo's frame log (MYGO_FRAME_STATS=all).
const frameLine = "mygo: frame "

// CountFrames counts the frame lines between the start and end marks of each
// phase in the app log. A phase with a start mark and no end mark counts to
// the end of the log. A log without frame lines gives zero for each phase.
func CountFrames(log string) map[string]int {
	counts := map[string]int{}
	cur := ""
	for _, line := range strings.Split(log, "\n") {
		if i := strings.Index(line, "bench: phase "); i >= 0 {
			rest := line[i+len("bench: phase "):]
			q, tail, ok := cutQuoted(rest)
			if ok {
				switch tail {
				case ": samples start":
					cur = q
					counts[q] = 0
				case ": samples end":
					cur = ""
				}
				continue
			}
		}
		if cur != "" && strings.Contains(line, frameLine) {
			counts[cur]++
		}
	}
	return counts
}

// cutQuoted splits a leading Go-quoted string from s.
func cutQuoted(s string) (unquoted, tail string, ok bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", "", false
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			u, err := strconv.Unquote(s[:i+1])
			if err != nil {
				return "", "", false
			}
			return u, strings.TrimRight(s[i+1:], " \r"), true
		}
	}
	return "", "", false
}
