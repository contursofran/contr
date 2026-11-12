// The flow pickup: a coordinator gets the flow and the profiles' models again
// when the owner changes them in agents.json during the session (D293).

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/contursofran/contr/internal/agents"
)

// flowChangedLead starts the context of a changed flow.
const flowChangedLead = "contr: the owner changed the flow and models in agents.json; follow these from now on.\n"

// flowSeenDir is the folder in the state folder that holds the last-seen
// flow hash of each terminal.
const flowSeenDir = "flow-seen"

// flowState is the flow text and one line per profile with its model and
// effort, in name order.
func flowState(getenv func(string) string) (string, error) {
	cfg, err := agents.LoadConfig(configPath(getenv))
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(cfg.Flow.Text())
	b.WriteString("\nProfiles:\n")
	for _, n := range names {
		p := cfg.Profiles[n]
		fmt.Fprintf(&b, "- %s: model %s, effort %s\n", n, orText(p.Model(), "default"), orText(p.Effort(), "default"))
	}
	return b.String(), nil
}

func flowHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func flowSeenPath(stateDir, id string) string {
	return filepath.Join(stateDir, flowSeenDir, filepath.Base(id))
}

// recordFlow saves the hash of the current flow as seen by terminal id.
func recordFlow(getenv func(string) string, stateDir, id string) error {
	text, err := flowState(getenv)
	if err != nil {
		return err
	}
	return saveFlowSeen(stateDir, id, flowHash(text))
}

func saveFlowSeen(stateDir, id, hash string) error {
	if stateDir == "" || id == "" {
		return nil
	}
	p := flowSeenPath(stateDir, id)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(hash), 0o600)
}

// flowChange is the context for a coordinator when the flow or the models
// changed since terminal id last saw them, else "". With no saved hash it
// only saves one: the session start missed it, so there is nothing to compare.
// hc is asked for the role only after a change, so most calls do no I/O but
// two small file reads.
func flowChange(getenv func(string) string, dial func(context.Context, func(string) string) (agentsCore, error), stateDir, id string) (string, error) {
	if stateDir == "" {
		return "", nil
	}
	text, err := flowState(getenv)
	if err != nil {
		return "", err
	}
	hash := flowHash(text)
	seen, err := os.ReadFile(flowSeenPath(stateDir, id))
	if errors.Is(err, os.ErrNotExist) {
		return "", saveFlowSeen(stateDir, id, hash)
	}
	if err != nil {
		return "", err
	}
	if string(seen) == hash {
		return "", nil
	}
	// Save first: a failed role check does not ask hc again on each call.
	if err := saveFlowSeen(stateDir, id, hash); err != nil {
		return "", err
	}
	coord, err := isCoordinatorTerminal(getenv, dial, id)
	if err != nil || !coord {
		return "", err
	}
	return flowChangedLead + text, nil
}

// isCoordinatorTerminal tells whether hc terminal id orchestrates: it has the
// label contr.orchestrating or role orchestrator.
func isCoordinatorTerminal(getenv func(string) string, dial func(context.Context, func(string) string) (agentsCore, error), id string) (bool, error) {
	if dial == nil {
		return false, errors.New("no hc connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	core, err := dial(ctx, getenv)
	if err != nil {
		return false, err
	}
	defer func() { _ = core.Close() }()
	w, err := agents.New(core, agents.Config{}).Get(ctx, id)
	if err != nil {
		return false, err
	}
	return w.Orchestrating || w.Orchestrator(), nil
}
