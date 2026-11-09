package statedir

import "testing"

func envOf(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestStateDirPrefersTheOverride(t *testing.T) {
	env := envOf(map[string]string{"CONTR_STATE_DIR": "/x/state", "XDG_STATE_HOME": "/x/xdg"})
	if got := From(env, "/h", true); got != "/x/state" {
		t.Fatalf("macos: %q", got)
	}
	env = envOf(map[string]string{"CONTR_STATE_DIR": "/x/state"})
	if got := From(env, "/h", false); got != "/x/state" {
		t.Fatalf("other: %q", got)
	}
}

func TestStateDirIsAppSupportOnMacos(t *testing.T) {
	if got := From(envOf(nil), "/h", true); got != "/h/Library/Application Support/contr" {
		t.Fatalf("got %q", got)
	}
}

func TestStateDirFollowsXdgElsewhere(t *testing.T) {
	if got := From(envOf(map[string]string{"XDG_STATE_HOME": "/x"}), "/h", false); got != "/x/contr" {
		t.Fatalf("xdg: %q", got)
	}
	if got := From(envOf(nil), "/h", false); got != "/h/.local/state/contr" {
		t.Fatalf("default: %q", got)
	}
}

func TestEmptyVariablesCountAsUnset(t *testing.T) {
	env := envOf(map[string]string{"CONTR_STATE_DIR": "", "XDG_STATE_HOME": ""})
	if got := From(env, "/h", false); got != "/h/.local/state/contr" {
		t.Fatalf("got %q", got)
	}
}

func TestNoHomeAndNoOverrideIsNone(t *testing.T) {
	for _, macos := range []bool{true, false} {
		if got := From(envOf(nil), "", macos); got != "" {
			t.Fatalf("macos=%v: %q", macos, got)
		}
	}
}
