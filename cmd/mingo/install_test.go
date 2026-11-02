//go:build unix

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func (h *home) settings() map[string]any {
	h.t.Helper()
	var s map[string]any
	if err := json.Unmarshal([]byte(h.read(filepath.Join(h.home, ".claude", "settings.json"))), &s); err != nil {
		h.t.Fatal(err)
	}
	return s
}

func compact(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestInstallCopiesTheCommandAndWritesTmuxOnlyWhenNone(t *testing.T) {
	h := newHome(t)
	if r := h.mingo("install"); r.code != 0 {
		t.Fatal(r.out)
	}
	bin := filepath.Join(h.home, ".local", "bin", "mingo")
	if r := h.runIn("", nil, bin, "version"); !strings.HasPrefix(r.out, "mingo ") {
		t.Fatal(r.out)
	}
	if exists(filepath.Join(h.home, ".bashrc")) || exists(filepath.Join(h.home, ".local", "share", "mingo")) {
		t.Fatal("install wrote shell files")
	}
	if !exists(filepath.Join(h.home, ".tmux.conf")) {
		t.Fatal("no tmux config")
	}
	h.write(filepath.Join(h.home, ".tmux.conf"), "mine\n")
	h.runIn("", nil, bin, "install")
	if h.read(filepath.Join(h.home, ".tmux.conf")) != "mine\n" {
		t.Fatal(h.read(filepath.Join(h.home, ".tmux.conf")))
	}
}

func TestInstallReplacesALinkAtTheCommandsPathAndNeverWritesThroughIt(t *testing.T) {
	h := newHome(t)
	target := filepath.Join(h.root, "elsewhere")
	h.write(target, "#!/bin/sh\necho elsewhere\n")
	link := filepath.Join(h.home, ".local", "bin", "mingo")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	h.mingo("install")
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the link was not replaced")
	}
	if h.read(target) != "#!/bin/sh\necho elsewhere\n" {
		t.Fatal("wrote through the link")
	}
}

func TestInstallAddsDenyRulesGuardsAndMessagingKeepingEveryOtherKey(t *testing.T) {
	h := newHome(t)
	h.write(filepath.Join(h.home, ".claude", "settings.json"),
		`{"theme": "dark", "permissions": {"deny": ["Read(./.env)"], "allow": ["Bash(ls *)"]}, "hooks": {"Stop": []}}`)
	h.mingo("install")
	s := h.settings()
	perms := s["permissions"].(map[string]any)
	hooks := s["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	bin := filepath.Join(h.home, ".local", "bin", "mingo")
	if s["theme"] != "dark" || compact(perms["deny"]) != `["Read(./.env)","Bash(pkill *)","Bash(killall *)"]` ||
		compact(perms["allow"]) != `["Bash(ls *)"]` || compact(hooks["Stop"]) != `[]` ||
		s["crossSessionInbound"] != "accept" || s["isolatePeerMachines"] != true || len(pre) != 2 {
		t.Fatal(compact(s))
	}
	write, bash := pre[0].(map[string]any), pre[1].(map[string]any)
	if write["matcher"] != "Edit|Write|NotebookEdit" || write["hooks"].([]any)[0].(map[string]any)["command"] != bin+" guard write" ||
		bash["matcher"] != "Bash|PowerShell" || bash["hooks"].([]any)[0].(map[string]any)["command"] != bin+" guard bash" {
		t.Fatal(compact(pre))
	}
	keys := h.read(filepath.Join(h.home, ".claude", "settings.json"))
	if !(strings.Index(keys, `"theme"`) < strings.Index(keys, `"permissions"`) && strings.Index(keys, `"permissions"`) < strings.Index(keys, `"hooks"`)) {
		t.Fatal("key order changed\n" + keys)
	}
}

func TestInstallASecondTimeChangesNothingInTheClaudeSettings(t *testing.T) {
	h := newHome(t)
	h.mingo("install")
	file := filepath.Join(h.home, ".claude", "settings.json")
	before := h.read(file)
	r := h.mingo("install")
	if !strings.Contains(r.out, "unchanged") || h.read(file) != before {
		t.Fatal(r.out)
	}
}

func TestInstallLeavesASettingsFileItCannotReadAsItIs(t *testing.T) {
	h := newHome(t)
	file := filepath.Join(h.home, ".claude", "settings.json")
	h.write(file, "{broken\n")
	if r := h.mingo("install"); r.code == 0 || h.read(file) != "{broken\n" {
		t.Fatal(r.out)
	}
}

func TestServiceEnableInstallsTheUnitAndEnablesTheProjectsInstance(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd is on linux alone")
	}
	h := newHome(t)
	h.addDemo()
	calls := filepath.Join(h.root, "systemctl")
	fake := filepath.Join(h.root, "fake-systemctl")
	h.write(fake, "#!/usr/bin/env bash\necho \"$*\" >>"+shellQuote(calls)+"\n")
	os.Chmod(fake, 0o755)
	r := h.runIn("", []string{"MINGO_SYSTEMCTL=" + fake}, h.bin, "service", "enable", "demo")
	if r.code != 0 {
		t.Fatal(r.out)
	}
	shipped, _ := os.ReadFile(filepath.Join(repoRoot(), "share", "mingo@.service"))
	unit := filepath.Join(h.home, ".config", "systemd", "user", "mingo@.service")
	if h.read(unit) != string(shipped) {
		t.Fatal("unit differs")
	}
	if info, _ := os.Stat(unit); info.Mode().Perm() != 0o644 {
		t.Fatal(info.Mode())
	}
	if h.read(calls) != "--user daemon-reload\n--user enable mingo@demo.service\n" {
		t.Fatal(h.read(calls))
	}
}

