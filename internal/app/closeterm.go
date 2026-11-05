package app

// CloseTerminal stops the selected plain terminal: Cmd-W and File > Close
// Terminal (#246, S4, D85). Its methods run on the UI thread; Stop runs off it.
type CloseTerminal struct {
	// Stop ends the hc terminal with an address. It blocks, so Start runs it
	// off the UI thread. Nil makes Start do nothing.
	Stop func(addr string) error
	Run  Runner
	// Status shows a line as a toast; may be nil.
	Status func(string)
	// Closed runs on the UI thread after Stop worked; may be nil.
	Closed func(addr string)

	busy map[string]bool // addresses with a stop running
}

// Start stops the terminal. A second call for the same address while the
// first runs does nothing.
func (c *CloseTerminal) Start(addr string) {
	if c.Stop == nil || addr == "" || c.busy[addr] {
		return
	}
	if c.busy == nil {
		c.busy = map[string]bool{}
	}
	c.busy[addr] = true
	c.Run.Do(func() func() {
		err := c.Stop(addr)
		return func() {
			delete(c.busy, addr)
			if err != nil {
				if c.Status != nil {
					c.Status("Close terminal: " + err.Error())
				}
				return
			}
			if c.Closed != nil {
				c.Closed(addr)
			}
		}
	})
}
