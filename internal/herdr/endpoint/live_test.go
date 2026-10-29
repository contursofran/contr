//go:build live

// Live tests: the endpoint client against a real herdr server.
//
// They run only against the isolated server from scripts/dev-herdr.sh. They
// never touch the owner's herdr. A test skips when the environment does not
// point at that server. Run them once, by hand:
//
//	eval "$(scripts/dev-herdr.sh)"
//	go test -tags live -race ./internal/herdr/endpoint/...
//	scripts/dev-herdr.sh stop
//
// Each test makes its own workspace and checks only the pane of that
// workspace, so state from earlier runs cannot satisfy a check. The build tag
// keeps these tests out of scripts/check.sh.

package endpoint

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/herdr"
)

const (
	liveWait = 5 * time.Second
	livePoll = 50 * time.Millisecond
)

var (
	liveSize  = GridSize{Cols: 60, Rows: 12}
	liveSmall = GridSize{Cols: 40, Rows: 8}
	liveCell  = CellPx{Width: 8, Height: 16}
)

// liveSocket returns the API socket of the dev server, and skips the test
// when the environment does not name it. The socket must be the one that
// scripts/dev-herdr.sh makes, so that the tests cannot reach another herdr.
func liveSocket(t *testing.T) string {
	t.Helper()
	sock := os.Getenv("HERDR_SOCKET_PATH")
	if sock == "" || os.Getenv("CONTR_SURFACE") == "" {
		t.Skip("no dev herdr (check TMPDIR too): run eval \"$(scripts/dev-herdr.sh)\" first")
	}
	base := os.Getenv("TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	want := filepath.Join(strings.TrimSuffix(base, "/"), "contr-go-dev-herdr", "a.sock")
	if filepath.Clean(sock) != want {
		t.Skipf("HERDR_SOCKET_PATH %q is not the dev herdr socket %q", sock, want)
	}
	return sock
}

// livePane is a pane of a fresh workspace of the dev server.
type livePane struct {
	api      string // the API socket
	endpoint string // the endpoint socket
	ws, tab  string
	pane     string
}

// startLive starts the dev server if needed, makes a workspace and focuses
// its pane.
func startLive(t *testing.T) livePane {
	t.Helper()
	api := liveSocket(t)
	if out, err := exec.Command("../../../scripts/dev-herdr.sh").CombinedOutput(); err != nil {
		t.Fatalf("dev-herdr.sh: %v\n%s", err, out)
	}
	endpointSock := clientSibling(api)
	before := paneIDs(t, api)
	createWorkspace(t, api)
	snap, err := herdr.Snapshot(api, liveWait)
	if err != nil {
		t.Fatalf("take a snapshot: %v", err)
	}
	var fresh []herdr.WirePane
	for _, p := range snap.Panes {
		if !slices.Contains(before, p.PaneID) {
			fresh = append(fresh, p)
		}
	}
	if len(fresh) != 1 {
		t.Fatalf("workspace.create made %d new panes, want 1: %v", len(fresh), fresh)
	}
	p := fresh[0]
	if err := herdr.FocusPane(api, p.PaneID, liveWait); err != nil {
		t.Fatalf("focus the new pane: %v", err)
	}
	return livePane{api: api, endpoint: endpointSock, ws: p.WorkspaceID, tab: p.TabID, pane: p.PaneID}
}

func paneIDs(t *testing.T, api string) []string {
	t.Helper()
	snap, err := herdr.Snapshot(api, liveWait)
	if err != nil {
		t.Fatalf("take a snapshot: %v", err)
	}
	ids := make([]string, 0, len(snap.Panes))
	for _, p := range snap.Panes {
		ids = append(ids, p.PaneID)
	}
	return ids
}

// createWorkspace sends workspace.create as a raw JSON line on the API socket
// of the dev server.
func createWorkspace(t *testing.T, api string) {
	t.Helper()
	request, err := herdr.RequestLine("t", "workspace.create", map[string]any{"cwd": "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.DialTimeout("unix", api, liveWait)
	if err != nil {
		t.Fatalf("connect to the API socket: %v", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(liveWait))
	if _, err := c.Write([]byte(request)); err != nil {
		t.Fatalf("send workspace.create: %v", err)
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatalf("read the workspace.create reply: %v", err)
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &reply); err != nil || reply.Result == nil {
		t.Fatalf("workspace.create failed: %s (%v)", line, err)
	}
}

// open connects to the endpoint with the 60x12 grid. The read timeout is
// short, so that pump can check its deadline. The connection detaches and
// closes with the test.
func (lp livePane) open(t *testing.T) *Conn {
	t.Helper()
	conn, err := Open(lp.endpoint, liveSize, liveCell, liveWait)
	if err != nil {
		t.Fatalf("open the endpoint: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Send(DetachMsg{})
		_ = conn.Close()
	})
	conn.SetReadTimeout(livePoll)
	return conn
}

// shown is what the test has seen from the server so far.
type shown struct {
	snapshot *ShellSnapshot
	surface  *Surface
}

func (s *shown) sizeIs(size GridSize) bool {
	return s.surface != nil && s.surface.Size == size
}

// focusedPaneID is the id of the focused pane of the surface, or "".
func (s *shown) focusedPaneID() string {
	if s.surface == nil {
		return ""
	}
	if p := s.surface.FocusedPane(); p != nil {
		return p.PaneID
	}
	return ""
}

// scroll is the scroll position of the focused pane, or nil.
func (s *shown) scroll() *ScrollMetrics {
	if s.surface == nil {
		return nil
	}
	if p := s.surface.FocusedPane(); p != nil {
		return p.Scroll
	}
	return nil
}

// pump reads server messages into sh until done holds. It fails after
// liveWait.
func pump(t *testing.T, conn *Conn, sh *shown, what string, done func(*shown) bool) {
	t.Helper()
	deadline := time.Now().Add(liveWait)
	for !done(sh) {
		if time.Now().After(deadline) {
			t.Fatalf("no %s within %v", what, liveWait)
		}
		msg, err := conn.Recv()
		if err != nil {
			t.Fatalf("receive while waiting for %s: %v", what, err)
		}
		switch m := msg.(type) {
		case ControlMsg:
			if m.Kind != SnapshotKind {
				continue
			}
			var snap ShellSnapshot
			if err := json.Unmarshal([]byte(m.Data), &snap); err != nil {
				t.Fatalf("read the snapshot: %v", err)
			}
			sh.snapshot = &snap
		case SurfaceMsg:
			surface := m.Frame.Surface
			sh.surface = &surface
		case PatchMsg:
			if sh.surface == nil {
				t.Fatal("a patch before any surface")
			}
			if err := sh.surface.ApplyPatch(m.Patch); err != nil {
				t.Fatalf("a patch that does not continue the surface: %v", err)
			}
		case ShutdownMsg:
			t.Fatalf("herdr shut down: %v", m.Reason)
		case ShellErrorMsg:
			t.Fatalf("herdr shell error: %s", m.Text)
		}
	}
}

// ownPane is true once the surface focuses the pane of this test.
func (lp livePane) ownPane(s *shown) bool { return s.focusedPaneID() == lp.pane }

// rowText is the text of one row: each cell's text, in column order.
func rowText(row Row) string {
	var b strings.Builder
	for _, c := range row {
		b.WriteString(c.Text)
	}
	return b.String()
}

// findRed returns the row and column where "RED ok" starts on screen, inside
// the focused pane.
func findRed(s *Surface) (y, x int, ok bool) {
	for y, row := range s.Rows {
		for x := range row {
			end := min(x+6, len(row))
			if rowText(row[x:end]) == "RED ok" {
				return y, x, true
			}
		}
	}
	return 0, 0, false
}

// shows reports whether a row of the focused pane reads exactly line once
// its padding is trimmed. Only the inner columns count: herdr paints its
// scrollbar in the column after them.
func shows(s *shown, line string) bool {
	if s.surface == nil {
		return false
	}
	pane := s.surface.FocusedPane()
	if pane == nil {
		return false
	}
	width := int(pane.InnerRect.Width)
	for _, row := range s.surface.Rows {
		if strings.TrimSpace(rowText(row[:min(width, len(row))])) == line {
			return true
		}
	}
	return false
}

// wheel is one wheel step at the first cell of the pane.
func wheel(dir WheelDir, lines uint16) InputEvent {
	return WheelEvent{Dir: dir, Lines: lines}
}

// typeLine types text in the pane and presses Enter.
func typeLine(t *testing.T, conn *Conn, pane, text string) {
	t.Helper()
	err := conn.Send(InputMsg{PaneID: pane, Events: []InputEvent{
		TextEvent{Text: text},
		KeyEvent{Code: KeyCode{Kind: KeyEnter}},
	}})
	if err != nil {
		t.Fatalf("send input: %v", err)
	}
}

// TestHandshakeColourAndResizeAgainstAnIsolatedHerdr ports
// handshake_colour_and_resize_against_an_isolated_herdr.
func TestHandshakeColourAndResizeAgainstAnIsolatedHerdr(t *testing.T) {
	lp := startLive(t)
	conn := lp.open(t)
	t.Logf("herdr server_version=%s generation=%d", conn.Welcome.ServerVersion, conn.Welcome.Generation)
	if !conn.Version.Tested() {
		t.Logf("herdr %s is not in the tested list", conn.Version.Untested)
	}
	if conn.Welcome.Generation != EndpointGeneration {
		t.Errorf("generation = %d, want %d", conn.Welcome.Generation, EndpointGeneration)
	}

	var sh shown
	pump(t, conn, &sh, "snapshot and a 60x12 surface of the new pane", func(s *shown) bool {
		return s.snapshot != nil && s.sizeIs(liveSize) && lp.ownPane(s)
	})

	typeLine(t, conn, lp.pane, `printf '\033[1;31mRED\033[0m ok\n'`)
	pump(t, conn, &sh, "RED ok on screen", func(s *shown) bool {
		if s.surface == nil {
			return false
		}
		_, _, ok := findRed(s.surface)
		return ok
	})

	y, x, _ := findRed(sh.surface)
	red := sh.surface.Rows[y][x]
	t.Logf("RED ok at row %d, column %d: fg=%#010x attrs=%#06x", y, x, red.Fg, red.Attrs)
	if red.Fg != 0x0100_0001 {
		t.Errorf("fg = %#010x, want indexed colour 1 (red) 0x01000001", red.Fg)
	}
	if red.Attrs&1 != 1 {
		t.Errorf("attrs = %#06x, want the bold bit", red.Attrs)
	}

	if err := conn.Send(ResizeMsg{Size: liveSmall, Cell: liveCell}); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	pump(t, conn, &sh, "a 40x8 surface after the resize", func(s *shown) bool { return s.sizeIs(liveSmall) })
}

// TestTabFocusMovesTheEndpointSurface ports tab_focus_moves_the_endpoint_surface.
func TestTabFocusMovesTheEndpointSurface(t *testing.T) {
	lp := startLive(t)
	conn := lp.open(t)

	var sh shown
	pump(t, conn, &sh, "a surface of the new pane", lp.ownPane)
	t.Logf("first pane %s in tab %s", lp.pane, lp.tab)

	// tab.create sends focus: true, so the API focus change must move the surface.
	if err := herdr.CreateTab(lp.api, lp.ws, liveWait); err != nil {
		t.Fatalf("create a tab: %v", err)
	}
	pump(t, conn, &sh, "a surface of the new tab", func(s *shown) bool {
		id := s.focusedPaneID()
		return id != "" && id != lp.pane
	})
	t.Logf("new tab pane %s", sh.focusedPaneID())

	// The title-bar click path: tab.focus on the first tab moves the surface back.
	if err := herdr.FocusTab(lp.api, lp.tab, liveWait); err != nil {
		t.Fatalf("focus the first tab: %v", err)
	}
	pump(t, conn, &sh, "the first tab's surface again", lp.ownPane)
}

// TestWheelScrollsHistoryAndAKeyReturnsToLive ports
// wheel_scrolls_history_and_a_key_returns_to_live.
func TestWheelScrollsHistoryAndAKeyReturnsToLive(t *testing.T) {
	lp := startLive(t)
	conn := lp.open(t)

	var sh shown
	pump(t, conn, &sh, "snapshot and a 60x12 surface of the new pane", func(s *shown) bool {
		return s.snapshot != nil && s.sizeIs(liveSize) && lp.ownPane(s)
	})

	// 500 lines push the top of the output into herdr's history. "500" is the
	// last output line, so it is on screen until the view scrolls.
	typeLine(t, conn, lp.pane, "seq 1 500")
	pump(t, conn, &sh, "500 on screen", func(s *shown) bool { return shows(s, "500") })

	// A wheel step up moves the view into the history: "500" leaves the screen
	// and the pane reports an offset from the bottom.
	if err := conn.Send(InputMsg{PaneID: lp.pane, Events: []InputEvent{wheel(WheelUp, 20)}}); err != nil {
		t.Fatalf("send wheel up: %v", err)
	}
	pump(t, conn, &sh, "the history above the screen", func(s *shown) bool {
		m := s.scroll()
		return !shows(s, "500") && m != nil && m.OffsetFromBottom > 0
	})
	up := *sh.scroll()
	t.Logf("wheel up 20 lines: %+v", up)

	// A wheel step down moves the view back by 5 lines. Plan: 15 rows above
	// the bottom. If herdr reports another number, the offset must still fall.
	before := up.OffsetFromBottom
	if err := conn.Send(InputMsg{PaneID: lp.pane, Events: []InputEvent{wheel(WheelDown, 5)}}); err != nil {
		t.Fatalf("send wheel down: %v", err)
	}
	pump(t, conn, &sh, "the offset to change", func(s *shown) bool {
		m := s.scroll()
		return m != nil && m.OffsetFromBottom != before
	})
	after := sh.scroll().OffsetFromBottom
	t.Logf("wheel down 5 lines: offset %d (was %d)", after, before)
	if after != 15 {
		t.Logf("herdr reports offset %d, not 15; checking only that it fell", after)
	}
	if after >= before {
		t.Errorf("offset = %d after a wheel down, was %d: the view must move toward the bottom", after, before)
	}

	// A key returns to the live screen: "500" is back and the view is at the bottom.
	err := conn.Send(InputMsg{PaneID: lp.pane, Events: []InputEvent{KeyEvent{Code: KeyCode{Kind: KeyLeft}}}})
	if err != nil {
		t.Fatalf("send the key: %v", err)
	}
	pump(t, conn, &sh, "the live screen again", func(s *shown) bool {
		m := s.scroll()
		return shows(s, "500") && (m == nil || m.OffsetFromBottom == 0)
	})
	t.Logf("back to live: scroll %+v", sh.scroll())
}

// TestSurfacePaneIsTheFocusedPaneOfTheSurface ports
// surface_pane_is_the_focused_pane_of_the_surface. It needs no server.
func TestSurfacePaneIsTheFocusedPaneOfTheSurface(t *testing.T) {
	pane := func(id string, focused bool) SurfacePane { return SurfacePane{PaneID: id, Focused: focused} }
	var sh shown
	if got := sh.focusedPaneID(); got != "" {
		t.Errorf("no surface yet: got %q, want none", got)
	}
	sh.surface = &Surface{BootID: "b1", Size: liveSize, Panes: []SurfacePane{pane("w1:p1", false)}}
	if got := sh.focusedPaneID(); got != "" {
		t.Errorf("a surface with no focused pane: got %q, want none", got)
	}
	sh.surface.Panes = []SurfacePane{pane("w1:p1", false), pane("w1:p2", true)}
	if got := sh.focusedPaneID(); got != "w1:p2" {
		t.Errorf("got %q, want w1:p2", got)
	}
}
