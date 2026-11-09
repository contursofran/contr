package main

import (
	"testing"
	"time"

	"github.com/contursofran/contr/internal/updated"
)

func TestUpdatedStoreWithNoStateFolder(t *testing.T) {
	s := updatedStore("")
	if n, err := s.Load(); err != nil || n != (updated.Note{}) {
		t.Fatalf("Load = %+v, %v; want a zero note and no error", n, err)
	}
	if err := s.Save(updated.Note{Version: "1.2.0"}); err == nil || err.Error() != "no state folder" {
		t.Fatalf("Save = %v; want no state folder", err)
	}
}

func TestUpdatedStoreKeepsTheNote(t *testing.T) {
	s := updatedStore(updated.Path(t.TempDir()))
	want := updated.Note{Version: "1.2.0", From: "1.1.0", InstalledAt: time.Unix(1700000000, 0).UTC()}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load(); err != nil || !got.InstalledAt.Equal(want.InstalledAt) || got.Version != want.Version || got.From != want.From {
		t.Fatalf("Load = %+v, %v; want %+v", got, err, want)
	}
}
