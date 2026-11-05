//go:build windows

package sys

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func OwnGroup(cmd *exec.Cmd) {}

// windows has no process groups to signal, so the command alone is ended
func SignalGroup(cmd *exec.Cmd, sig os.Signal) error {
	return KillGroup(cmd)
}

func KillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return errors.New("no process to end")
	}
	return cmd.Process.Kill()
}

func Raise(sig os.Signal) {
	if s, ok := sig.(syscall.Signal); ok {
		os.Exit(128 + int(s))
	}
	os.Exit(1)
}
