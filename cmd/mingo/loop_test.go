//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/cli"
	"github.com/Lindeneg/mingo/internal/loop"
)

var (
	endedZero  = regexp.MustCompile(`session ended with code 0 after [0-9]+ s`)
	endedTerm  = regexp.MustCompile(`session ended with code 143 after [0-9]+ s`)
	endedKill  = regexp.MustCompile(`session ended with code 137 after [0-9]+ s`)
	endedNoCLI = regexp.MustCompile(`session ended with code 127 after [0-9]+ s`)
)

const graces = 1 + 3 + 1 + 2

func TestLoopTypesExitAndSendsNoSignalToASessionThatEndsOnIt(t *testing.T) {
	h := newHome(t)
	h.startLoop("obeys")
	first := h.nthStart(1)
	h.setMode("held")
	h.touch(h.marker("restart"))
	h.mustWait(20, "the second start", h.startsAre(2))
	if isAlive(first) {
		t.Fatal("first session still runs")
	}
	if s := h.screen(); !endedZero.MatchString(s) || strings.Contains(s, "the loop ended it") {
		t.Fatal(s)
	}
	if exists(h.marker("restart")) {
		t.Fatal("restart marker kept")
	}
}

func TestLoopEndsAHeldSessionByItsPidAfterTheGrace(t *testing.T) {
	h := newHome(t)
	h.startLoop("held")
	first := h.nthStart(1)
	h.touch(h.marker("restart"))
	h.mustWait(30, "the second start", h.startsAre(2))
	if isAlive(first) {
		t.Fatal("first session still runs")
	}
	s := h.screen()
	if !endedTerm.MatchString(s) || !strings.Contains(s, "/exit had not ended it after 3 s, a question nobody answered held it, so the loop ended it with SIGTERM") {
		t.Fatal(s)
	}
}

func TestLoopEndsWithSigkillASessionThatIgnoresSigterm(t *testing.T) {
	h := newHome(t)
	h.startLoop("deaf")
	first := h.nthStart(1)
	h.touch(h.marker("restart"))
	h.mustWait(30, "the second start", h.startsAre(2))
	if isAlive(first) {
		t.Fatal("first session still runs")
	}
	if s := h.screen(); !endedKill.MatchString(s) || !strings.Contains(s, "the loop ended it with SIGKILL") {
		t.Fatal(s)
	}
}

func TestLoopLeavesASessionItsGraceAndTheOneAfterAlone(t *testing.T) {
	h := newHome(t)
	h.startLoop("slow")
	h.setMode("held")
	h.touch(h.marker("restart"))
	h.mustWait(20, "the second start", h.startsAre(2))
	if s := h.screen(); !endedZero.MatchString(s) || strings.Contains(s, "the loop ended it") {
		t.Fatal(s)
	}
	sleepSeconds(graces)
	if h.starts() != 2 || !isAlive(h.nthStart(2)) {
		t.Fatal("the session after was touched")
	}
}

func TestLoopTypesNothingForAMarkerBetweenTwoSessions(t *testing.T) {
	h := newHome(t)
	h.setEnv("MINGO_LOOP_DELAY=12")
	h.startLoop("obeys")
	h.setMode("listens")
	h.touch(h.marker("restart"))
	h.mustWait(20, "the running marker to go", func() bool { return !exists(h.marker("running")) })
	h.touch(h.marker("restart"))
	h.mustWait(10, "the restart marker to go", func() bool { return !exists(h.marker("restart")) })
	if h.starts() != 1 {
		t.Fatal(h.starts())
	}
	h.mustWait(30, "the second start", h.startsAre(2))
	sleepSeconds(2)
	if h.typed() != "" || !isAlive(h.nthStart(2)) {
		t.Fatalf("typed %q", h.typed())
	}
}

func TestLoopEndsItsOwnSessionWhenTheMarkerIsGone(t *testing.T) {
	h := newHome(t)
	h.startLoop("listens")
	first := h.nthStart(1)
	h.mustWait(10, "the pid marker", func() bool { return exists(h.marker("pid")) })
	os.Remove(h.marker("pid"))
	h.setMode("held")
	h.touch(h.marker("restart"))
	h.mustWait(30, "the second start", h.startsAre(2))
	if isAlive(first) {
		t.Fatal("first session still runs")
	}
	if h.typed() != "/exit" {
		t.Fatalf("typed %q", h.typed())
	}
}

