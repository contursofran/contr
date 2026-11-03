// Package hctest starts scratch hc cores for tests. A scratch core has its own
// session and XDG folders, so it never touches the owner's core.
package hctest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// Core is a scratch core: its folder and the environment that selects it.
type Core struct {
	// Dir is the scratch folder. A short path: macOS limits socket paths to
	// 103 bytes.
	Dir string
	// Env selects the scratch core. A test may append more variables.
	Env []string
}

// New makes the folder /tmp/<prefix>* and the environment of a scratch core
// with the given session name. When the test ends, it ends the holder process
// of each terminal, stops the core, and removes the folder last.
func New(t *testing.T, prefix, session string) *Core {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatal(err)
	}
	// Cleanups run last in, first out: this one runs after the stop below.
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	c := &Core{Dir: dir, Env: []string{"HC_SESSION=" + session}}
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "XDG_CACHE_HOME"} {
		p := filepath.Join(dir, strings.ToLower(v[4:6]))
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		c.Env = append(c.Env, v+"="+p)
	}
	t.Cleanup(func() {
		if n, err := KillHolders(dir); err != nil {
			t.Logf("end the holders: %v", err)
		} else if n > 0 {
			t.Logf("ended %d holder(s)", n)
		}
		cmd := exec.Command("hc", "server", "stop")
		cmd.Env = append(cmd.Environ(), c.Env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("stop the scratch core: %v: %s", err, out)
		}
	})
	return c
}

// Session returns a session name from a letter prefix and the test process id.
func Session(prefix string) string { return fmt.Sprintf("%s%d", prefix, os.Getpid()) }

// KillHolders ends each `hc hold` process whose socket is under dir, and
// returns how many it ended. A holder with a socket elsewhere is never touched.
func KillHolders(dir string) (int, error) {
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return 0, err
	}
	roots := []string{dir + "/"}
	if r, err := filepath.EvalSymlinks(dir); err == nil && r != dir {
		roots = append(roots, r+"/")
	}
	n := 0
	for _, pid := range holderPIDs(string(out), roots) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
			n++
		}
	}
	return n, nil
}

// holderPIDs picks the pids of `hc hold --socket <path>` lines in ps output
// whose path starts with one of roots.
func holderPIDs(ps string, roots []string) []int {
	var pids []int
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		// The program is the first word, or the second after a shell (a script stands in for hc in the test).
		i := 1
		if len(f) > 1 && filepath.Base(f[1]) == "sh" {
			i = 2
		}
		if len(f) < i+4 || filepath.Base(f[i]) != "hc" || f[i+1] != "hold" {
			continue
		}
		sock := ""
		for j := i + 2; j+1 < len(f); j++ {
			if f[j] == "--socket" {
				sock = f[j+1]
			}
		}
		for _, r := range roots {
			if strings.HasPrefix(sock, r) {
				if pid, err := strconv.Atoi(f[0]); err == nil {
					pids = append(pids, pid)
				}
				break
			}
		}
	}
	return pids
}
