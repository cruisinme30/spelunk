//go:build windows

package cli

import (
	"os/exec"
	"syscall"
)

// Process creation flags (CreateProcess): a new process group with no
// console, so the reader outlives the terminal or agent that started it.
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detach starts cmd's process on its own.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
