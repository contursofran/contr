package bench

import (
	"errors"
	"strings"
	"testing"
)

const fpHeader = "zsh [92038]: 64-bit    Footprint: %s (16384 bytes per page)\n\nDirty  Clean\n"

func TestParsePS(t *testing.T) {
	cpu, rss, err := ParsePS("  0.3  12345 \n")
	if err != nil || cpu != 0.3 || rss != 12345 {
		t.Fatalf("got %v %v %v", cpu, rss, err)
	}
}

func TestParsePSBad(t *testing.T) {
	for _, in := range []string{"", "0.3", "a b", "0.3 x", "1 2 3"} {
		if _, _, err := ParsePS(in); err == nil {
			t.Errorf("%q: want error", in)
		}
	}
}

func TestParseFootprint(t *testing.T) {
	cases := map[string]int64{
		"2640 KB": 2640,
		"112 MB":  112 * 1024,
		"1 GB":    1 << 20,
		"1.5 MB":  1536,
	}
	for v, want := range cases {
		got, err := ParseFootprint(strings.Replace(fpHeader, "%s", v, 1))
		if err != nil || got != want {
			t.Errorf("%s: got %d, %v; want %d", v, got, err, want)
		}
	}
}

func TestParseFootprintBad(t *testing.T) {
	for _, in := range []string{"", "no such line\n", "x Footprint: 5\n", "x Footprint: 5 TB\n", "x Footprint: a MB\n"} {
		if _, err := ParseFootprint(in); err == nil {
			t.Errorf("%q: want error", in)
		}
	}
}

func fake(ps, fp string, err error) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		if name == "ps" {
			return []byte(ps), nil
		}
		return []byte(fp), nil
	}
}

func TestRead(t *testing.T) {
	old := run
	defer func() { run = old }()
	var calls []string
	run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return fake(" 1.5 2048\n", strings.Replace(fpHeader, "%s", "64 MB", 1), nil)(name, args...)
	}
	s, err := Read(42)
	if err != nil {
		t.Fatal(err)
	}
	if s != (Sample{CPU: 1.5, RSSKB: 2048, FootprintKB: 65536}) {
		t.Fatalf("got %+v", s)
	}
	if len(calls) != 2 || calls[0] != "ps -o %cpu= -o rss= -p 42" || calls[1] != "footprint 42" {
		t.Fatalf("calls %q", calls)
	}
}

func TestReadErrors(t *testing.T) {
	old := run
	defer func() { run = old }()
	run = fake("", "", errors.New("gone"))
	if _, err := Read(1); err == nil {
		t.Error("runner error: want error")
	}
	run = fake("bad", "", nil)
	if _, err := Read(1); err == nil {
		t.Error("bad ps: want error")
	}
	run = fake("1 2", "nothing", nil)
	if _, err := Read(1); err == nil {
		t.Error("bad footprint: want error")
	}
}

func TestSummarize(t *testing.T) {
	odd := Summarize([]Sample{
		{CPU: 3, RSSKB: 1024, FootprintKB: 2048},
		{CPU: 1, RSSKB: 4096, FootprintKB: 1024},
		{CPU: 2, RSSKB: 2048, FootprintKB: 3072},
	})
	want := Stats{CPUMin: 1, CPUMed: 2, CPUMax: 3, FootprintMaxMB: 3, RSSMaxMB: 4}
	if odd != want {
		t.Errorf("odd: got %+v", odd)
	}
	even := Summarize([]Sample{{CPU: 4}, {CPU: 1}, {CPU: 2}, {CPU: 3}})
	if even.CPUMed != 2.5 || even.CPUMin != 1 || even.CPUMax != 4 {
		t.Errorf("even: got %+v", even)
	}
	if got := Summarize(nil); got != (Stats{}) {
		t.Errorf("empty: got %+v", got)
	}
}

func TestRow(t *testing.T) {
	got := Row("idle shown", Phase{Frames: 30, FramesPerSec: 2, CPUSeconds: 0.123},
		Stats{CPUMin: 0, CPUMed: 0.1, CPUMax: 0.4, FootprintMaxMB: 112, RSSMaxMB: 98}, "2.1")
	want := "| idle shown | no | 30 (2.0) | 0.12 | 0.0 / 0.1 / 0.4 % | 112 MB | 98 MB | 2.1 |"
	if got != want {
		t.Errorf("got %q", got)
	}
	head := strings.SplitN(Header(), "\n", 2)[0]
	if strings.Count(head, "|") != strings.Count(got, "|") {
		t.Error("header columns differ from row")
	}
}
