//go:build linux

package sys

import (
	"os/exec"
	"syscall"
)

// the kernel sends the command sigterm when the thread that started it ends
func EndWithParent(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}