func TestServiceEnableWritesThePlistAndEnablesTheAgentAndDisableUndoesIt(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd is on macos alone")
	}
	h := newHome(t)
	h.addDemoWith(`autostart = ["lead"]`)
	calls := filepath.Join(h.root, "launchctl")
	fake := filepath.Join(h.root, "fake-launchctl")
	h.write(fake, "#!/usr/bin/env bash\necho \"$*\" >>"+shellQuote(calls)+"\n")
	os.Chmod(fake, 0o755)
	h.setEnv("MINGO_LAUNCHCTL=" + fake)
	r := h.mingo("service", "enable", "demo")
	if r.code != 0 || r.out != "mingo.demo starts lead at login\n" {
		t.Fatal(r.out)
	}
	plist := filepath.Join(h.home, "Library", "LaunchAgents", "mingo.demo.plist")
	if info, err := os.Stat(plist); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatal("no plist, or not 0644")
	}
	if !strings.Contains(h.read(plist), "<string>"+h.home+"/.local/bin/mingo</string>") {
		t.Fatal(h.read(plist))
	}
	if out, err := exec.Command("plutil", "-lint", plist).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	target := "gui/" + strconv.Itoa(os.Getuid()) + "/mingo.demo"
	if h.read(calls) != "enable "+target+"\n" {
		t.Fatal(h.read(calls))
	}
	if r := h.mingo("service", "disable", "demo"); r.code != 0 || exists(plist) {
		t.Fatal(r.out)
	}
	if r := h.mingo("service", "disable", "demo"); r.code != 0 {
		t.Fatal(r.out)
	}
	if h.read(calls) != "enable "+target+"\ndisable "+target+"\ndisable "+target+"\n" {
		t.Fatal(h.read(calls))
	}
}

