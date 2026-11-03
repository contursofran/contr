package herdr

// Commands that change herdr's state. Each opens one connection, sends one
// request and reads one reply.

import "time"

// command sends one request and checks the type of the reply.
func command(path string, timeout time.Duration, id, method string, params map[string]any, kind string) error {
	result, err := exchange(path, timeout, id, method, params)
	if err != nil {
		return err
	}
	return expectType(result, kind)
}

// FocusPane moves herdr's focus to paneID (`pane.focus`). herdr also
// switches to the tab and workspace that hold the pane.
func FocusPane(path, paneID string, timeout time.Duration) error {
	return command(path, timeout, "contr-focus", "pane.focus", focusPaneParams(paneID), "pane_info")
}

func focusPaneParams(paneID string) map[string]any {
	return map[string]any{"pane_id": paneID}
}
