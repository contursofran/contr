package setup

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/usage"
)

// fake is a set of programs: the output or error of each command line.
type fake struct {
	missing map[string]bool
	out     map[string]string
	err     map[string]error
	hang    map[string]bool
}

func (f fake) env(t *testing.T) Env {
	return Env{
		StateDir: t.TempDir(),
		Timeout:  50 * time.Millisecond,
		Find: func(name string) (string, error) {
			if f.missing[name] {
				return "", errors.New("not found")
			}
			return name, nil
		},
		Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			key := name
			for _, a := range args {
				key += " " + a
			}
			if f.hang[key] {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []byte(f.out[key]), f.err[key]
		},
	}
}

// good is a Mac with everything set up, but no meter data.
func good() fake {
	return fake{missing: map[string]bool{}, err: map[string]error{}, hang: map[string]bool{},
		out: map[string]string{
			"hc --version":              "hc 0.1.18\n",
			"claude plugin list --json": `[{"id":"other@x","version":"1.0.0","enabled":true},{"id":"contr@contr","version":"0.1.0","enabled":true}]`,
		}}
}

func byID(rs []Result, id string) Result {
	for _, r := range rs {
		if r.ID == id {
			return r
		}
	}
	return Result{}
}

func check(t *testing.T, f fake, id string, state State, detail, copy string) {
	t.Helper()
	r := byID(Run(context.Background(), f.env(t)), id)
	if r.State != state || r.Detail != detail || r.Copy != copy {
		t.Fatalf("%s = %+v; want state %d, detail %q, copy %q", id, r, state, detail, copy)
	}
}

func TestHC(t *testing.T) {
	check(t, good(), IDHC, Ready, "hc 0.1.18", "")
	f := good()
	f.missing["hc"] = true
	check(t, f, IDHC, Missing, "not installed", CopyHC)
	f = good()
	f.out["hc --version"] = "herdr something odd"
	check(t, f, IDHC, Action, "unexpected version output", "")
	f.out["hc --version"] = "hc 0.1.18 extra"
	check(t, f, IDHC, Action, "unexpected version output", "")
	f = good()
	f.hang["hc --version"] = true
	check(t, f, IDHC, Action, "timed out", "")
	f = good()
	f.err["hc --version"] = errors.New("exit status 1")
	check(t, f, IDHC, Action, "failed: exit status 1", "")
}

func TestPlugin(t *testing.T) {
	check(t, good(), IDPlugin, Ready, "contr 0.1.0", "")
	f := good()
	f.out["claude plugin list --json"] = `[{"id":"contr@contr","version":"0.1.0","enabled":false}]`
	check(t, f, IDPlugin, Action, "disabled", CopyEnable)
	f.out["claude plugin list --json"] = `[{"id":"contrib@x","version":"1","enabled":true}]`
	check(t, f, IDPlugin, Missing, "not installed", CopyPlugin)
	f.out["claude plugin list --json"] = "Installed plugins:\n"
	check(t, f, IDPlugin, Action, "unexpected plugin list output", "")
	f = good()
	f.hang["claude plugin list --json"] = true
	check(t, f, IDPlugin, Action, "timed out", "")
	f = good()
	f.missing["claude"] = true
	check(t, f, IDPlugin, Missing, "Claude Code not installed", "")
}

func TestMeter(t *testing.T) {
	f := good()
	check(t, f, IDMeter, Action, "no data from the status line", CopyMeter)
	now := time.Unix(1_800_000_000, 0)
	for _, c := range []struct {
		age    time.Duration
		state  State
		detail string
	}{
		{5 * time.Minute, Ready, "last data 5 min ago"},
		{23 * time.Hour, Ready, "last data 23 h ago"},
		{72 * time.Hour, Action, "last data 3 days ago"},
	} {
		e := f.env(t)
		e.Now = func() time.Time { return now }
		snap := &usage.ClaudeSnapshot{Schema: 1, UpdatedAt: uint64(now.Add(-c.age).Unix()), Windows: []usage.Window{{}}}
		if _, err := usage.WriteSnapshot(e.StateDir, snap); err != nil {
			t.Fatal(err)
		}
		r := byID(Run(context.Background(), e), IDMeter)
		if r.State != c.state || r.Detail != c.detail {
			t.Fatalf("age %v: %+v", c.age, r)
		}
		if (r.Copy == "") != (c.state == Ready) {
			t.Fatalf("age %v: copy %q", c.age, r.Copy)
		}
	}
}

func TestGH(t *testing.T) {
	check(t, good(), IDGH, Ready, "signed in", "")
	f := good()
	f.err["gh auth status"] = errors.New("exit status 1")
	check(t, f, IDGH, Action, "not signed in", CopyGHLogin)
	f = good()
	f.hang["gh auth status"] = true
	check(t, f, IDGH, Action, "timed out", CopyGHLogin)
	f = good()
	f.missing["gh"] = true
	check(t, f, IDGH, Missing, "not installed", CopyGHApp)
}

func TestOptionalChecks(t *testing.T) {
	check(t, good(), IDCodex, Ready, "installed", "")
	check(t, good(), IDContr, Ready, "linked", "")
	f := good()
	f.missing["codex"], f.missing["contr"] = true, true
	check(t, f, IDCodex, Optional, "not installed", CopyCodex)
	check(t, f, IDContr, Optional, "not linked: the meter needs it", CopyContrCLI)
	rs := Run(context.Background(), f.env(t))
	if byID(rs, IDCodex).Required || !byID(rs, IDCodex).OK() {
		t.Fatal("codex must be optional and count as OK")
	}
}

func TestCoreAndRequired(t *testing.T) {
	if r := Core("0.1.18", true); r.State != Ready || r.Detail != "running · hc 0.1.18" {
		t.Fatalf("%+v", r)
	}
	if r := Core("", false); r.State != Missing || r.Detail != "not connected" {
		t.Fatalf("%+v", r)
	}
	rs := []Result{Core("", false), {Required: true}, {Required: false, State: Optional}, {Required: true, State: Action}}
	if n := Required(rs); n != 2 {
		t.Fatalf("Required = %d, want 2", n)
	}
}

func TestRunPluginRunsOnlyThePluginLines(t *testing.T) {
	var got []string
	e := Env{Find: func(string) (string, error) { return "/bin/claude", nil },
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			got = append([]string{name}, args...)
			return nil, nil
		}}
	if err := RunPlugin(context.Background(), e, CopyEnable); err != nil || strings.Join(got, " ") != "/bin/claude plugin enable contr@contr" {
		t.Fatalf("err %v, ran %q", err, got)
	}
	got = nil
	if err := RunPlugin(context.Background(), e, "rm -rf x"); err == nil || got != nil {
		t.Fatalf("ran another line: %v %q", err, got)
	}
	e.Find = func(string) (string, error) { return "", errors.New("claude not found") }
	if err := RunPlugin(context.Background(), e, CopyPlugin); err == nil || got != nil {
		t.Fatalf("ran without claude: %v", err)
	}
}

func TestRunPluginErrorIsTheLastStderrLine(t *testing.T) {
	e := Env{Find: func(string) (string, error) { return "/bin/sh", nil },
		Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
			return exec.CommandContext(ctx, "/bin/sh", "-c", "echo one >&2; echo 'two last' >&2; exit 1").Output()
		}}
	if err := RunPlugin(context.Background(), e, CopyPlugin); err == nil || err.Error() != "two last" {
		t.Fatalf("err %v", err)
	}
}