func TestLoopNeverSignalsTheProcessAMarkerNames(t *testing.T) {
	h := newHome(t)
	h.startLoop("listens")
	first := h.nthStart(1)
	other := exec.Command("sleep", "600")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		other.Process.Kill()
		other.Wait()
	}()
	h.write(h.marker("pid"), strconv.Itoa(other.Process.Pid)+"\n")
	h.setMode("held")
	h.touch(h.marker("restart"))
	h.mustWait(30, "the second start", h.startsAre(2))
	if isAlive(first) {
		t.Fatal("own session still runs")
	}
	if !isAlive(other.Process.Pid) {
		t.Fatal("the process the marker named was signalled")
	}
}

func TestLoopEndsItsOwnSessionWhenTheMarkerHoldsZero(t *testing.T) {
	h := newHome(t)
	h.startLoop("listens")
	first := h.nthStart(1)
	h.write(h.marker("pid"), "0\n")
	h.setMode("held")
	h.touch(h.marker("restart"))
	h.mustWait(30, "the second start", h.startsAre(2))
	if isAlive(first) {
		t.Fatal("own session still runs")
	}
}

func TestLoopSaysAFailedStartLeavesNoMarkerAndTriesAgain(t *testing.T) {
	h := newHome(t)
	claude := filepath.Join(h.root, "no-such-claude")
	h.setEnv("MINGO_LOOP_DELAY=2", "MINGO_CLAUDE="+claude)
	h.addDemo()
	// mingo start refuses a claude that is not there, so it goes away only once the loop runs
	h.write(claude, "#!/bin/sh\n")
	os.Chmod(claude, 0o755)
	if r := h.mingo("start", "demo/lead"); r.code != 0 {
		t.Fatal(r.out)
	}
	removeFile(claude)
	h.mustWait(30, "two failed starts", func() bool { return len(endedNoCLI.FindAllString(h.screen(), -1)) >= 2 })
	s := h.screen()
	if !strings.Contains(s, "a failed start") || strings.Contains(s, "the loop ended it") || h.starts() != 0 {
		t.Fatal(s)
	}
}

func TestLoopPassesOpenerNameModelEffortAndWhoTheSessionIs(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addDemo()
	args := filepath.Join(h.root, "args")
	h.write(filepath.Join(h.home, ".local", "bin", "claude"),
		"#!/usr/bin/env bash\nprintf '%s\\n' \"$@\" \"$MINGO_PROJECT/$MINGO_ROLE\" \"$PWD\" >"+shellQuote(args)+"\nexec sleep 600\n")
	h.mingo("start", "demo/lead")
	h.mustWait(10, "the args", func() bool { return h.read(args) != "" })
	want := strings.Join([]string{"You are the lead of demo.", "--name", "demo-lead", "--model", "claude-opus-5-5",
		"--effort", "high", "--permission-mode", "auto", "--remote-control", "demo/lead", h.home + "/dev/demo/lead"}, "\n") + "\n"
	if got := h.read(args); got != want {
		t.Fatalf("got\n%s", got)
	}
}

func TestStopWaitsLongerThanTheLoopTakesToEndAHeldSession(t *testing.T) {
	if cli.StopWait <= loop.WatchEvery+loop.ExitGrace+loop.TermGrace {
		t.Fatal(cli.StopWait, loop.WatchEvery+loop.ExitGrace+loop.TermGrace)
	}
}

func TestLoopRunsTheNextSessionFromTheBinaryOnDiskWhenItAnswers(t *testing.T) {
	h := newHome(t)
	h.bin = filepath.Join(h.root, "mingo")
	data, err := os.ReadFile(mingoBin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.bin, data, 0o755); err != nil {
		t.Fatal(err)
	}
	h.startLoop("obeys")
	h.touch(h.marker("restart"))
	h.mustWait(20, "the second start", h.startsAre(2))
	if s := h.screen(); strings.Contains(s, "does not answer") {
		t.Fatal(s)
	}
	h.write(h.bin+".new", "#!/bin/sh\nexit 1\n")
	os.Chmod(h.bin+".new", 0o755)
	if err := os.Rename(h.bin+".new", h.bin); err != nil {
		t.Fatal(err)
	}
	h.touch(h.marker("restart"))
	h.mustWait(20, "the third start", h.startsAre(3))
	if s := h.screen(); !strings.Contains(s, "the mingo on disk does not answer mingo version, continuing with the loaded loop") {
		t.Fatal(s)
	}
}
