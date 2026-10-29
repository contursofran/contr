package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/assets"
	"github.com/contursofran/contr/internal/app/theme"
)

// closed is a writer that fails on every write, as a closed pipe does.
type closed struct{}

func (closed) Write([]byte) (int, error) { return 0, errors.New("closed") }

func parse(args ...string) Command { return parseArgs(args) }

func window(icons bool) Command { return Command{Kind: kindWindow, Icons: icons} }

func same(t *testing.T, got, want Command) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestNoArgumentOpensTheWindow(t *testing.T) {
	same(t, parse("contr"), window(false))
	// The program name is never an argument, even if it looks like one.
	same(t, parse("--icons"), window(false))
	same(t, parse("statusline"), window(false))
	same(t, parseArgs(nil), window(false))
}

func TestIconsFlagOpensTheLegend(t *testing.T) {
	same(t, parse("contr", "--icons"), window(true))
}

func TestHelpFlagsAskForTheUsage(t *testing.T) {
	help := Command{Kind: kindHelp}
	same(t, parse("contr", "--help"), help)
	same(t, parse("contr", "-h"), help)
	same(t, parse("contr", "--icons", "--help"), help)
}

func TestTheFirstProblemDecidesBetweenHelpAndAnError(t *testing.T) {
	same(t, parse("contr", "--help", "--bogus"), Command{Kind: kindHelp})
	same(t, parse("contr", "--bogus", "--help"), Command{Kind: kindUnknown, Unknown: "--bogus"})
}

func TestOtherArgumentsAreUnknown(t *testing.T) {
	unknown := func(a string) Command { return Command{Kind: kindUnknown, Unknown: a} }
	same(t, parse("contr", "--icon"), unknown("--icon"))
	same(t, parse("contr", "--icons", "--x"), unknown("--x"))
	same(t, parse("contr", "extra"), unknown("extra"))
	// `statusline` is a subcommand only as the first argument.
	same(t, parse("contr", "--icons", "statusline"), unknown("statusline"))
	// `dev` is not ported (decision D5), so it is unknown.
	same(t, parse("contr", "dev", "state"), unknown("dev"))
}

func TestFinderProcessSerialNumberIsIgnored(t *testing.T) {
	// macOS passes `-psn_<n>_<n>` when an app starts from Finder or the Dock.
	same(t, parse("contr", "-psn_0_12345"), window(false))
	same(t, parse("contr", "-psn_0_12345", "--icons"), window(true))
	same(t, parse("contr", "-psn_0_12345", "--help"), Command{Kind: kindHelp})
	// Only the `-psn_` prefix is ignored. Other dash arguments stay unknown.
	same(t, parse("contr", "-psn"), Command{Kind: kindUnknown, Unknown: "-psn"})
}

func TestStatuslineArgumentSelectsTheStatusLine(t *testing.T) {
	same(t, parse("contr", "statusline"), Command{Kind: kindStatusline})
	same(t, parse("contr", "statusline", "--", "cat", "-n"),
		Command{Kind: kindStatusline, Then: []string{"cat", "-n"}})
}

func TestStatuslineWithoutASeparatorChainsNothing(t *testing.T) {
	same(t, parse("contr", "statusline", "cat"), Command{Kind: kindStatusline})
	// A separator that is not right after `statusline` does not start a chain.
	same(t, parse("contr", "statusline", "cat", "--", "x"), Command{Kind: kindStatusline})
}

func TestStatuslineNeverTurnsItsWordsIntoHelpOrAnError(t *testing.T) {
	// The status line must not fail on its input, so these words are not checked.
	same(t, parse("contr", "statusline", "--help"), Command{Kind: kindStatusline})
	same(t, parse("contr", "statusline", "--bogus"), Command{Kind: kindStatusline})
}

