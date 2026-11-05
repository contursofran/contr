package app

// viewersEnabled is the one switch for the v0.2 parts that the redesign (tracker #333) has not
// replaced yet: the board panel, the worker detail and the PR viewer. The owner disabled them
// until they are redesigned (D127, D147; issue #352). The code and its tests stay.
//
// While it is false, no button, key, menu or click opens them: the frame does not draw the PR
// pane, the board and worker panel, and [Frame.OpenPR] does nothing. Rows and cards that would
// open the pane are built without that action. Tests of the parts set it with enableViewers.
var viewersEnabled = false

// ViewersEnabled reports the switch above, so an e2e test of a hidden part can skip itself
// while the part is hidden and run again when the switch is on.
func ViewersEnabled() bool { return viewersEnabled }
