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
	paneInfo      = `{"id":"contr-focus","result":{"type":"pane_info","pane":{"pane_id":"w1:p1"}}}`
	tabInfo       = `{"id":"contr-tab-focus","result":{"type":"tab_info","tab":{"tab_id":"w1:t1"}}}`
	tabCreated    = `{"id":"contr-tab-create","result":{"type":"tab_created","tab":{"tab_id":"w1:t2"},"root_pane":{"pane_id":"w1:p3"}}}`
	splitPaneInfo = `{"id":"contr-split","result":{"type":"pane_info","pane":{"pane_id":"w1:p3"}}}`
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

func TestFocusTabParamsIsTheTabTarget(t *testing.T) {
	want := map[string]any{"tab_id": "w1:t2"}
	if got := focusTabParams("w1:t2"); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestCreateTabParamsIsTheWorkspaceAndFocus(t *testing.T) {
	want := map[string]any{"workspace_id": "w1", "focus": true}
	if got := createTabParams("w1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestSplitRightParamsIsThePaneToTheRightFocused(t *testing.T) {
	want := map[string]any{"target_pane_id": "w1:p2", "direction": "right", "focus": true}
	if got := splitRightParams("w1:p2"); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestFocusTabSendsTabFocusAndAcceptsTabInfo(t *testing.T) {
	path, server := serve(t, tempDir(t), []string{tabInfo}, 0)
	if err := FocusTab(path, "w1:t1", tLong); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, server, "contr-tab-focus", "tab.focus", map[string]any{"tab_id": "w1:t1"})
}

func TestFocusTabMapsTabNotFoundToAServerError(t *testing.T) {
	reply := serverReply("contr-tab-focus", "tab_not_found", "no such tab")
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	wantServerCode(t, FocusTab(path, "w9:t9", tLong), "tab_not_found")
	server.Request(t)
}

func TestFocusTabMissingSocketIsNotRunning(t *testing.T) {
	err := FocusTab(filepath.Join(tempDir(t), "missing.sock"), "w1:t1", tLong)
	if we := wireErr(t, err); !we.IsNotRunning() {
		t.Fatalf("%v", err)
	}
}

func TestCreateTabSendsTabCreateFocusedAndAcceptsTabCreated(t *testing.T) {
	path, server := serve(t, tempDir(t), []string{tabCreated}, 0)
	if err := CreateTab(path, "w1", tLong); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, server, "contr-tab-create", "tab.create",
		map[string]any{"workspace_id": "w1", "focus": true})
}

func TestCreateTabMapsWorkspaceNotFoundToAServerError(t *testing.T) {
	reply := serverReply("contr-tab-create", "workspace_not_found", "no such workspace")
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	wantServerCode(t, CreateTab(path, "w9", tLong), "workspace_not_found")
	server.Request(t)
}

func TestCreateTabRejectsAResultOfAnotherType(t *testing.T) {
	path, server := serve(t, tempDir(t), []string{tabInfo}, 0)
	wantKind(t, CreateTab(path, "w1", tLong), ErrUnexpected)
	server.Request(t)
}

func TestSplitRightSendsPaneSplitRightAndAcceptsPaneInfo(t *testing.T) {
	path, server := serve(t, tempDir(t), []string{splitPaneInfo}, 0)
	if err := SplitRight(path, "w1:p2", tLong); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, server, "contr-split", "pane.split",
		map[string]any{"target_pane_id": "w1:p2", "direction": "right", "focus": true})
}

func TestSplitRightMapsPaneNotFoundToAServerError(t *testing.T) {
	reply := serverReply("contr-split", "pane_not_found", "no such pane")
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	wantServerCode(t, SplitRight(path, "w9:p9", tLong), "pane_not_found")
	server.Request(t)
}