func TestUsageListsEveryForm(t *testing.T) {
	var text bytes.Buffer
	if err := writeUsage(&text, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text.String(), "Usage:\n") {
		t.Errorf("usage starts with %q", text.String())
	}
	for _, form := range []string{
		"contr --icons",
		"contr statusline [-- <command> [args…]]",
		"contr --help, -h",
	} {
		if !strings.Contains(text.String(), form) {
			t.Errorf("usage lacks %q", form)
		}
	}
}

func TestHelpPrintsTheUsageAndExitsZero(t *testing.T) {
	var out bytes.Buffer
	if code := printUsage(&out); code != 0 {
		t.Errorf("exit code %d", code)
	}
	if out.String() != usageText {
		t.Errorf("out = %q", out.String())
	}
}

func TestHelpExitsOneWhenStdoutIsClosed(t *testing.T) {
	if code := printUsage(closed{}); code != 1 {
		t.Errorf("exit code %d", code)
	}
}

func TestUnknownArgumentPrintsTheErrorAndTheUsageAndExitsTwo(t *testing.T) {
	var err bytes.Buffer
	if code := rejectArgument("--bogus", &err); code != 2 {
		t.Errorf("exit code %d", code)
	}
	if want := "contr: unknown argument --bogus\n" + usageText; err.String() != want {
		t.Errorf("stderr = %q", err.String())
	}
}

func TestUnknownArgumentStillExitsTwoWhenStderrIsClosed(t *testing.T) {
	if code := rejectArgument("--bogus", closed{}); code != 2 {
		t.Errorf("exit code %d", code)
	}
}

