package github

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const checksFixture = `[
{"name":"build","workflow":"CI","link":"https://x.test/1","bucket":"pass","state":"SUCCESS","startedAt":"2026-11-04T10:00:00Z","completedAt":"2026-11-04T10:02:05Z"},
{"name":"lint","workflow":"CI","link":"https://x.test/2","bucket":"fail","state":"FAILURE","startedAt":"2026-11-04T10:00:00Z","completedAt":"2026-11-04T10:00:30Z"},
{"name":"e2e","workflow":"CI","link":"https://x.test/3","bucket":"pending","state":"IN_PROGRESS","startedAt":"2026-11-04T10:00:00Z","completedAt":"0001-01-01T00:00:00Z"},
{"name":"docs","workflow":"Docs","link":"","bucket":"skipping","state":"SKIPPED","startedAt":"0001-01-01T00:00:00Z","completedAt":"0001-01-01T00:00:00Z"},
{"name":"old","workflow":"CI","link":"","bucket":"cancel","state":"CANCELLED","startedAt":"0001-01-01T00:00:00Z","completedAt":"0001-01-01T00:00:00Z"}
]`

func TestChecksParsesFixture(t *testing.T) {
	f := &fakeRunner{out: []byte(checksFixture)}
	got, err := Client{R: f, Dir: "/w"}.Checks(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.args[:3], " ") != "pr checks 7" || f.args[3] != "--json" {
		t.Errorf("args %v", f.args)
	}
	var res []string
	for _, c := range got {
		res = append(res, c.Name+":"+c.Result)
	}
	if s := strings.Join(res, ","); s != "build:pass,lint:fail,e2e:pending,docs:skip,old:cancel" {
		t.Errorf("rows %s", s)
	}
	if got[0].Workflow != "CI" || got[0].Link != "https://x.test/1" {
		t.Errorf("row 0 %+v", got[0])
	}
	if d := got[0].Duration(); d != 2*time.Minute+5*time.Second {
		t.Errorf("duration %v", d)
	}
	if got[2].Duration() != 0 || got[3].Duration() != 0 {
		t.Errorf("unfinished checks have no duration")
	}
}

func TestChecksPendingExitIsNotError(t *testing.T) {
	f := &fakeRunner{out: []byte(checksFixture), err: errors.New("exit status 8")}
	got, err := Client{R: f}.Checks(context.Background(), 1)
	if err != nil || len(got) != 5 {
		t.Fatalf("rows %d err %v", len(got), err)
	}
}

func TestChecksNoChecks(t *testing.T) {
	f := &fakeRunner{err: errors.New("no checks reported on the 'x' branch")}
	got, err := Client{R: f}.Checks(context.Background(), 1)
	if err != nil || len(got) != 0 {
		t.Fatalf("rows %v err %v", got, err)
	}
}

func TestChecksFailsWithoutRows(t *testing.T) {
	f := &fakeRunner{err: errors.New("could not find pull request")}
	_, err := Client{R: f}.Checks(context.Background(), 1)
	if err == nil || !strings.HasPrefix(err.Error(), "gh: ") {
		t.Errorf("err %v", err)
	}
}

func TestChecksBadJSON(t *testing.T) {
	f := &fakeRunner{out: []byte("not json")}
	_, err := Client{R: f}.Checks(context.Background(), 1)
	if err == nil || !strings.HasPrefix(err.Error(), "gh: ") {
		t.Errorf("err %v", err)
	}
}

func TestChecksTimeout(t *testing.T) {
	f := &fakeRunner{fn: func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return []byte(checksFixture), ctx.Err()
	}}
	_, err := Client{R: f, Timeout: 10 * time.Millisecond}.Checks(context.Background(), 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err %v", err)
	}
}
