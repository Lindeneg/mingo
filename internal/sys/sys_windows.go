//go:build windows

package sys

import (
	"errors"
	"os/exec"
	"syscall"
)

func PidRuns(pid int) bool {
	return false
}

func Signal(pid int, sig syscall.Signal) error {
	return NotBuilt("signalling a process")
}

func Exec(path string, argv, env []string) error {
	return NotBuilt("exec")
}

type Lock struct{}

func LockFile(path string) (*Lock, error) {
	return nil, NotBuilt("the slots' lock")
}

func (l *Lock) Unlock() {}

func Writable(dir string) bool {
	return true
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 127
}