func TestTheUnitStartsAutostartRolesAndStopsTheWholeProject(t *testing.T) {
	unit, err := os.ReadFile(filepath.Join(repoRoot(), "share", "mingo@.service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"ExecStart=%h/.local/bin/mingo start --autostart %i", "ExecStop=%h/.local/bin/mingo stop %i", "KillMode=process"} {
		if !strings.Contains(string(unit), "\n"+line+"\n") {
			t.Fatal(line)
		}
	}
}

func TestHelpListsTheCommandsAndExplainsEach(t *testing.T) {
	h := newHome(t)
	r := h.mingo("help")
	if r.code != 0 || !strings.Contains(r.out, "\n  mingo attach <target>") || !strings.HasSuffix(r.out, "mingo help <command> explains a command, and so does mingo <command> --help.\n") {
		t.Fatal(r.out)
	}
	for _, words := range [][]string{{"help", "attach"}, {"attach", "--help"}, {"attach", "-h"}} {
		if r := h.mingo(words...); r.code != 0 || !strings.HasPrefix(r.out, "mingo attach <target>\n\nAttaches this terminal") {
			t.Fatal(words, r.out)
		}
	}
	for _, words := range [][]string{{"help", "project", "add"}, {"project", "add", "--help"}} {
		if r := h.mingo(words...); r.code != 0 || !strings.HasPrefix(r.out, "mingo project add <name>\n") {
			t.Fatal(words, r.out)
		}
	}
	if r := h.mingo("help", "pr", "merge"); !strings.HasPrefix(r.out, "mingo pr merge <number>\n") {
		t.Fatal(r.out)
	}
	if r := h.mingo("help", "project"); !strings.Contains(r.out, "  add    writes a config from the template") {
		t.Fatal(r.out)
	}
	if r := h.mingo("help", "new"); !strings.Contains(r.out, "--repo <value>") || !strings.Contains(r.out, "--workflow-base <value>") || !strings.Contains(r.out, "master unless set") {
		t.Fatal(r.out)
	}
	if r := h.mingo("help", "nope"); r.code != 1 || r.out != "mingo: no command nope, mingo help lists them\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestEveryCommandInTheListHasAPage(t *testing.T) {
	h := newHome(t)
	for _, line := range h.mingo("help").lines() {
		f := strings.Fields(line)
		if !strings.HasPrefix(line, "  mingo ") || len(f) < 2 {
			continue
		}
		words := []string{f[1]}
		if len(f) > 2 && !strings.HasPrefix(f[2], "<") && !strings.HasPrefix(f[2], "[") && !strings.Contains(f[2], "|") {
			words = append(words, f[2])
		}
		if r := h.mingo(append([]string{"help"}, words...)...); r.code != 0 || !strings.HasPrefix(r.out, "mingo "+words[0]) {
			t.Fatal(words, r.out)
		}
	}
}

func TestGuardCommandsAnswerTheHook(t *testing.T) {
	h := newHome(t)
	project := filepath.Join(h.home, "dev", "demo", "lead")
	in := `{"tool_name": "Write", "tool_input": {"file_path": "` + h.home + `/.bashrc"}}`
	r := h.runIn("", []string{"CLAUDE_PROJECT_DIR=" + project, "TMPDIR=" + filepath.Join(h.root, "tmp")}, "bash", "-c", "printf '%s' \"$1\" | \"$2\" guard write", "_", in, h.bin)
	if r.code != 0 || !strings.Contains(r.out, `"permissionDecision":"deny"`) {
		t.Fatal(r.out)
	}
	in = `{"tool_name": "Bash", "tool_input": {"command": "pkill node"}}`
	r = h.runIn("", nil, "bash", "-c", "printf '%s' \"$1\" | \"$2\" guard bash", "_", in, h.bin)
	if r.code != 0 || !strings.Contains(r.out, "Never end a process by its name") {
		t.Fatal(r.out)
	}
	in = `{"tool_name": "Bash", "tool_input": {"command": "kill 1234"}}`
	if r = h.runIn("", nil, "bash", "-c", "printf '%s' \"$1\" | \"$2\" guard bash", "_", in, h.bin); r.code != 0 || r.out != "" {
		t.Fatal(r.out)
	}
}
