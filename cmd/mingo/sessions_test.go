//go:build unix

package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStopEndsAHeldSessionThroughTheLoop(t *testing.T) {
	h := newHome(t)
	h.startLoop("held")
	first := h.nthStart(1)
	r := h.mingo("stop", "demo/lead")
	if r.out != "demo-lead: stopped\n" || isAlive(first) || h.starts() != 1 {
		t.Fatal(r.out)
	}
}

func TestRestartForceSendsCtrlCThenExitAndThePid(t *testing.T) {
	h := newHome(t)
	h.startLoop("raw")
	first := h.nthStart(1)
	h.setMode("held")
	r := h.mingo("restart", "--force", "demo/lead")
	if !strings.Contains(r.out, "demo-lead: interrupted") {
		t.Fatal(r.out)
	}
	h.mustWait(30, "the second start", h.startsAre(2))
	if isAlive(first) || !strings.Contains(h.screen(), "the loop ended it with SIGTERM") {
		t.Fatal(h.screen())
	}
}

func TestStartStartsEveryRoleAndSaysWhenOneRuns(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addDemo()
	r := h.mingo("start", "demo")
	lines := r.lines()
	if len(lines) < 2 || lines[0] != "demo-lead: started on claude-opus-5-5, effort high" ||
		lines[1] != "demo-helper: started on the default model, effort default" {
		t.Fatal(r.out)
	}
	if r := h.mingo("start", "demo/h"); r.out != "demo-helper: already running\n" {
		t.Fatal(r.out)
	}
}

func TestStartAutostartStartsOnlyTheAutostartRoles(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addDemoWith(`autostart = ["helper"]`)
	if r := h.mingo("start", "--autostart", "demo"); r.out != "demo-helper: started on the default model, effort default\n" {
		t.Fatal(r.out)
	}
}

func TestStartAutostartStartsNothingWithoutAutostartRoles(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addDemo()
	if r := h.mingo("start", "--autostart", "demo"); r.out != "demo: no autostart roles, nothing to start\n" {
		t.Fatal(r.out)
	}
	r := h.mingo("agents", "demo")
	if lines := r.lines(); len(lines) < 2 || !regexp.MustCompile(`^demo +lead +stopped`).MatchString(lines[1]) {
		t.Fatal(r.out)
	}
}

func TestStartRefusesARoleWithoutAClone(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	if err := removeDir(filepath.Join(h.home, "dev", "demo", "helper")); err != nil {
		t.Fatal(err)
	}
	r := h.mingo("start", "demo/helper")
	if r.code == 0 || !strings.Contains(r.out, "demo-helper has no clone at "+h.home+"/dev/demo/helper") {
		t.Fatal(r.out)
	}
}

func TestStatusSaysWhatRunsAndWhatAStartWouldUse(t *testing.T) {
	h := newHome(t)
	h.startLoop("obeys")
	lines := h.mingo("status", "demo").lines()
	if len(lines) < 3 || lines[0] != "demo-lead: running, on claude-opus-5-5, effort high" ||
		lines[1] != "demo-helper: not running, would start on the default model, effort default" ||
		lines[2] != "stand-in is logged in" {
		t.Fatal(lines)
	}
}

func TestSayTypesIntoARunningSessionAndIsRefusedBetweenSessions(t *testing.T) {
	h := newHome(t)
	h.startLoop("listens")
	if r := h.mingo("say", "demo/l", "hello", "there"); r.code != 0 {
		t.Fatal(r.out)
	}
	h.mustWait(10, "the typed text", func() bool { return h.typed() == "hello there" })
	removeFile(h.marker("running"))
	r := h.mingo("say", "demo/l", "again")
	if r.code == 0 || !strings.Contains(r.out, "between sessions") {
		t.Fatal(r.out)
	}
}

func TestClearFromInsideACloneRestartsThatRole(t *testing.T) {
	h := newHome(t)
	h.startLoop("obeys")
	r := h.runIn(filepath.Join(h.home, "dev", "demo", "lead"), nil, h.bin, "clear")
	if r.out != "demo/lead: the loop ends this session within seconds and starts a fresh one\n" {
		t.Fatal(r.out)
	}
	h.mustWait(20, "the second start", h.startsAre(2))
}

func TestClearInsideASessionUsesTheNamesTheLoopGaveIt(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	h.runIn(h.root, []string{"MINGO_PROJECT=demo", "MINGO_ROLE=helper"}, h.bin, "clear")
	if !exists(h.marker("restart", "demo", "helper")) {
		t.Fatal("no restart marker")
	}
}

func TestClearOutsideEveryCloneRefuses(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	if r := h.runIn(h.root, nil, h.bin, "clear"); r.code == 0 {
		t.Fatal(r.out)
	}
}

func TestPeekShowsTheLastLinesOfASessionsScreen(t *testing.T) {
	h := newHome(t)
	h.startLoop("obeys")
	if r := h.mingo("peek", "demo/lead", "40"); !strings.Contains(r.out, "fresh session at") {
		t.Fatal(r.out)
	}
	if r := h.mingo("peek", "demo/lead", "lots"); r.code == 0 {
		t.Fatal(r.out)
	}
}

func TestAgentsListsEveryRoleOfEveryProject(t *testing.T) {
	h := newHome(t)
	h.startLoop("obeys")
	h.addProject("other", `[[roles]]`, `name = "solo"`)
	lines := h.mingo("agents").lines()
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "PROJECT") ||
		!regexp.MustCompile(`^demo +lead +(starting|between)`).MatchString(lines[1]) ||
		!regexp.MustCompile(`^demo +helper +stopped`).MatchString(lines[2]) ||
		!regexp.MustCompile(`^other +solo +stopped`).MatchString(lines[3]) {
		t.Fatal(lines)
	}
}

func TestAgentsSizesEveryColumnToItsLongestValue(t *testing.T) {
	h := newHome(t)
	h.addProject("corner-bakery-shop", `[[roles]]`, `name = "lead"`, `[[roles]]`, `name = "a-rather-long-role"`)
	h.addProject("mingo", `[[roles]]`, `name = "lead"`)
	want := "PROJECT             ROLE                STATE    UP  LINK\n" +
		"corner-bakery-shop  lead                stopped  -   -\n" +
		"corner-bakery-shop  a-rather-long-role  stopped  -   -\n" +
		"mingo               lead                stopped  -   -\n"
	if r := h.mingo("agents"); r.out != want {
		t.Fatalf("got\n%s", r.out)
	}
}

func TestStopOfAProjectThatDoesNotRunSaysSo(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	if r := h.mingo("stop", "demo"); r.out != "demo-lead: not running\ndemo-helper: not running\n" {
		t.Fatal(r.out)
	}
}
