package apptest

import (
	"slices"
	"sync"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
)

// Source is a stub agentstate.AgentSource that lets a test set the snapshot and records
// the actions.
type Source struct {
	Snap      agentstate.Snapshot
	Cleared   int
	Answers   [][2]string // id, answer
	Dones     []string
	ActionErr error
	Told      [][2]string // name, text of Send
	TellErr   error
	Text      string // what Read returns
	ReadErr   error
	Reads     []string // names read
	Texts     []string // what the next Read calls return, before Text
	Sent      [][]byte // keys sent
	SentTo    []string
	SendErr   error
	Signalled []string // names interrupted
	Stopped   []string
	Closed    []string
	// Visible and Reconnects record SetVisible and Reconnect.
	Visible    []bool
	Reconnects int
	// Mu guards Stopped, Closed, Visible and Reconnects: the frame calls
	// Stop and Close on a goroutine.
	Mu sync.Mutex
}

func (s *Source) SetVisible(v bool) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Visible = append(s.Visible, v)
}

func (s *Source) Reconnect() {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Reconnects++
}

// Calls returns copies of the stopped and closed names.
func (s *Source) Calls() (stopped, closed []string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return slices.Clone(s.Stopped), slices.Clone(s.Closed)
}

func (s *Source) Snapshot() agentstate.Snapshot { return s.Snap }
func (s *Source) Answer(id, a string) error {
	s.Answers = append(s.Answers, [2]string{id, a})
	return s.ActionErr
}
func (s *Source) Done(id string) error {
	s.Dones = append(s.Dones, id)
	return s.ActionErr
}
func (s *Source) ClearDone() { s.Cleared++ }
func (s *Source) Read(name string, _ int) (string, error) {
	s.Reads = append(s.Reads, name)
	if len(s.Texts) > 0 {
		t := s.Texts[0]
		s.Texts = s.Texts[1:]
		return t, s.ReadErr
	}
	return s.Text, s.ReadErr
}
func (s *Source) Send(name, text string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Told = append(s.Told, [2]string{name, text})
	return s.TellErr
}
func (s *Source) SendKeys(name string, keys []byte) error {
	s.SentTo = append(s.SentTo, name)
	s.Sent = append(s.Sent, keys)
	return s.SendErr
}
func (s *Source) Interrupt(name string) error {
	s.Signalled = append(s.Signalled, name)
	return s.ActionErr
}
func (s *Source) Stop(name string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Stopped = append(s.Stopped, name)
	return s.ActionErr
}
func (s *Source) Close(name string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	s.Closed = append(s.Closed, name)
	return s.ActionErr
}

// BlockedWorker is a blocked worker of project with the message msg.
func BlockedWorker(name, project, msg string) agents.Worker {
	return agents.Worker{Name: name, Project: project, Status: "blocked", Msg: msg}
}

// Keys lists the keys of tabs.
func Keys(tabs []agentstate.NeedTab) []string {
	var out []string
	for _, t := range tabs {
		out = append(out, t.Key)
	}
	return out
}
