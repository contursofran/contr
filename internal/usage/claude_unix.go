//go:build unix

package usage

import (
	"os/exec"
	"syscall"
)

// isolate makes the command the leader of its own process group, so killGroup
// reaches the processes it forks.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup sends SIGKILL to every process in the command's group. A failure is
// ignored: the caller still kills the command itself.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
