package bench

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseCPUTime(t *testing.T) {
	cases := map[string]float64{
		"  0:00.02\n":   0.02,
		"0:12.34":       12.34,
		"3:05.50":       185.5,
		"1:02:03.25":    3723.25,
		"2-01:00:00.00": 2*86400 + 3600,
	}
	for in, want := range cases {
		got, err := ParseCPUTime(in)
		if err != nil || got < want-1e-9 || got > want+1e-9 {
			t.Errorf("%q: got %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestParseCPUTimeBad(t *testing.T) {
	for _, in := range []string{"", "  ", "12", "a:b", "1:2:3:4", "x-1:00", "-1-0:00", "1.5:00.00", "0:-1"} {
		if _, err := ParseCPUTime(in); err == nil {
			t.Errorf("%q: want error", in)
		}
	}
}

func TestReadCPUTime(t *testing.T) {
	old := run
	defer func() { run = old }()
	var call string
	run = func(name string, args ...string) ([]byte, error) {
		call = name + " " + strings.Join(args, " ")
		return []byte(" 1:01.50\n"), nil
	}
	got, err := ReadCPUTime(7)
	if err != nil || got != 61.5 || call != "ps -o time= -p 7" {
		t.Fatalf("got %v, %v, %q", got, err, call)
	}
	run = func(string, ...string) ([]byte, error) { return nil, errors.New("gone") }
	if _, err := ReadCPUTime(7); err == nil {
		t.Error("runner error: want error")
	}
	run = func(string, ...string) ([]byte, error) { return []byte("bad"), nil }
	if _, err := ReadCPUTime(7); err == nil {
		t.Error("bad output: want error")
	}
}

func TestCountFrames(t *testing.T) {
	log := strings.Join([]string{
		"mygo: frame 1 took 1 ms: build 0.1", // before any phase: not counted
		StartMark("idle shown"),
		"mygo: frame 2 took 1 ms",
		"2026/10/29 mygo: frame 3 took 1 ms",
		"some other line",
		EndMark("idle shown"),
		"mygo: frame 4 took 1 ms", // between phases: not counted
		StartMark(`odd "name"`),
		"mygo: frame 5 took 1 ms",
		EndMark(`odd "name"`),
		StartMark("open"),
		"mygo: frame 6 took 1 ms",
	}, "\n")
	got := CountFrames(log)
	want := map[string]int{"idle shown": 2, `odd "name"`: 1, "open": 1}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%q: got %d, want %d", k, got[k], v)
		}
	}
	// No frame lines (MYGO_FRAME_STATS unset): zero for the phase.
	got = CountFrames(StartMark("p") + "\n" + EndMark("p") + "\n")
	if v, ok := got["p"]; !ok || v != 0 {
		t.Errorf("no frames: got %v", got)
	}
}

func TestNewPhase(t *testing.T) {
	p := NewPhase(450, 15, 1.25, Stats{CPUMed: 3, FootprintMaxMB: 100}, true)
	if p.Frames != 450 || p.FramesPerSec != 30 || p.CPUSeconds != 1.25 || p.CPUMed != 3 || p.FootprintMax != 100 || !p.Working {
		t.Errorf("got %+v", p)
	}
	if NewPhase(5, 0, 0, Stats{}, false).FramesPerSec != 0 {
		t.Error("zero seconds: want 0 frames a second")
	}
}

func TestMarshalResult(t *testing.T) {
	b, err := MarshalResult(map[string]Phase{"idle shown": {Frames: 2, FramesPerSec: 0.5, CPUSeconds: 0.1, CPUMed: 0, FootprintMax: 80, Working: true}})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	m := back["idle shown"]
	for _, k := range []string{"frames", "frames_per_s", "cpu_seconds", "cpu_med", "footprint_max", "working"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %s", k, b)
		}
	}
	if m["working"] != true || m["frames"] != float64(2) {
		t.Errorf("values: %v", m)
	}
}
