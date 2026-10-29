// Package bench reads CPU and memory of a process and sums up samples
// for the whole-app benchmark (M13).
package bench

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Sample is one reading of a process.
type Sample struct {
	CPU         float64 // percent, from ps
	RSSKB       int64
	FootprintKB int64
}

// Stats sums up the samples of one phase.
type Stats struct {
	CPUMin, CPUMed, CPUMax float64
	FootprintMaxMB         float64
	RSSMaxMB               float64
}

// run starts a command and returns its output. Tests replace it, so tests
// that do this must not run in parallel. LC_ALL=C keeps the number and unit
// format of ps and footprint the same in every locale.
var run = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd.Output()
}

// ParsePS parses the output of `ps -o %cpu= -o rss=`: two numbers.
func ParsePS(out string) (cpu float64, rssKB int64, err error) {
	f := strings.Fields(out)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("bench: ps output %q: want 2 fields", strings.TrimSpace(out))
	}
	cpu, err = strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bench: ps cpu %q: %w", f[0], err)
	}
	rssKB, err = strconv.ParseInt(f[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bench: ps rss %q: %w", f[1], err)
	}
	return cpu, rssKB, nil
}

// ParseFootprint returns the first "Footprint: N KB|MB|GB" value in KB.
func ParseFootprint(out string) (kb int64, err error) {
	for _, line := range strings.Split(out, "\n") {
		_, rest, ok := strings.Cut(line, "Footprint:")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 2 {
			return 0, fmt.Errorf("bench: footprint line %q: no unit", strings.TrimSpace(line))
		}
		v, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			return 0, fmt.Errorf("bench: footprint value %q: %w", f[0], err)
		}
		var mul float64
		switch f[1] {
		case "KB":
			mul = 1
		case "MB":
			mul = 1 << 10
		case "GB":
			mul = 1 << 20
		default:
			return 0, fmt.Errorf("bench: footprint unit %q", f[1])
		}
		return int64(v*mul + 0.5), nil
	}
	return 0, fmt.Errorf("bench: no Footprint line in output")
}

// Read samples the process with ps and footprint.
func Read(pid int) (Sample, error) {
	p := strconv.Itoa(pid)
	psOut, err := run("ps", "-o", "%cpu=", "-o", "rss=", "-p", p)
	if err != nil {
		return Sample{}, fmt.Errorf("bench: ps: %w", err)
	}
	cpu, rss, err := ParsePS(string(psOut))
	if err != nil {
		return Sample{}, err
	}
	fpOut, err := run("footprint", p)
	if err != nil {
		return Sample{}, fmt.Errorf("bench: footprint: %w", err)
	}
	fp, err := ParseFootprint(string(fpOut))
	if err != nil {
		return Sample{}, err
	}
	return Sample{CPU: cpu, RSSKB: rss, FootprintKB: fp}, nil
}

// Summarize gives min, median and max CPU and the largest memory.
// An empty slice gives zero Stats.
func Summarize(s []Sample) Stats {
	if len(s) == 0 {
		return Stats{}
	}
	cpus := make([]float64, len(s))
	var fpMax, rssMax int64
	for i, x := range s {
		cpus[i] = x.CPU
		fpMax = max(fpMax, x.FootprintKB)
		rssMax = max(rssMax, x.RSSKB)
	}
	sort.Float64s(cpus)
	n := len(cpus)
	med := cpus[n/2]
	if n%2 == 0 {
		med = (cpus[n/2-1] + cpus[n/2]) / 2
	}
	return Stats{
		CPUMin:         cpus[0],
		CPUMed:         med,
		CPUMax:         cpus[n-1],
		FootprintMaxMB: float64(fpMax) / 1024,
		RSSMaxMB:       float64(rssMax) / 1024,
	}
}

// Header is the Markdown table head that matches Row.
func Header() string {
	return "| Phase | CPU min / med / max | Footprint max | RSS max | Load avg |\n" +
		"|---|---|---|---|---|"
}

// Row is one Markdown table row.
func Row(phase string, s Stats, load string) string {
	return fmt.Sprintf("| %s | %.1f / %.1f / %.1f %% | %.0f MB | %.0f MB | %s |",
		phase, s.CPUMin, s.CPUMed, s.CPUMax, s.FootprintMaxMB, s.RSSMaxMB, load)
}
