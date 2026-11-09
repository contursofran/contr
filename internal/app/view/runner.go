package view

// Runner runs work off the UI thread and hands the result back to it. The
// views call hc and owner.json through it, so a slow core never blocks a
// frame (AGENTS.md). Tests give it a runner that works at once.
type Runner struct {
	// Go runs work on another goroutine. Nil means a new goroutine.
	Go func(func())
	// Post runs fn on the UI thread (Window.Update). Nil runs it at once.
	Post func(func())
}

// Do runs work off the UI thread. The function that work returns runs on it.
func (r Runner) Do(work func() func()) {
	run := func() {
		done := work()
		if done == nil {
			return
		}
		if r.Post == nil {
			done()
			return
		}
		r.Post(done)
	}
	if r.Go == nil {
		go run()
		return
	}
	r.Go(run)
}
