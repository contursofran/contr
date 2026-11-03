package main

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartAgentsWithoutACoreShowsNoAgentsAndStops(t *testing.T) {
	env := map[string]string{
		"CONTR_HC":            filepath.Join(t.TempDir(), "no-such-hc"),
		"CONTR_AGENTS_CONFIG": filepath.Join(t.TempDir(), "agents.json"),
		"CONTR_OWNER_FILE":    filepath.Join(t.TempDir(), "owner.json"),
	}
	var notified atomic.Int32
	src, stop := startAgents(func(k string) string { return env[k] })(func() { notified.Add(1) })
	if s := src.Snapshot(); len(s.Workers)+len(s.Orchestrators) != 0 {
		t.Errorf("snapshot %+v, want empty", s)
	}
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
}
