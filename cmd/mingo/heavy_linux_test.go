//go:build linux

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestHeavyOnLinuxEndsItsCommandWhenItIsKilled(t *testing.T) {
	h := newHome(t)
	h.heavyDir()
	pidFile := filepath.Join(h.root, "pid")
	cmd := exec.Command(h.bin, "heavy", "check", "--", "bash", "-c", `echo $$ >"$1"; exec sleep 600`, "_", pidFile)
	cmd.Env = h.env()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h.mustWait(10, "the command's pid", func() bool { return strings.TrimSpace(h.read(pidFile)) != "" })
	var child int
	fmt.Sscan(h.read(pidFile), &child)
	t.Cleanup(func() { syscall.Kill(child, syscall.SIGKILL) })
	cmd.Process.Kill()
	cmd.Wait()
	if !waitFor(5, func() bool { return !isAlive(child) }) {
		t.Fatal("the command runs on after mingo heavy was killed")
	}
}
