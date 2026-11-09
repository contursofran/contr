package app

// NewTerminal opens a plain hc terminal in the open project: the "+" in the
// title bar and Cmd-T (#246, D82). Its methods run on the UI thread; Open runs
// off it.
type NewTerminal struct {
	// Open starts a shell in an hc terminal and returns its address. project is
	// the open project and dir the start folder; either may be empty. It
	// blocks, so Start runs it off the UI thread. Nil makes Start do nothing.
	Open func(project, dir string) (addr string, err error)
	// Env runs Open off the UI thread and shows its error.
	Env Env
	// Select selects the new terminal. It runs on the UI thread; may be nil.
	Select func(addr string)

	busy bool // an open is running
}

// Start opens a terminal. A second call while the first runs does nothing.
func (n *NewTerminal) Start(project, dir string) {
	if n.Open == nil || n.busy {
		return
	}
	n.busy = true
	n.Env.Do(func() func() {
		addr, err := n.Open(project, dir)
		return func() {
			n.busy = false
			if err != nil {
				n.Env.Show("Terminal: " + err.Error())
				return
			}
			if n.Select != nil {
				n.Select(addr)
			}
		}
	})
}
