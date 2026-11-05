//go:build unix

package main

import (
	"os"
	"os/exec"
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

func TestStartTakesItsFlagAnywhereAndRefusesAnyOther(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addDemoWith(`autostart = ["helper"]`)
	r := h.mingo("start", "demo", "--bogus")
	if r.code != 2 || r.out != "mingo: unknown flag \"--bogus\"\nusage: mingo start [--autostart] [target...]\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("start", "demo", "--autostart"); r.out != "demo-helper: started on the default model, effort default\n" {
		t.Fatal(r.out)
	}
}

func TestStartAndStopResolveEveryTargetBeforeTouchingAnySession(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addDemo()
	r := h.mingo("start", "demo/lead", "demo/typo")
	if r.code != 1 || r.out != "mingo: demo has no role or alias typo, it has lead helper\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("status", "demo"); !strings.HasPrefix(r.out, "demo-lead: not running") {
		t.Fatal(r.out)
	}
	h.startLoop("obeys")
	first := h.nthStart(1)
	if r := h.mingo("stop", "demo/lead", "demo/typo"); r.code != 1 || !strings.Contains(r.out, "no role or alias typo") {
		t.Fatal(r.code, r.out)
	}
	if !isAlive(first) || exists(h.marker("stop")) {
		t.Fatal("stop touched demo/lead")
	}
}

func TestStopAndRestartRefuseAnUnknownFlagAndRestartTakesOneTarget(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	if r := h.mingo("stop", "--all"); r.code != 2 || r.out != "mingo: unknown flag \"--all\"\nusage: mingo stop [target...]\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("restart", "demo/lead", "demo/helper"); r.code != 2 || r.out != "mingo: extra argument \"demo/helper\"\nusage: mingo restart [--force] <target>\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("restart", "demo/helper", "--force"); r.code != 1 || r.out != "mingo: demo-helper is not running, use mingo start\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestStartRefusesWhileClaudeCodeIsNotInstalled(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	r := h.runIn("", []string{"MINGO_CLAUDE=/nowhere/claude"}, h.bin, "start", "demo/lead")
	if r.code != 1 || r.out != "mingo: /nowhere/claude is not installed, so no session can start, see https://code.claude.com/docs/en/setup\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("status", "demo"); !strings.HasPrefix(r.out, "demo-lead: not running") {
		t.Fatal(r.out)
	}
}

// a path of tmux alone, so no claude a machine has installed is found
func (h *home) pathWithoutClaude() string {
	h.t.Helper()
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		h.t.Fatal(err)
	}
	dir := filepath.Join(h.root, "only-tmux")
	h.mkdir(dir)
	if err := os.Symlink(tmux, filepath.Join(dir, "tmux")); err != nil {
		h.t.Fatal(err)
	}
	return "PATH=" + dir
}

func TestStatusSaysClaudeCodeIsNotInstalledRatherThanNotLoggedIn(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	r := h.runIn("", []string{h.pathWithoutClaude()}, h.bin, "status", "demo")
	if lines := r.lines(); r.code != 0 || len(lines) != 3 || lines[2] != "claude: not installed on this host, see https://code.claude.com/docs/en/setup" {
		t.Fatal(r.code, r.out)
	}
}

func TestLsStatusAndAgentsShowAConfigThatDoesNotLoadInItsPlaceAndGoOn(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addProject("broken", `roles = 1`)
	h.addDemo()
	h.addProject("zeta", `[[roles]]`, `name = "solo"`)
	file := filepath.Join(h.config, "projects", "broken.toml")
	for _, words := range [][]string{{"ls"}, {"status"}, {"agents"}} {
		r := h.mingo(words...)
		lines := r.lines()
		if r.code != 1 || len(lines) < 3 || !strings.HasPrefix(lines[0], "mingo: "+file+": ") && !strings.HasPrefix(lines[1], "mingo: "+file+": ") ||
			!strings.Contains(r.out, "demo") || !strings.Contains(r.out, "solo") {
			t.Fatal(words, r.code, r.out)
		}
	}
	if r := h.mingo("status", "demo", "nope"); r.code != 1 || !strings.Contains(r.out, "demo-helper: not running") || !strings.Contains(r.out, "mingo: no project named nope") {
		t.Fatal(r.code, r.out)
	}
}

