//go:build unix

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func (h *home) heavyDir() string {
	dir := filepath.Join(h.state, "heavy")
	h.mkdir(dir)
	return dir
}

func noSlots(dir string) bool {
	matches, _ := filepath.Glob(filepath.Join(dir, "slot-*"))
	return len(matches) == 0
}

func (h *home) heavyHolder(n int, label string) {
	h.t.Helper()
	cmd := exec.Command("sleep", "600")
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	h.write(filepath.Join(h.heavyDir(), fmt.Sprintf("slot-%d", n)), fmt.Sprintf("%d %d %s\n", cmd.Process.Pid, time.Now().Unix()-120, label))
}

func TestHeavyRunsTheCommandUnderASlotAndReturnsItsExitCode(t *testing.T) {
	h := newHome(t)
	dir := h.heavyDir()
	r := h.mingo("heavy", "check", "--", "bash", "-c", `ls "$1" | grep -c slot; exit 3`, "_", dir)
	if r.code != 3 || r.out != "1\n" || !noSlots(dir) {
		t.Fatal(r.code, r.out)
	}
}

func TestHeavyTellsTheCommandItHoldsASlotSoOneItStartsRunsAtOnce(t *testing.T) {
	h := newHome(t)
	dir := h.heavyDir()
	r := h.mingo("heavy", "outer", "--", h.bin, "heavy", "inner", "--", "bash", "-c", `echo "$MINGO_HEAVY_HELD"; ls "$1" | grep -c slot`, "_", dir)
	if r.out != "1\n1\n" {
		t.Fatal(r.out)
	}
}

func TestHeavyRunsAtOnceWithoutASlotWhenOff(t *testing.T) {
	h := newHome(t)
	dir := h.heavyDir()
	r := h.runIn("", []string{"MINGO_HEAVY=off"}, h.bin, "heavy", "check", "--", "bash", "-c", `ls "$1" | grep -c slot || true`, "_", dir)
	if r.out != "0\n" {
		t.Fatal(r.out)
	}
}

func TestHeavySaysWhoHoldsTheSlotsAndTakesTheOneThatFrees(t *testing.T) {
	h := newHome(t)
	h.heavyHolder(1, "build")
	h.heavyHolder(2, "test")
	dir := h.heavyDir()
	go func() {
		time.Sleep(2 * time.Second)
		os.Remove(filepath.Join(dir, "slot-1"))
	}()
	r := h.runIn("", []string{"MINGO_HEAVY_MIN_FREE_MB=0", "MINGO_HEAVY_POLL_S=1"}, h.bin, "heavy", "check", "--", "true")
	if r.code != 0 || !strings.Contains(r.out, "check waits for a slot, held by build (pid") || !strings.Contains(r.out, "test (pid") ||
		!strings.Contains(r.out, "check took slot 1") {
		t.Fatal(r.out)
	}
}

func TestHeavyReleasesTheSlotWhenTheCommandCannotStart(t *testing.T) {
	h := newHome(t)
	dir := h.heavyDir()
	if r := h.mingo("heavy", "check", "--", filepath.Join(h.root, "no-such-command")); r.code != 127 || !noSlots(dir) {
		t.Fatal(r.code, r.out)
	}
}

func TestHeavyRunsWithoutASlotWhenTheFolderCannotBeUsed(t *testing.T) {
	h := newHome(t)
	h.touch(filepath.Join(h.root, "not-a-folder"))
	r := h.runIn("", []string{"MINGO_STATE=" + filepath.Join(h.root, "not-a-folder")}, h.bin, "heavy", "check", "--", "echo", "ran")
	if r.code != 0 || !strings.Contains(r.out, "cannot be used, running check without a slot") || !strings.Contains(r.out, "ran") {
		t.Fatal(r.out)
	}
}

func TestHeavyRefusesACallWithoutALabelAndACommand(t *testing.T) {
	h := newHome(t)
	for _, args := range [][]string{{"heavy", "check"}, {"heavy", "--", "true"}} {
		if r := h.mingo(args...); r.code == 0 {
			t.Fatal(args, r.out)
		}
	}
}

func TestHeavyPassesASigtermOnAtOnceAndFreesTheSlot(t *testing.T) {
	h := newHome(t)
	dir := h.heavyDir()
	pidFile := filepath.Join(h.root, "pid")
	cmd := exec.Command(h.bin, "heavy", "check", "--", "bash", "-c", `echo $$ >"$1"; exec sleep 600`, "_", pidFile)
	cmd.Env = h.env()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h.mustWait(10, "the command's pid", func() bool { return strings.TrimSpace(h.read(pidFile)) != "" })
	var child int
	fmt.Sscan(h.read(pidFile), &child)
	if noSlots(dir) {
		t.Fatal("no slot held")
	}
	sent := time.Now()
	cmd.Process.Signal(syscall.SIGTERM)
	err := cmd.Wait()
	if time.Since(sent) > 3*time.Second {
		t.Fatal("mingo heavy took", time.Since(sent), "to end")
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 143 {
		t.Fatal(err)
	}
	if isAlive(child) || !noSlots(dir) {
		t.Fatal("the command runs on or the slot is kept")
	}
}

func TestHeavyRefusesASettingOutOfRangeAndRunsNothing(t *testing.T) {
	h := newHome(t)
	marker := filepath.Join(h.root, "ran")
	r := h.runIn("", []string{"MINGO_HEAVY_SLOTS=0"}, h.bin, "heavy", "check", "--", "touch", marker)
	if r.code != 2 || r.out != "mingo heavy: MINGO_HEAVY_SLOTS is 0, it takes a whole number of 1 or more\n" || exists(marker) {
		t.Fatal(r.code, r.out)
	}
}
