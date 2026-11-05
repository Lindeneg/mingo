//go:build linux

package team

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// the helper of kind run on a terminal of its own, as mingo is at a shell, with the terminal's
// end the test types into
func onTerminal(t *testing.T, kind, file string) (*exec.Cmd, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skip("no pseudo terminal here:", err)
	}
	t.Cleanup(func() { master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close() })
	go io.Copy(io.Discard, master)
	helper := exec.Command(self(t))
	helper.Env = append(os.Environ(), "TEAM_HELPER="+kind, "TEAM_FILE="+file)
	helper.Stdin, helper.Stdout, helper.Stderr = slave, slave, slave
	helper.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { helper.Process.Kill() })
	return helper, master
}

func waitWithin(t *testing.T, cmd *exec.Cmd, d time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(d):
		t.Fatal("the helper did not end within", d)
		return nil
	}
}

func endedBy(err error) syscall.Signal {
	if exit, ok := err.(*exec.ExitError); ok {
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return ws.Signal()
		}
	}
	return 0
}

func TestACommandRunAtATerminalCanReadTheTerminal(t *testing.T) {
	file := filepath.Join(t.TempDir(), "typed")
	helper, master := onTerminal(t, "tty", file)
	if _, err := master.Write([]byte("yes\n")); err != nil {
		t.Fatal(err)
	}
	err := waitWithin(t, helper, 10*time.Second)
	if data, _ := os.ReadFile(file); err != nil || string(data) != "yes\n" {
		t.Fatalf("%v %q", err, data)
	}
}

func TestCtrlCAtATerminalEndsTheCommandAndThenThisProcess(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pid")
	helper, master := onTerminal(t, "tty-sleep", file)
	pid := readPid(t, file)
	killLater(t, pid)
	if _, err := master.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	if err := waitWithin(t, helper, 10*time.Second); endedBy(err) != syscall.SIGINT {
		t.Fatal("the process did not end by ctrl c", err)
	}
	if !endsWithin(pid, 5*time.Second) {
		t.Fatal("the command outlived ctrl c")
	}
}

func TestSigtermAtATerminalReachesTheCommandAndThenEndsThisProcess(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pid")
	helper, _ := onTerminal(t, "tty-sleep", file)
	pid := readPid(t, file)
	killLater(t, pid)
	helper.Process.Signal(syscall.SIGTERM)
	if err := waitWithin(t, helper, 10*time.Second); endedBy(err) != syscall.SIGTERM {
		t.Fatal("the process did not end by the signal", err)
	}
	if !endsWithin(pid, 5*time.Second) {
		t.Fatal("the command outlived the signal")
	}
}
