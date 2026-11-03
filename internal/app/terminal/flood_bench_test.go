package terminal

// The perf gate of the hc terminal hot path (#216). No window and no hc: a
// generated flood goes through the framed stream decoder, into the emulator,
// and a screen is built for each batch of frames, as the link does for each
// UI frame (D53). BenchmarkFloodHotPath reports the speed. TestFloodGate
// fails when allocations for each MB of output pass floodAllocsLimit.

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/term/vt"
)

const (
	floodCols, floodRows = 100, 52
	// floodBatch is the frames for each screen. The core sends about 50,000
	// frames a second and the view takes about 60 screens a second.
	floodBatch = 256
	// floodAllocsLimit is the most allocations for each MB of output. The
	// baseline (PR #220 code, see docs/BENCHMARKS.md) is floodAllocsBaseline;
	// the limit is about 2x that, so noise passes and a lost fix does not.
	floodAllocsBaseline = 21600
	floodAllocsLimit    = 2 * floodAllocsBaseline
)

// floodStream makes size bytes of colored ASCII lines as framed DATA frames
// on one channel. The payloads are 40 to 176 bytes (median about 108), cut
// anywhere, even inside a line or an escape sequence, as the core cuts them.
func floodStream(size int) (stream []byte, output int) {
	var text bytes.Buffer
	for i := 0; text.Len() < size; i++ {
		if i%4 == 0 {
			fmt.Fprintf(&text, "\x1b[3%dm", i%8)
		}
		fmt.Fprintf(&text, "bench line %d abcdefghij 0123456789 -_=+()[]{}<>\r\n", i)
		if i%4 == 3 {
			text.WriteString("\x1b[0m")
		}
	}
	data := text.Bytes()
	for i := 0; len(data) > 0; i++ {
		n := min(40+(i*37)%137, len(data))
		stream = hc.AppendFrame(stream, 1, hc.KindData, data[:n])
		data = data[n:]
	}
	return stream, text.Len()
}

// floodRun decodes stream and feeds the emulator, one screen for each batch.
func floodRun(tb testing.TB, emu *vt.Terminal, stream []byte) {
	r := bytes.NewReader(stream)
	for n := 0; ; n++ {
		f, err := hc.ReadFrame(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			tb.Fatal(err)
		}
		ev := hc.StreamEvent{Data: f.Payload}
		if _, err := emu.Write(ev.Data); err != nil {
			tb.Fatal(err)
		}
		if n%floodBatch == floodBatch-1 {
			_ = emu.Screen(0)
		}
	}
	_ = emu.Screen(0)
}

func BenchmarkFloodHotPath(b *testing.B) {
	stream, out := floodStream(2 << 20)
	emu := vt.New(floodCols, floodRows, nil)
	defer emu.Close()
	floodRun(b, emu, stream) // fills the scrollback
	b.SetBytes(int64(out))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		floodRun(b, emu, stream)
	}
	b.ReportMetric(float64(testing.AllocsPerRun(1, func() { floodRun(b, emu, stream) }))*float64(1<<20)/float64(out), "allocs/MB")
}

// floodAllocsPerMB is the allocations for each MB of output, with the
// scrollback full.
func floodAllocsPerMB(t testing.TB) float64 {
	stream, out := floodStream(1 << 20)
	emu := vt.New(floodCols, floodRows, nil)
	defer emu.Close()
	floodRun(t, emu, stream)
	runs := testing.AllocsPerRun(2, func() { floodRun(t, emu, stream) })
	return runs * float64(1<<20) / float64(out)
}

// floodGate is nil when perMB is within limit.
func floodGate(perMB, limit float64) error {
	if perMB > limit {
		return fmt.Errorf("flood hot path: %.0f allocations/MB, limit %.0f (baseline %d): see docs/BENCHMARKS.md", perMB, limit, floodAllocsBaseline)
	}
	return nil
}

// TestFloodGate is the gate. check.sh runs it alone with CONTR_PERF_GATE=1,
// without -race (the race detector changes allocations and speed).
func TestFloodGate(t *testing.T) {
	if os.Getenv("CONTR_PERF_GATE") != "1" {
		t.Skip("set CONTR_PERF_GATE=1 (scripts/check.sh does)")
	}
	limit := float64(floodAllocsLimit)
	if s := os.Getenv("CONTR_FLOOD_LIMIT"); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatal(err)
		}
		limit = v
	}
	perMB := floodAllocsPerMB(t)
	t.Logf("flood hot path: %.0f allocations/MB (limit %.0f)", perMB, limit)
	if err := floodGate(perMB, limit); err != nil {
		t.Fatal(err)
	}
}

// TestFloodGateFails checks that the gate fails when the limit is crossed,
// and passes at the limit.
func TestFloodGateFails(t *testing.T) {
	if err := floodGate(float64(floodAllocsLimit)+1, floodAllocsLimit); err == nil {
		t.Error("gate passed above the limit")
	}
	if err := floodGate(float64(floodAllocsLimit), floodAllocsLimit); err != nil {
		t.Errorf("gate failed at the limit: %v", err)
	}
	if testing.Short() || raceOn {
		return
	}
	// With the real measurement and a limit of 1, the gate must fail.
	if err := floodGate(floodAllocsPerMB(t), 1); err == nil {
		t.Error("gate passed with a limit of 1")
	}
}
