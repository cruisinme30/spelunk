//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

// detach makes cmd's process its own session, so it outlives the terminal
// or agent that started it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
