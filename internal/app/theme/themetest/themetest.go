// Package themetest switches the current palette in tests (D119).
package themetest

import (
	"testing"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/app/theme"
)

type platform struct{}

func (platform) SetSource(mygo.ThemeSource)    {}
func (platform) IsDark() bool                  { return true }
func (platform) OnUpdated(func()) (off func()) { return func() {} }

type window struct{}

func (window) Update(fn func()) { fn() }

// Use makes the palette of src the current one, and puts the Dark palette
// back when the test ends.
func Use(t *testing.T, src mygo.ThemeSource) {
	t.Helper()
	theme.Follow(platform{}, window{}, src).Stop()
	t.Cleanup(func() {
		theme.ClearTints()
		theme.Follow(platform{}, window{}, mygo.ThemeDark).Stop()
	})
}

// Follow makes a Follower with src on a fake platform and window, and puts
// the Dark palette back when the test ends.
func Follow(t *testing.T, src mygo.ThemeSource) *theme.Follower {
	t.Helper()
	t.Cleanup(func() { theme.Follow(platform{}, window{}, mygo.ThemeDark).Stop() })
	return theme.Follow(platform{}, window{}, src)
}
