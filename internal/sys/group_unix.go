//go:build unix

package sys

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func OwnGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// the command leads its group, so the group's id is its pid
func SignalGroup(cmd *exec.Cmd, sig os.Signal) error {
	s, ok := sig.(syscall.Signal)
	if !ok || cmd.Process == nil {
		return errors.New("no signal for a process group")
	}
	err := syscall.Kill(-cmd.Process.Pid, s)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func KillGroup(cmd *exec.Cmd) error {
	return SignalGroup(cmd, syscall.SIGKILL)
}

// ends this process by the signal, as its default action does. the signal reaches the process
// a moment after kill returns, so the exit after it is only for a signal that did not end it
func Raise(sig os.Signal) {
	s, ok := sig.(syscall.Signal)
	if !ok {
		os.Exit(1)
	}
	signal.Reset(s)
	syscall.Kill(os.Getpid(), s)
	time.Sleep(time.Second)
	os.Exit(128 + int(s))
}
