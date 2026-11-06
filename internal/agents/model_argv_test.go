package agents

import (
	"encoding/json"
	"testing"
)

func TestWorkerModelIsTheModelOfItsOwnCommandLine(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"claude", "--model", "opus", "--effort", "low"}, "opus"},
		{[]string{"claude", "--model=sonnet"}, "sonnet"},
		{[]string{"claude"}, ""},
		{[]string{"claude", "--model"}, ""},
	}
	for _, c := range cases {
		raw, _ := json.Marshal(map[string]any{"address": "t1", "state": "running", "argv": c.argv})
		w, err := decodeWorker(raw)
		if err != nil {
			t.Fatal(err)
		}
		if w.Model != c.want {
			t.Errorf("argv %v: model %q, want %q", c.argv, w.Model, c.want)
		}
	}
}
