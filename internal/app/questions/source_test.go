package questions

import (
	"context"
	"encoding/json"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/hc"
)

// source is the stub AgentSource of the tests.
type source = apptest.Source

// needsCore is a fake hc core: a fixed snapshot, no events.
type needsCore struct {
	snap  hc.Snapshot
	sends []string
}

func (f *needsCore) Call(_ context.Context, method string, params, _ any) error {
	if method == "terminal.send" {
		b, _ := json.Marshal(params)
		f.sends = append(f.sends, string(b))
	}
	return nil
}

func (f *needsCore) Subscribe(context.Context, hc.Filter) (agents.Stream, error) {
	return &needsStream{f.snap}, nil
}

type needsStream struct{ snap hc.Snapshot }

func (s *needsStream) Snapshot() hc.Snapshot   { return s.snap }
func (s *needsStream) Events() <-chan hc.Event { return nil }
func (s *needsStream) Err() error              { return nil }
func (s *needsStream) Close() error            { return nil }

func needsTerm(addr, name, project, status, msg string) json.RawMessage {
	t := map[string]any{
		"address": addr, "name": name, "state": "running", "turn": 1,
		"labels": map[string]string{"contr": "agent", "role": "builder", "project": project},
	}
	if status != "" {
		t["status"] = map[string]any{"state": status, "msg": msg}
	}
	b, _ := json.Marshal(t)
	return b
}
