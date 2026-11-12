//go:build darwin

package sys

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

func FreeMB() (int, error) {
	free, err := unix.SysctlUint32("vm.page_free_count")
	if err != nil {
		return 0, err
	}
	external, err := unix.SysctlUint32("vm.page_pageable_external_count")
	if err != nil {
		return 0, err
	}
	return int((uint64(free) + uint64(external)) * uint64(os.Getpagesize()) >> 20), nil
}

func ProcStart(pid int) (string, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	t := kp.Proc.P_starttime
	if t.Sec == 0 && t.Usec == 0 {
		return "", errors.New("no start time for " + strconv.Itoa(pid))
	}
	return fmt.Sprintf("%d.%06d", t.Sec, t.Usec), nil
}

func IsTerminal(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), unix.TIOCGETA)
	return err == nil
}

func launchctl() string {
	if v := os.Getenv("MINGO_LAUNCHCTL"); v != "" {
		return v
	}
	return "launchctl"
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func service(project string) string {
	return "gui/" + strconv.Itoa(os.Getuid()) + "/" + Label(project)
}

func ServiceEnable(home, project string, tmpl []byte) error {
	plist, err := RenderPlist(tmpl, home, project)
	if err != nil {
		return err
	}
	file := PlistPath(home, project)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(file, plist, 0o644); err != nil {
		return err
	}
	if err := os.Chmod(file, 0o644); err != nil {
		return err
	}
	return run(launchctl(), "enable", service(project))
}

func ServiceDisable(home, project string) error {
	if err := os.Remove(PlistPath(home, project)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return run(launchctl(), "disable", service(project))
}

func SetEcho(fd uintptr, on bool) error {
	t, err := unix.IoctlGetTermios(int(fd), unix.TIOCGETA)
	if err != nil {
		return err
	}
	if on {
		t.Lflag |= unix.ECHO
	} else {
		t.Lflag &^= unix.ECHO
	}
	return unix.IoctlSetTermios(int(fd), unix.TIOCSETA, t)
}
