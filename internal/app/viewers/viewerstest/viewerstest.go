// Package viewerstest holds test helpers that build viewers values on the fakes of apptest, for the
// tests of internal/app. Only _test.go files import it. The tests of viewers do not, because this
// package imports viewers.
package viewerstest

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/app/viewers"
	"github.com/contursofran/contr/internal/github"
)

// PRRig is a PR pane on a fake gh runner that answers [apptest.PRJSON], with the reads held in
// Queue until [PRRig.Flush].
type PRRig struct {
	Pane   *viewers.PRPane
	Run    *apptest.PRRunner
	TT     *ui.Tester
	Queue  []func()
	Opened []string
	Closed int
}

// NewPRRig makes the rig with a tester of the pane.
func NewPRRig(t *testing.T) *PRRig {
	t.Helper()
	r := &PRRig{Run: &apptest.PRRunner{Reply: func(string) ([]byte, error) { return []byte(apptest.PRJSON), nil }}}
	r.Pane = &viewers.PRPane{
		Client: func(dir string) github.Client { return github.Client{R: r.Run, Dir: dir} },
		Env: view.Env{Run: view.Runner{Go: func(f func()) { r.Queue = append(r.Queue, f) }},
			OpenURL: func(u string) { r.Opened = append(r.Opened, u) }},
		Now:     func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) },
		OnClose: func() { r.Closed++ },
	}
	r.TT = ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Font(theme.FontFamily).FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).
			TextColor(theme.Color(theme.Text)).Children(func() { r.Pane.View(c) })
	}, 900, 700)
	return r
}

// Flush runs the held reads and builds a frame.
func (r *PRRig) Flush() {
	q := r.Queue
	r.Queue = nil
	for _, f := range q {
		f()
	}
	r.TT.Frame()
}
