package term

// LinkStateKind is the kind of a [LinkState].
type LinkStateKind int

const (
	// LinkConnecting: connecting, or reconnecting.
	LinkConnecting LinkStateKind = iota + 1
	// LinkLive: connected.
	LinkLive
	// LinkNotRunning: the terminal source is not running or closed the
	// connection. Message is for the log.
	LinkNotRunning
	// LinkIncompatible: the source refused contr, or sent what contr cannot
	// read. Message is for the user.
	LinkIncompatible
)

// LinkState is the connection state, as the UI shows it. Compare with ==.
type LinkState struct {
	Kind LinkStateKind
	// ServerVersion is set for LinkLive.
	ServerVersion string
	// Untested is set for LinkLive when contr has not been tested with this
	// version.
	Untested bool
	// Message is set for LinkNotRunning and LinkIncompatible.
	Message string
}

// Snapshot is what a source says about the shell, without screen data.
type Snapshot struct {
	BootID        string
	Revision      uint64
	FocusedPaneID *string
}

// LinkUpdate is what the link tells the UI. The UI applies them in order.
// It is one of [StateUpdate], [ScreenUpdate], [SnapshotUpdate] and
// [ClipboardUpdate].
type LinkUpdate interface{ linkUpdate() }

// StateUpdate reports a new connection state.
type StateUpdate struct{ State LinkState }

// ScreenUpdate says the newest screen changed. Take it from the link: it can
// be newer than the Screen here, because updates coalesce. Treat Screen as
// read-only.
type ScreenUpdate struct{ Screen *Screen }

// SnapshotUpdate carries a shell snapshot.
type SnapshotUpdate struct{ Snapshot *Snapshot }

// ClipboardUpdate carries text a program wrote to the clipboard with OSC 52.
// The UI writes it to the system clipboard, once per update, in order.
type ClipboardUpdate struct{ Text string }

func (StateUpdate) linkUpdate()     {}
func (ScreenUpdate) linkUpdate()    {}
func (SnapshotUpdate) linkUpdate()  {}
func (ClipboardUpdate) linkUpdate() {}
