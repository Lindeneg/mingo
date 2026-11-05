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
	"time"

	"github.com/Lindeneg/mingo/internal/cli"
	"github.com/Lindeneg/mingo/internal/loop"
)

var (
	endedZero = regexp.MustCompile(`session ended with code 0 after [0-9]+ s`)
	endedTerm = regexp.MustCompile(`session ended with code 143 after [0-9]+ s`)
	endedKill = regexp.MustCompile(`session ended with code 137 after [0-9]+ s`)
	endedOne  = regexp.MustCompile(`session ended with code 1 after [0-9]+ s`)
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

// /exit typed into the pane ends the session without the restart marker, so the loop counts it a failed start
func (h *home) typeExit() {
	h.t.Helper()
	if err := exec.Command("tmux", "-S", h.socket, "send-keys", "-t", "=demo-lead:", "/exit", "Enter").Run(); err != nil {
		h.t.Fatal(err)
	}
}

func TestLoopTypesNothingForAMarkerBetweenTwoSessions(t *testing.T) {
	h := newHome(t)
	h.setEnv("MINGO_LOOP_DELAY=12")
	h.startLoop("obeys")
	h.setMode("listens")
	h.typeExit()
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

func (h *home) installQuitter() {
	h.t.Helper()
	file := filepath.Join(h.home, ".local", "bin", "claude")
	h.write(file, "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(file, 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func TestLoopSaysAFailedStartLeavesNoMarkerAndTriesAgain(t *testing.T) {
	h := newHome(t)
	h.setEnv("MINGO_LOOP_DELAY=2")
	h.installQuitter()
	h.addDemo()
	h.mingo("start", "demo/lead")
	h.mustWait(30, "two failed starts", func() bool { return len(endedOne.FindAllString(h.screen(), -1)) >= 2 })
	s := h.screen()
	if !strings.Contains(s, "a failed start") || strings.Contains(s, "the loop ended it") || h.starts() != 0 || exists(h.marker("running")) {
		t.Fatal(s)
	}
}

// the loop as mingo start runs it, without the checks start makes before it
func (h *home) startLoopDirectly() {
	h.t.Helper()
	args := append([]string{"-S", h.socket, "new-session", "-d", "-s", "demo-lead", "-c", h.home, "env"}, h.env()...)
	cmd := exec.Command("tmux", append(args, h.bin, "loop", "demo", "lead")...)
	cmd.Env = h.env()
	if out, err := cmd.CombinedOutput(); err != nil {
		h.t.Fatal(err, string(out))
	}
}

func TestLoopWaitsForAMissingClaudeAndStartsItOnceItIsThere(t *testing.T) {
	h := newHome(t)
	later := filepath.Join(h.root, "later", "claude")
	h.setEnv("MINGO_CLAUDE=" + later)
	h.installStandIn()
	h.addDemo()
	h.startLoopDirectly()
	h.mustWait(15, "the missing claude named", func() bool {
		return strings.Contains(h.screen(), `MINGO_CLAUDE is "`+later+`", which cannot be run: no such file or directory`)
	})
	sleepSeconds(2)
	if s := h.screen(); strings.Contains(s, "session ended") || strings.Count(s, "which cannot be run") != 1 || !strings.Contains(s, "the loop tries again every 1 s") {
		t.Fatal(s)
	}
	h.mkdir(filepath.Dir(later))
	if err := os.Rename(filepath.Join(h.home, ".local", "bin", "claude"), later); err != nil {
		t.Fatal(err)
	}
	h.mustWait(15, "the first start", h.startsAre(1))
}

func TestLoopStartedOnAConfigThatDoesNotLoadSaysSoAndStartsOnceItLoads(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addDemo()
	file := filepath.Join(h.config, "projects", "demo.toml")
	good := h.read(file)
	h.write(file, "[[roles]\n")
	h.startLoopDirectly()
	h.mustWait(15, "the config's problem", func() bool { return strings.Contains(h.screen(), "[mingo] demo/lead: "+file+": ") })
	if s := h.screen(); !strings.Contains(s, "[mingo] demo/lead: the loop tries again every 1 s") || h.starts() != 0 {
		t.Fatal(s)
	}
	h.write(file, good)
	h.mustWait(15, "the first start", h.startsAre(1))
}

func TestLoopGoesOnWithTheConfigItHasWhenTheConfigStopsLoading(t *testing.T) {
	h := newHome(t)
	h.startLoop("obeys")
	file := filepath.Join(h.config, "projects", "demo.toml")
	good := h.read(file)
	h.write(file, "[[roles]\n")
	h.touch(h.marker("restart"))
	h.mustWait(20, "the second start", h.startsAre(2))
	s := h.screen()
	if !strings.Contains(s, "[mingo] demo-lead: "+file+": ") || !strings.Contains(s, "the loop goes on with the config it loaded and tries the file again at the next session boundary") {
		t.Fatal(s)
	}
	h.write(file, good)
	h.touch(h.marker("restart"))
	h.mustWait(20, "the third start", h.startsAre(3))
	if s := h.screen(); strings.Count(s, "tries the file again") != 1 {
		t.Fatal(s)
	}
}

func TestLoopCountsNoSessionTheRestartMarkerEndedAsAFailedStart(t *testing.T) {
	h := newHome(t)
	h.setEnv("MINGO_LOOP_DELAY=30")
	h.startLoop("obeys")
	h.touch(h.marker("restart"))
	h.mustWait(20, "the second start", h.startsAre(2))
	if s := h.screen(); strings.Contains(s, "a failed start") {
		t.Fatal(s)
	}
}

func TestStopEndsALoopThatWaitsOutAFailedStartWithinSeconds(t *testing.T) {
	h := newHome(t)
	h.setEnv("MINGO_LOOP_DELAY=120")
	h.installQuitter()
	h.addDemo()
	h.mingo("start", "demo/lead")
	h.mustWait(15, "the failed start", func() bool { return strings.Contains(h.screen(), "a failed start, next try in 120 s") })
	started := time.Now()
	r := h.mingo("stop", "demo/lead")
	if r.code != 0 || r.out != "demo-lead: stopped\n" || time.Since(started) > 10*time.Second {
		t.Fatal(r.out, time.Since(started))
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
