package herdr

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const (
	paneInfo = `{"id":"contr-focus","result":{"type":"pane_info","pane":{"pane_id":"w1:p1"}}}`
	tabInfo  = `{"id":"contr-tab-focus","result":{"type":"tab_info","tab":{"tab_id":"w1:t1"}}}`
)

func serverReply(id, code, message string) string {
	return `{"id":"` + id + `","error":{"code":"` + code + `","message":"` + message + `"}}`
}

func wantServerCode(t *testing.T, err error, code string) {
	t.Helper()
	if we := wantKind(t, err, ErrServer); we.Code != code {
		t.Fatalf("code = %q, want %q", we.Code, code)
	}
}

// wantRequest checks id, method and params of the request line that the
// fake server read.
func wantRequest(t *testing.T, server fakeServer, id, method string, params map[string]any) {
	t.Helper()
	request := requestJSON(t, server.Request(t))
	if request["id"] != id || request["method"] != method {
		t.Fatalf("%v", request)
	}
	if !reflect.DeepEqual(request["params"], params) {
		t.Fatalf("params = %v, want %v", request["params"], params)
	}
}

func TestFocusPaneParamsIsThePaneTarget(t *testing.T) {
	want := map[string]any{"pane_id": "w1:p2"}
	if got := focusPaneParams("w1:p2"); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestFocusPaneSendsPaneFocusAndAcceptsPaneInfo(t *testing.T) {
	path, server := serve(t, tempDir(t), []string{paneInfo}, 0)
	if err := FocusPane(path, "w1:p1", tLong); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, server, "contr-focus", "pane.focus", map[string]any{"pane_id": "w1:p1"})
}

func TestFocusPaneMapsPaneNotFoundToAServerError(t *testing.T) {
	reply := serverReply("contr-focus", "pane_not_found", "no such pane")
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	wantServerCode(t, FocusPane(path, "w9:p9", tLong), "pane_not_found")
	server.Request(t)
}

func TestFocusPaneRejectsAResultOfAnotherType(t *testing.T) {
	reply := `{"id":"contr-focus","result":{"type":"pong"}}`
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	wantKind(t, FocusPane(path, "w1:p1", tLong), ErrUnexpected)
	server.Request(t)
}

func TestFocusPaneTimesOutWhenServerIsSilent(t *testing.T) {
	path, server := serve(t, tempDir(t), nil, 600*time.Millisecond)
	err := FocusPane(path, "w1:p1", short)
	we := wantKind(t, err, ErrIO)
	if !errors.Is(we, os.ErrDeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	if we.IsNotRunning() {
		t.Fatal("a timeout is not 'not running'")
	}
	server.Request(t)
}

func TestFocusPaneMissingSocketIsNotRunning(t *testing.T) {
	err := FocusPane(filepath.Join(tempDir(t), "missing.sock"), "w1:p1", tLong)
	if we := wireErr(t, err); !we.IsNotRunning() {
		t.Fatalf("%v", err)
	}
}
