//go:build !unix

package usage

import "os/exec"

func isolate(*exec.Cmd) {}

func killGroup(*exec.Cmd) {}
