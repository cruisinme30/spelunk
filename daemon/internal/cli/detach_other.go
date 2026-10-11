//go:build !unix && !windows

package cli

import "os/exec"

// detach does nothing where processes can't be detached.
func detach(*exec.Cmd) {}
