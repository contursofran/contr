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

func TestTheHookLabelsWinOverArgv(t *testing.T) {
	argv := []string{"claude", "--model", "opus", "--effort", "low"}
	cases := []struct {
		labels             map[string]string
		wantModel, wantEff string
	}{
		{map[string]string{LabelModel: "fable", LabelEffort: "high"}, "fable", "high"},
		{nil, "opus", "low"},
		{map[string]string{LabelModel: "fable"}, "fable", "low"},
		{map[string]string{LabelEffort: "high"}, "opus", "high"},
		{map[string]string{LabelModel: "", LabelEffort: ""}, "opus", "low"},
	}
	for _, c := range cases {
		raw, _ := json.Marshal(map[string]any{"address": "t1", "state": "running", "argv": argv, "labels": c.labels})
		w, err := decodeWorker(raw)
		if err != nil {
			t.Fatal(err)
		}
		if w.Model != c.wantModel || w.Effort != c.wantEff {
			t.Errorf("labels %v: model %q effort %q, want %q %q", c.labels, w.Model, w.Effort, c.wantModel, c.wantEff)
		}
	}
}
