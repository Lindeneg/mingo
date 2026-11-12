//go:build unix

package sys

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func PidRuns(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true
	}
	_, err = os.Stat("/proc/" + strconv.Itoa(pid))
	return err == nil
}

func Signal(pid int, sig syscall.Signal) error {
	if pid <= 0 {
		return syscall.ESRCH
	}
	return syscall.Kill(pid, sig)
}

func Exec(path string, argv, env []string) error {
	return syscall.Exec(path, argv, env)
}

type Lock struct {
	f *os.File
}

func LockFile(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		if f, err = os.Open(path); err != nil {
			return nil, err
		}
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

func (l *Lock) Unlock() {
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	l.f.Close()
}

func Writable(dir string) bool {
	return syscall.Access(dir, 2) == nil
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return exit.ExitCode()
	}
	if errors.Is(err, os.ErrPermission) {
		return 126
	}
	return 127
}
