package agents

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/contursofran/contr/internal/hc"
)

// Watch follows the workers, finished ones included, until ctx ends or the
// event stream ends. fn gets the whole list, sorted by name, once after the
// snapshot and then after each change. fn runs on the caller's goroutine.
//
// hc does not document the data of each event type, so an event about a
// worker reloads that worker with terminal.get. A "removed" event drops it;
// "missed" replaces the list with the fresh snapshot that comes with it.
func (m *Manager) Watch(ctx context.Context, fn func([]Worker)) error {
	s, err := m.core.Subscribe(ctx, hc.Filter{Labels: filter})
	if err != nil {
		return err
	}
	defer s.Close()

	ws, err := fromSnapshot(s.Snapshot())
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
			changed, err := m.apply(ctx, ws, e)
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
func (m *Manager) apply(ctx context.Context, ws map[string]Worker, e hc.Event) (bool, error) {
	switch e.Method {
	case "heartbeat":
		return false, nil
	case "missed":
		var snap hc.Snapshot
		if err := json.Unmarshal(e.Data, &snap); err != nil {
			return false, err
		}
		fresh, err := fromSnapshot(snap)
		if err != nil {
			return false, err
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
	err := m.core.Call(ctx, "terminal.get", map[string]any{"terminal": e.Terminal}, &raw)
	if hc.IsKind(err, "not_found") {
		delete(ws, e.Terminal)
		return had, nil
	}
	if err != nil {
		return false, err
	}
	w, err := decodeWorker(raw)
	if err != nil {
		return false, err
	}
	ws[e.Terminal] = w
	return !had || !reflect.DeepEqual(old, w), nil
}

func fromSnapshot(snap hc.Snapshot) (map[string]Worker, error) {
	ws := make(map[string]Worker, len(snap.Terminals))
	for _, raw := range snap.Terminals {
		w, err := decodeWorker(raw)
		if err != nil {
			return nil, err
		}
		ws[w.Address] = w
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