func TestStartRefusesARoleWithoutAClone(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
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
	if r := h.mingo("say", "demo/l", "hello", "-h", "there"); r.code != 0 {
		t.Fatal(r.out)
	}
	h.mustWait(10, "the typed text", func() bool { return h.typed() == "hello -h there" })
	removeFile(h.marker("running"))
	r := h.mingo("say", "demo/l", "again")
	if r.code == 0 || !strings.Contains(r.out, "between sessions") {
		t.Fatal(r.out)
	}
}

func TestSayShowsItsHelpOnlyForAHelpWordBeforeTheMessage(t *testing.T) {
	h := newHome(t)
	if r := h.mingo("say", "-h"); r.code != 0 || !strings.HasPrefix(r.out, "mingo say ") {
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
	h.installStandIn()
	h.addDemo()
	if r := h.mingo("start", "demo/helper"); r.code != 0 {
		t.Fatal(r.out)
	}
	h.mustWait(15, "the first start", h.startsAre(1))
	r := h.runIn(h.root, []string{"MINGO_PROJECT=demo", "MINGO_ROLE=helper"}, h.bin, "clear")
	if r.out != "demo/helper: the loop ends this session within seconds and starts a fresh one\n" {
		t.Fatal(r.out)
	}
	h.mustWait(20, "the second start", h.startsAre(2))
}

func TestClearSaysSoAndChangesNothingWhenNoSessionOfTheRoleRuns(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	r := h.runIn(h.root, []string{"MINGO_PROJECT=demo", "MINGO_ROLE=helper"}, h.bin, "clear")
	if r.code != 1 || r.out != "mingo: demo-helper is not running, so there is no session to clear\n" || exists(h.marker("restart", "demo", "helper")) {
		t.Fatal(r.code, r.out)
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

// the config made unloadable, and what loading it says
func (h *home) breakDemo() string {
	h.t.Helper()
	file := filepath.Join(h.config, "projects", "demo.toml")
	h.write(file, "[[roles]\n")
	r := h.mingo("path", "demo/lead")
	if r.code != 1 || !strings.HasPrefix(r.out, "mingo: "+file+": ") {
		h.t.Fatal(r.code, r.out)
	}
	return strings.TrimSuffix(strings.TrimPrefix(r.out, "mingo: "), "\n")
}

func TestTheLoopControlsReachASessionWrittenAsProjectAndRoleWhileTheConfigDoesNotLoad(t *testing.T) {
	h := newHome(t)
	h.startLoop("listens")
	problem := h.breakDemo()
	warning := "mingo: demo's config does not load, so the command goes to the tmux session demo-lead: " + problem + "\n"
	if r := h.mingo("say", "demo/lead", "hello"); r.code != 0 || r.out != warning {
		t.Fatal(r.code, r.out)
	}
	h.mustWait(10, "the typed text", func() bool { return h.typed() == "hello" })
	if r := h.mingo("peek", "demo/lead"); r.code != 0 || !strings.HasPrefix(r.out, warning) || !strings.Contains(r.out, "fresh session at") {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("restart", "demo/lead"); r.code != 0 || r.out != warning+"demo-lead: /exit within seconds ends the session where it stands, then the loop starts a fresh one\n" {
		t.Fatal(r.code, r.out)
	}
	h.mustWait(30, "the second start", h.startsAre(2))
	if r := h.mingo("stop", "demo/lead"); r.code != 0 || r.out != warning+"demo-lead: stopped\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestATargetWithoutItsRoleIsRefusedWhileTheConfigDoesNotLoad(t *testing.T) {
	h := newHome(t)
	h.startLoop("obeys")
	problem := h.breakDemo()
	want := "mingo: demo's config does not load, so write the target as demo/<role>: " + problem + "\n"
	for _, words := range [][]string{{"attach", "demo"}, {"say", "demo", "hello"}} {
		if r := h.mingo(words...); r.code != 1 || r.out != want {
			t.Fatal(words, r.code, r.out)
		}
	}
	if r := h.runIn("", []string{"MINGO_PROJECT=demo"}, h.bin, "peek", "lead"); r.code != 1 || r.out != want {
		t.Fatal(r.code, r.out)
	}
	if exists(h.marker("stop")) || h.starts() != 1 {
		t.Fatal("a refused target touched the session")
	}
}

func TestStopOfAProjectWhoseConfigDoesNotLoadStopsEachRoleThatHasRunASession(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	problem := h.breakDemo()
	if r := h.mingo("stop", "demo"); r.code != 1 || r.out != "mingo: demo's config does not load, so write the target as demo/<role>: "+problem+"\n" {
		t.Fatal(r.code, r.out)
	}
	h.startLoop("obeys")
	h.breakDemo()
	want := "mingo: demo's config does not load, so the command goes to the tmux sessions of the roles that have run, demo-lead: " + problem + "\n" +
		"demo-lead: stopped\n"
	if r := h.mingo("stop", "demo"); r.code != 0 || r.out != want {
		t.Fatal(r.code, r.out)
	}
}

func TestClearInsideASessionNeedsNoConfigThatLoads(t *testing.T) {
	h := newHome(t)
	h.startLoop("obeys")
	h.breakDemo()
	r := h.runIn(h.root, []string{"MINGO_PROJECT=demo", "MINGO_ROLE=lead"}, h.bin, "clear")
	if r.code != 0 || r.out != "demo/lead: the loop ends this session within seconds and starts a fresh one\n" {
		t.Fatal(r.code, r.out)
	}
	h.mustWait(20, "the second start", h.startsAre(2))
}

func TestAgentsAsksTheClaudeThatMingoClaudeNames(t *testing.T) {
	h := newHome(t)
	h.startLoop("obeys")
	claude := filepath.Join(h.root, "elsewhere", "claude")
	h.write(claude, "#!/usr/bin/env bash\n"+`echo '[{"name":"demo-lead","pid":1,"status":"busy","startedAt":0}]'`+"\n")
	if err := os.Chmod(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	r := h.runIn("", []string{"MINGO_CLAUDE=" + claude}, h.bin, "agents", "demo")
	if lines := r.lines(); r.code != 0 || len(lines) != 3 || !regexp.MustCompile(`^demo +lead +busy `).MatchString(lines[1]) {
		t.Fatal(r.code, r.out)
	}
}

func TestStatusAsksTheClaudeThatMingoClaudeNames(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	r := h.runIn("", []string{h.pathWithoutClaude(), "MINGO_CLAUDE=/nowhere/claude"}, h.bin, "status", "demo")
	if lines := r.lines(); r.code != 0 || len(lines) != 3 || lines[2] != "/nowhere/claude: not installed on this host, see https://code.claude.com/docs/en/setup" {
		t.Fatal(r.code, r.out)
	}
	h.installStandIn()
	claude := filepath.Join(h.root, "elsewhere", "claude")
	h.mkdir(filepath.Dir(claude))
	if err := os.Rename(filepath.Join(h.home, ".local", "bin", "claude"), claude); err != nil {
		t.Fatal(err)
	}
	r = h.runIn("", []string{"MINGO_CLAUDE=" + claude}, h.bin, "status", "demo")
	if lines := r.lines(); r.code != 0 || len(lines) != 3 || lines[2] != "stand-in is logged in" {
		t.Fatal(r.code, r.out)
	}
}
