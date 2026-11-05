//go:build unix

package team

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func readPid(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, _ := os.ReadFile(file)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatal("no pid in", file)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func endsWithin(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}

func killLater(t *testing.T, pid int) {
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
}

func TestATimeoutEndsEveryProcessTheCommandStarted(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pid")
	r, err := RunIntoLog(Options{Dir: dir, Timeout: time.Second}, filepath.Join(dir, "log"), "sh", "-c", `sleep 600 & echo $! >"$1"; wait`, "_", file)
	if err != nil || r != (Result{TimedOut: true}) {
		t.Fatal(r, err)
	}
	pid := readPid(t, file)
	killLater(t, pid)
	if !endsWithin(pid, 5*time.Second) {
		t.Fatal("the process the command started outlived the timeout")
	}
}

func TestACommandThatExitsZeroWithABackgroundProcessOnItsOutputSucceeds(t *testing.T) {
	r, err := Capture(Options{Dir: t.TempDir(), Timeout: 20 * time.Second}, "sh", "-c", `sleep 600 & echo $!; exit 0`)
	pid, _ := strconv.Atoi(strings.TrimSpace(r.Stdout))
	if pid > 0 {
		killLater(t, pid)
	}
	if err != nil || !r.OK() || pid == 0 {
		t.Fatal(r, err)
	}
}

func TestASignalEndsTheCommandWithEveryProcessItStartedAndThenThisProcess(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pid")
	helper := exec.Command(self(t))
	helper.Env = append(os.Environ(), "TEAM_HELPER=group", "TEAM_FILE="+file)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { helper.Process.Kill() })
	pid := readPid(t, file)
	killLater(t, pid)
	helper.Process.Signal(syscall.SIGTERM)
	err := helper.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatal(err)
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatal("the process did not end by the signal", err)
	}
	if !endsWithin(pid, 5*time.Second) {
		t.Fatal("the process the command started outlived the signal")
	}
}
