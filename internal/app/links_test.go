package app

import (
	"reflect"
	"testing"
	"time"

	gterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/ghostty"
)

func TestOpenableLink(t *testing.T) {
	for link, want := range map[string]bool{
		"https://example.com/a?b=1": true,
		"http://example.com":        true,
		"HTTPS://EXAMPLE.COM":       true,
		"mailto:me@example.com":     true,
		"file:///etc/passwd":        false,
		"ssh://host":                false,
		"javascript:alert(1)":       false,
		"x-app://run":               false,
		"/abs/path.go":              false,
		"~/notes.md":                false,
		"src/app.go:12":             false,
		"":                          false,
		"http://%zz":                false,
	} {
		if got := openableLink(link); got != want {
			t.Errorf("openableLink(%q) = %v, want %v", link, got, want)
		}
	}
}

func TestTerminalOpenLinkTakesEveryLink(t *testing.T) {
	var opened []string
	open := terminalOpenLink(func(u string) { opened = append(opened, u) })
	for _, l := range []string{"https://a.test", "file:///x", "main.go:3", "mailto:a@b.test"} {
		if !open(l) {
			t.Errorf("OpenLink(%q) = false, want true: the plugin must not open it", l)
		}
	}
	if want := []string{"https://a.test", "mailto:a@b.test"}; !reflect.DeepEqual(opened, want) {
		t.Errorf("opened %q, want %q", opened, want)
	}
}

// linkTester shows a terminal with text fed to it. Opened collects the
// addresses opened.
func linkTester(t *testing.T, text string) (*ui.Tester, *[]string) {
	t.Helper()
	var opened []string
	conn := ghostty.NewConn(&scriptSource{}, ghostty.Options{Watch: true})
	term, err := gterm.New(terminalOptions(conn, func(u string) { opened = append(opened, u) }))
	if err != nil {
		t.Skipf("terminal plugin unavailable: %v", err)
	}
	t.Cleanup(func() { _ = term.Close() })
	tt := ui.NewTester(func(c *ui.Context) { gterm.View(c, term).Fill().AutoFocus() }, 600, 400)
	tt.Frame()
	term.Feed([]byte(text))
	time.Sleep(50 * time.Millisecond)
	tt.Frame()
	return tt, &opened
}

// rowHeight is about one terminal row in pixels.
const rowHeight = 20

func TestCmdClickOpensWebAddressOnce(t *testing.T) {
	tt, opened := linkTester(t, "see https://example.com/pr/1 now\r\nfile:///etc/hosts\r\n")
	// The terminal has no padding of its own (the view adds it); each cell is
	// about 8 px wide and row 0 is the first 20 px.
	x, y := float32(60), float32(10)
	tt.ClickAt(x, y)
	if len(*opened) != 0 {
		t.Fatalf("a plain click opened %q", *opened)
	}
	tt.ClickAtWith(ui.Super, x, y)
	if want := []string{"https://example.com/pr/1"}; !reflect.DeepEqual(*opened, want) {
		t.Errorf("Cmd-click opened %q, want %q", *opened, want)
	}
	// A scheme that is not allowed does nothing, and the system opens nothing.
	tt.ClickAtWith(ui.Super, 20, 10+rowHeight)
	if len(*opened) != 1 || len(tt.OpenedURLs()) != 0 {
		t.Errorf("file link: opened %q, system %q", *opened, tt.OpenedURLs())
	}
}