func TestStatuslineReturnsZeroAndReportsAFailureOnStderr(t *testing.T) {
	old := runStatusline
	defer func() { runStatusline = old }()
	var gotThen []string
	runStatusline = func(_ io.Reader, _ io.Writer, then []string) error {
		gotThen = then
		return errors.New("stdout closed")
	}
	var stderr bytes.Buffer
	if code := statusline(strings.NewReader(""), io.Discard, &stderr, []string{"cat"}); code != 0 {
		t.Errorf("exit code %d", code)
	}
	if !reflect.DeepEqual(gotThen, []string{"cat"}) {
		t.Errorf("then = %v", gotThen)
	}
	if want := "contr statusline: stdout closed\n"; stderr.String() != want {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestStatuslineWithBadInputPrintsNothingAndExitsZero(t *testing.T) {
	t.Setenv("CONTR_STATE_DIR", t.TempDir())
	var out, stderr bytes.Buffer
	if code := statusline(strings.NewReader("not json"), &out, &stderr, nil); code != 0 {
		t.Errorf("exit code %d", code)
	}
	if out.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("out %q, stderr %q", out.String(), stderr.String())
	}
}

func face(t *testing.T, name string) []byte {
	t.Helper()
	data, err := assets.Fonts.ReadFile("fonts/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// table returns the table with tag in a TrueType face, or nil when the
// directory or the table is cut short.
func table(face []byte, tag string) []byte {
	if len(face) < 6 {
		return nil
	}
	count := int(binary.BigEndian.Uint16(face[4:6]))
	for i := 0; i < count; i++ {
		rec := face
		if len(rec) < 28+16*i {
			return nil
		}
		rec = rec[12+16*i : 28+16*i]
		if string(rec[:4]) != tag {
			continue
		}
		off := int(binary.BigEndian.Uint32(rec[8:12]))
		n := int(binary.BigEndian.Uint32(rec[12:16]))
		if off+n > len(face) {
			return nil
		}
		return face[off : off+n]
	}
	return nil
}

// familyAndItalic reads the family name (Windows, Unicode) and the italic bit
// of a face, as the text system matches them. ok is false when a table is
// missing or cut short.
func familyAndItalic(face []byte) (family string, italic, ok bool) {
	os2 := table(face, "OS/2")
	name := table(face, "name")
	if len(os2) < 64 || len(name) < 6 {
		return "", false, false
	}
	italic = binary.BigEndian.Uint16(os2[62:64])&1 != 0
	count := int(binary.BigEndian.Uint16(name[2:4]))
	storage := int(binary.BigEndian.Uint16(name[4:6]))
	for i := 0; i < count; i++ {
		if len(name) < 18+12*i {
			break
		}
		rec := name[6+12*i : 18+12*i]
		field := func(at int) int { return int(binary.BigEndian.Uint16(rec[at:])) }
		if field(0) != 3 || field(6) != 1 {
			continue
		}
		start := storage + field(10)
		if start+field(8) > len(name) {
			break
		}
		raw := name[start : start+field(8)]
		var b strings.Builder
		for j := 0; j+1 < len(raw); j += 2 {
			b.WriteRune(rune(binary.BigEndian.Uint16(raw[j:])))
		}
		return b.String(), italic, true
	}
	return "", false, false
}

func TestFontsAreTruetype(t *testing.T) {
	if len(bundledFonts) != 4 {
		t.Errorf("%d fonts; want regular, bold, italic, bold italic", len(bundledFonts))
	}
	for _, name := range bundledFonts {
		if f := face(t, name); !bytes.HasPrefix(f, []byte{0, 1, 0, 0}) {
			t.Errorf("%s is not TrueType", name)
		}
	}
}

func TestTheItalicFacesAreTheTerminalFamilyAndItalic(t *testing.T) {
	// The text system matches the faces by family name and style. These are the same fields.
	for _, c := range []struct {
		name   string
		italic bool
	}{
		{"JetBrainsMono-Regular.ttf", false},
		{"JetBrainsMono-Bold.ttf", false},
		{"JetBrainsMono-Italic.ttf", true},
		{"JetBrainsMono-BoldItalic.ttf", true},
	} {
		family, italic, ok := familyAndItalic(face(t, c.name))
		if !ok || family != theme.FontFamily || italic != c.italic {
			t.Errorf("%s: family %q italic %v ok %v", c.name, family, italic, ok)
		}
	}
}

func TestAFaceCutShortHasNoFamily(t *testing.T) {
	italic := face(t, "JetBrainsMono-Italic.ttf")
	if _, _, ok := familyAndItalic(italic[:12]); ok {
		t.Error("cut face has a family")
	}
	if _, _, ok := familyAndItalic(nil); ok {
		t.Error("empty face has a family")
	}
}

func TestRegisterFontsLoadsEveryFaceUnderTheFamily(t *testing.T) {
	var families []string
	registerFonts(assets.Fonts, func(data []byte, family string) error {
		if len(data) == 0 {
			t.Error("empty font")
		}
		families = append(families, family)
		return nil
	})
	if len(families) != 4 {
		t.Fatalf("%d faces registered", len(families))
	}
	for _, f := range families {
		if f != theme.FontFamily {
			t.Errorf("family %q", f)
		}
	}
}

func TestRegisterFontsKeepsGoingAfterAFailure(t *testing.T) {
	n := 0
	registerFonts(assets.Fonts, func([]byte, string) error {
		n++
		return errors.New("bad font")
	})
	if n != 4 {
		t.Errorf("%d tries", n)
	}
}

func TestTitlebarHidesTheSystemTitleBar(t *testing.T) {
	// The Rust app made the title bar transparent on macOS. MyGo's hidden
	// style is the same look; the traffic light position is not settable.
	if got := windowOptions(false).TitleBarStyle; got != mygo.TitleBarHidden {
		t.Errorf("style %v", got)
	}
}

func TestWindowOptionsSizeTheFrameAndTheLegend(t *testing.T) {
	f := windowOptions(false)
	if f.Width != theme.WindowWidth || f.Height != theme.WindowHeight || f.MinWidth != 960 || f.MinHeight != 600 {
		t.Errorf("frame %+v", f)
	}
	l := windowOptions(true)
	if l.Width != 480 || l.Height != 420 || l.MinWidth != 0 || l.MinHeight != 0 {
		t.Errorf("legend %+v", l)
	}
}
