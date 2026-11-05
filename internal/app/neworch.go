package app

import "errors"

// ErrPickCancelled is what a folder picker returns when the owner closes it
// without a choice. Starting an orchestrator then does nothing.
var ErrPickCancelled = errors.New("folder picker cancelled")

// NewOrchestrator starts an orchestrator in a folder that the owner picks
// (the `+ new` button next to Spaces, D57). Its methods run on the UI thread;
// the picker and the spawn run off it.
type NewOrchestrator struct {
	// Pick asks the owner for a folder. It blocks until the owner answers and
	// returns ErrPickCancelled for no choice. Nil makes Start do nothing.
	Pick func() (string, error)
	// Spawn starts the orchestrator profile in a folder.
	Spawn func(dir string) error
	Run   Runner
	// Status shows a line as a toast; may be nil.
	Status func(string)

	busy bool // a pick or spawn is running
}

// Start picks a folder, then spawns an orchestrator there. A second click
// while the first runs does nothing.
func (n *NewOrchestrator) Start() {
	if n.Pick == nil || n.Spawn == nil || n.busy {
		return
	}
	n.busy = true
	n.Run.Do(func() func() {
		dir, err := n.Pick()
		if err == nil {
			err = n.Spawn(dir)
		}
		return func() {
			n.busy = false
			switch {
			case errors.Is(err, ErrPickCancelled):
			case err != nil:
				n.show("New orchestrator: " + err.Error())
			}
		}
	})
}

// StartIn spawns an orchestrator in dir with no picker: the `+ new` of an
// empty project group (D116).
func (n *NewOrchestrator) StartIn(dir string) {
	if n.Spawn == nil || n.busy {
		return
	}
	n.busy = true
	n.Run.Do(func() func() {
		err := n.Spawn(dir)
		return func() {
			n.busy = false
			if err != nil {
				n.show("New orchestrator: " + err.Error())
			}
		}
	})
}

func (n *NewOrchestrator) show(s string) {
	if n.Status != nil {
		n.Status(s)
	}
}
