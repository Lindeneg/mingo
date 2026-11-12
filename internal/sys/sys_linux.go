//go:build linux

package sys

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

func FreeMB() (int, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kb, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, err
			}
			return kb / 1024, nil
		}
	}
	return 0, errors.New("no MemAvailable in /proc/meminfo")
}

// the command name in the stat line may hold spaces and parentheses, so the fields are counted from its closing one
func ProcStart(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	line := string(data)
	end := strings.LastIndexByte(line, ')')
	if end < 0 {
		return "", errors.New("unreadable /proc stat line")
	}
	fields := strings.Fields(line[end+1:])
	if len(fields) < 20 {
		return "", errors.New("short /proc stat line")
	}
	return fields[19], nil
}

func IsTerminal(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

func systemctl() string {
	if v := os.Getenv("MINGO_SYSTEMCTL"); v != "" {
		return v
	}
	return "systemctl"
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func unitDir(home string) string {
	return filepath.Join(home, ".config", "systemd", "user")
}

func ServiceEnable(home, project string, unit []byte) error {
	dir := unitDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file := filepath.Join(dir, "mingo@.service")
	if err := os.WriteFile(file, unit, 0o644); err != nil {
		return err
	}
	if err := os.Chmod(file, 0o644); err != nil {
		return err
	}
	if err := run(systemctl(), "--user", "daemon-reload"); err != nil {
		return err
	}
	return run(systemctl(), "--user", "enable", "mingo@"+project+".service")
}

func ServiceDisable(home, project string) error {
	return run(systemctl(), "--user", "disable", "mingo@"+project+".service")
}

func SetEcho(fd uintptr, on bool) error {
	var t syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&t))); errno != 0 {
		return errno
	}
	if on {
		t.Lflag |= syscall.ECHO
	} else {
		t.Lflag &^= syscall.ECHO
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&t))); errno != 0 {
		return errno
	}
	return nil
}
