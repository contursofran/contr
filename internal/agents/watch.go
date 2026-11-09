package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/contursofran/contr/internal/hc"
)

// The time limits of the watch's calls to the core (D223). subscribeLimit
// covers the subscribe: a dial, the hello and the snapshot; 20 s, as
// OpenTerminal. getLimit covers the terminal.get of one event; 10 s, as the
// hooks. A call that passes its limit ends the watch with
// context.DeadlineExceeded. Variables so tests can shorten them.
var (
	subscribeLimit = 20 * time.Second
	getLimit       = 10 * time.Second
)

// Watch follows the workers, finished ones included, until ctx ends or the
// event stream ends. fn gets the whole list, sorted by name, once after the
// snapshot and then after each change. fn runs on the caller's goroutine.
//
// hc does not document the data of each event type, so an event about a
// worker reloads that worker with terminal.get. A "removed" event drops it;
// "missed" replaces the list with the fresh snapshot that comes with it.
func (m *Manager) Watch(ctx context.Context, fn func([]Worker)) error {
	return m.watch(ctx, fn, false)
}

// WatchAll is Watch with the live terminals that contr did not start too
// (Worker.Foreign, D149). The agents command line and the MCP tools use
// Watch and List, which list only the workers that contr started.
func (m *Manager) WatchAll(ctx context.Context, fn func([]Worker)) error {
	return m.watch(ctx, fn, true)
}

func (m *Manager) watch(ctx context.Context, fn func([]Worker), foreign bool) error {
	f := hc.Filter{Labels: filter}
	if foreign {
		f = hc.Filter{}
	}
	sctx, cancel := context.WithTimeout(ctx, subscribeLimit)
	s, err := m.core.Subscribe(sctx, f)
	cancel() // the stream does not use sctx after Subscribe returns
	if err != nil {
		return err
	}
	defer s.Close()

	ws, err := fromSnapshot(s.Snapshot(), foreign)
	if err != nil {
		return err
	}
	fn(sorted(ws))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e, ok := <-s.Events():
			if !ok {
				if err := s.Err(); err != nil {
					return err
				}
				return hc.ErrClosed
			}
			changed, err := m.apply(ctx, ws, e, foreign)
			if err != nil {
				return err
			}
			if changed {
				fn(sorted(ws))
			}
		}
	}
}

// apply changes ws for one event and tells if anything changed.
func (m *Manager) apply(ctx context.Context, ws map[string]Worker, e hc.Event, foreign bool) (bool, error) {
	switch e.Method {
	case "heartbeat":
		return false, nil
	case "missed":
		var snap hc.Snapshot
		if err := json.Unmarshal(e.Data, &snap); err != nil {
			return false, fmt.Errorf("agents: bad missed snapshot: %w", err)
		}
		fresh, err := fromSnapshot(snap, foreign)
		if err != nil {
			return false, err
		}
		for k, v := range fresh { // a snapshot has no output time: keep ours
			v.LastOutput = ws[k].LastOutput
			fresh[k] = v
		}
		changed := !reflect.DeepEqual(fresh, ws)
		clear(ws)
		for k, v := range fresh {
			ws[k] = v
		}
		return changed, nil
	case "event":
	default:
		return false, nil
	}
	if e.Terminal == "" {
		return false, nil
	}
	old, had := ws[e.Terminal]
	if e.Type == "removed" {
		delete(ws, e.Terminal)
		return had, nil
	}
	var raw json.RawMessage
	gctx, cancel := context.WithTimeout(ctx, getLimit)
	err := m.core.Call(gctx, "terminal.get", map[string]any{"terminal": e.Terminal}, &raw)
	cancel()
	if hc.IsKind(err, "not_found") {
		delete(ws, e.Terminal)
		return had, nil
	}
	if err != nil {
		return false, err
	}
	var t terminal
	if err := json.Unmarshal(raw, &t); err != nil {
		return false, fmt.Errorf("agents: bad terminal: %w", err)
	}
	w := t.worker()
	if !keep(w, foreign) { // its labels changed, or a foreign terminal ended
		delete(ws, e.Terminal)
		return had, nil
	}
	// hc has no output time: stamp the event (D35). The stamp alone is not a
	// change, so a busy terminal does not repaint the UI; the next change
	// carries the newest stamp.
	stamp := m.now()
	w.LastOutput = old.LastOutput
	changed := !had || !reflect.DeepEqual(old, w)
	w.LastOutput = stamp
	ws[e.Terminal] = w
	return changed, nil
}

// keep tells if a watch lists w. A watch of contr's workers drops a terminal
// without contr labels. A watch of all terminals drops a foreign terminal
// that ended: the owner started it, so contr does not keep it (D112).
func keep(w Worker, foreign bool) bool {
	if w.Foreign {
		return foreign && w.Live
	}
	return true
}

func fromSnapshot(snap hc.Snapshot, foreign bool) (map[string]Worker, error) {
	ws := make(map[string]Worker, len(snap.Terminals))
	for _, raw := range snap.Terminals {
		w, err := decodeWorker(raw)
		if err != nil {
			return nil, err
		}
		if keep(w, foreign) {
			ws[w.Address] = w
		}
	}
	return ws, nil
}

func sorted(ws map[string]Worker) []Worker {
	out := make([]Worker, 0, len(ws))
	for _, w := range ws {
		out = append(out, w)
	}
	sortWorkers(out)
	return out
}
