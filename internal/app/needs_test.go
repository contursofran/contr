package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/viewers"
)

// source is the stub AgentSource of the tests.
type source = apptest.Source

func TestNeedsSectionLongNameStaysOnOneLine(t *testing.T) {
	name := "b148-s3-card-with-a-very-long-worker-name"
	tabs := []agentstate.NeedTab{{Kind: agentstate.NeedBlocked, Key: name, Icon: icons.Blocked}}
	tt := ui.NewTester(func(c *ui.Context) { viewers.NeedsSection(c, tabs, nil) }, 400, 200)
	row, ok := tt.Find(name + " , blocked")
	if !ok {
		t.Fatal("no needs line")
	}
	if row.H != theme.BoardLineHeight {
		t.Fatalf("line height %v, want %v", row.H, theme.BoardLineHeight)
	}
	key, ok := tt.Find(name)
	if !ok {
		t.Fatal("no name text")
	}
	if key.H > theme.BoardLineHeight {
		t.Errorf("name height %v: it wrapped", key.H)
	}
	if key.W < 100 {
		t.Errorf("name width %v: it is squeezed", key.W)
	}
}
