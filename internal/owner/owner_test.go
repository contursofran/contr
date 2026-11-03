package owner

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func store(t *testing.T) *Store {
	t.Helper()
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	return &Store{Path: filepath.Join(t.TempDir(), "sub", "owner.json"), Now: func() time.Time { return at }}
}

func one() *int { n := 1; return &n }

func TestAskPinAndOpen(t *testing.T) {
	s := store(t)
	st, err := s.Read()
	if err != nil || len(st.Questions) != 0 {
		t.Fatalf("missing file: %+v, %v", st, err)
	}
	q, err := s.Ask(Question{Project: "contr", Coordinator: "t1", Question: "Which layout?", Options: []string{"A", "B"}, Recommended: one()})
	if err != nil || q.ID != "D1" || q.Asked.IsZero() {
		t.Fatalf("ask = %+v, %v", q, err)
	}
	q2, _ := s.Ask(Question{Project: "aerum", Question: "Ship it?"})
	if q2.ID != "D2" {
		t.Errorf("second id = %s", q2.ID)
	}
	todo, err := s.Pin(Todo{Project: "contr", Text: "Test the build"})
	if err != nil || todo.ID != "T1" || todo.Kind != "do" {
		t.Fatalf("pin = %+v, %v", todo, err)
	}
	if _, err := s.Pin(Todo{Project: "contr", Kind: "know", Text: "CI is slow"}); err != nil {
		t.Fatal(err)
	}

	st, _ = s.Read()
	qs, ts := st.Open("contr")
	if len(qs) != 1 || qs[0].ID != "D1" || len(ts) != 2 {
		t.Errorf("open contr = %+v %+v", qs, ts)
	}
	if qs, _ := st.Open(""); len(qs) != 2 {
		t.Errorf("open all = %d questions", len(qs))
	}
	info, err := os.Stat(s.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, %v", info, err)
	}
}

func TestAskAndPinCheckTheirInput(t *testing.T) {
	s := store(t)
	bad := 2
	for _, q := range []Question{{}, {Question: "x", Options: []string{"a"}, Recommended: &bad}} {
		if _, err := s.Ask(q); err == nil {
			t.Errorf("ask %+v: no error", q)
		}
	}
	for _, td := range []Todo{{}, {Text: "x", Kind: "maybe"}} {
		if _, err := s.Pin(td); err == nil {
			t.Errorf("pin %+v: no error", td)
		}
	}
	if _, err := os.Stat(s.Path); !os.IsNotExist(err) {
		t.Errorf("a refused item wrote the file: %v", err)
	}
}

func TestDecideAndUnpin(t *testing.T) {
	s := store(t)
	for range 3 {
		if _, err := s.Ask(Question{Question: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Pin(Todo{Text: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Decide([]string{"D1", "D3"}, "B", "owner"); err != nil {
		t.Fatal(err)
	}
	st, _ := s.Read()
	if st.Questions[0].Answer != "B" || st.Questions[0].AnsweredBy != "owner" || st.Questions[0].Answered.IsZero() || st.Questions[1].Answer != "" {
		t.Errorf("questions = %+v", st.Questions)
	}
	// One bad id: nothing changes.
	if err := s.Decide([]string{"D2", "D1"}, "C", "coordinator"); err == nil {
		t.Error("deciding an answered question: no error")
	}
	if err := s.Decide([]string{"D9"}, "C", "coordinator"); err == nil {
		t.Error("unknown id: no error")
	}
	if err := s.Decide(nil, "x", "owner"); err == nil {
		t.Error("no ids: no error")
	}
	if err := s.Unpin(nil); err == nil {
		t.Error("unpin with no ids: no error")
	}
	if err := s.Decide([]string{"D2"}, "", "owner"); err == nil {
		t.Error("empty answer: no error")
	}
	if err := s.Decide([]string{"D2"}, "x", "someone"); err == nil {
		t.Error("bad by: no error")
	}
	if st, _ := s.Read(); st.Questions[1].Answer != "" {
		t.Errorf("a failed decide changed D2: %+v", st.Questions[1])
	}

	if err := s.Unpin([]string{"T1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Unpin([]string{"T1"}); err == nil {
		t.Error("unpinning a done to-do: no error")
	}
	if st, _ := s.Read(); st.Todos[0].Done.IsZero() {
		t.Error("T1 not done")
	}
}

func TestConcurrentWritersKeepEveryItem(t *testing.T) {
	s := store(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := &Store{Path: s.Path} // a second writer, as another coordinator would be
			if _, err := other.Ask(Question{Question: "q"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	st, err := s.Read()
	if err != nil || len(st.Questions) != 20 || st.NextQ != 21 {
		t.Fatalf("questions %d, next %d, %v", len(st.Questions), st.NextQ, err)
	}
	seen := map[string]bool{}
	for _, q := range st.Questions {
		if seen[q.ID] {
			t.Errorf("id %s used twice", q.ID)
		}
		seen[q.ID] = true
	}
}

func TestReadReportsABadFile(t *testing.T) {
	s := store(t)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(); err == nil {
		t.Error("bad file: no error")
	}
	if _, err := s.Ask(Question{Question: "q"}); err == nil {
		t.Error("ask over a bad file: no error")
	}
}
