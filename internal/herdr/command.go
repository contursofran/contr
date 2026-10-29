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

// FocusTab makes tabID herdr's focused tab, and its workspace the focused
// workspace (`tab.focus`).
func FocusTab(path, tabID string, timeout time.Duration) error {
	return command(path, timeout, "contr-tab-focus", "tab.focus", focusTabParams(tabID), "tab_info")
}

func focusTabParams(tabID string) map[string]any {
	return map[string]any{"tab_id": tabID}
}

// CreateTab makes a tab in workspaceID, focused (`tab.create`).
func CreateTab(path, workspaceID string, timeout time.Duration) error {
	return command(path, timeout, "contr-tab-create", "tab.create", createTabParams(workspaceID), "tab_created")
}

func createTabParams(workspaceID string) map[string]any {
	return map[string]any{"workspace_id": workspaceID, "focus": true}
}

// SplitRight splits paneID to the right (`pane.split`, decision D20). The new
// pane is focused.
func SplitRight(path, paneID string, timeout time.Duration) error {
	return command(path, timeout, "contr-split", "pane.split", splitRightParams(paneID), "pane_info")
}

func splitRightParams(paneID string) map[string]any {
	return map[string]any{"target_pane_id": paneID, "direction": "right", "focus": true}
}
