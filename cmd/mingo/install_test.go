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
	if write["matcher"] != "Edit|Write|NotebookEdit" || write["hooks"].([]any)[0].(map[string]any)["command"] != bin+" guard claude write" ||
		bash["matcher"] != "Bash|PowerShell" || bash["hooks"].([]any)[0].(map[string]any)["command"] != bin+" guard claude bash" {
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

func TestInstallLeavesASettingsFileWithAKeyOfAnotherTypeAsItIs(t *testing.T) {
	h := newHome(t)
	file := filepath.Join(h.home, ".claude", "settings.json")
	for _, text := range []string{
		`{"permissions": {"deny": "Read(./.env)"}}`,
		`{"permissions": []}`,
		`{"hooks": {"PreToolUse": {}}}`,
		`{"hooks": "none"}`,
	} {
		h.write(file, text)
		if r := h.mingo("install"); r.code == 0 || !strings.Contains(r.out, "left as it is") || h.read(file) != text {
			t.Fatal(text, r.out)
		}
	}
	h.write(file, `{"permissions": {"deny": "Read(./.env)"}}`)
	if r := h.mingo("install"); !strings.Contains(r.out, file+" has a permissions.deny key that is not a JSON list, left as it is") {
		t.Fatal(r.out)
	}
}

func TestInstallSaysWhereASettingsFileStopsBeingJSONAndWhenItIsNoObject(t *testing.T) {
	h := newHome(t)
	file := filepath.Join(h.home, ".claude", "settings.json")
	h.write(file, "{\n  \"theme\": \"dark\",\n  oops\n}\n")
	if r := h.mingo("install"); r.code != 1 || !strings.HasSuffix(r.out, "mingo: "+file+" is not valid JSON at line 3, column 3 (invalid character 'o' looking for beginning of object key string), left as it is\n") {
		t.Fatal(r.code, r.out)
	}
	h.write(file, "[1, 2]\n")
	if r := h.mingo("install"); r.code != 1 || !strings.HasSuffix(r.out, "mingo: "+file+" is not a JSON object, left as it is\n") || h.read(file) != "[1, 2]\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestInstallWritesALinkedSettingsFileWhereTheLinkPoints(t *testing.T) {
	h := newHome(t)
	kept := filepath.Join(h.root, "dotfiles", "settings.json")
	h.write(kept, `{"theme": "dark"}`)
	link := filepath.Join(h.home, ".claude", "settings.json")
	h.mkdir(filepath.Dir(link))
	if err := os.Symlink("../../dotfiles/settings.json", link); err != nil {
		t.Fatal(err)
	}
	r := h.mingo("install")
	if r.code != 0 || !strings.Contains(r.out, "claude settings: "+link+", through the link to "+kept+"\n") {
		t.Fatal(r.out)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced")
	}
	if s := h.settings(); s["theme"] != "dark" || s["isolatePeerMachines"] != true {
		t.Fatal(compact(s))
	}
	if r := h.mingo("install"); !strings.Contains(r.out, "claude settings: "+link+" unchanged\n") {
		t.Fatal(r.out)
	}
}

func TestInstallSaysEachKeyItChangedWithItsOldAndNewValue(t *testing.T) {
	h := newHome(t)
	h.write(filepath.Join(h.home, ".claude", "settings.json"), `{"crossSessionInbound": "hold", "permissions": {"deny": ["Bash(pkill *)"]}}`)
	bin := filepath.Join(h.home, ".local", "bin", "mingo")
	want := "claude settings: " + filepath.Join(h.home, ".claude", "settings.json") + "\n" +
		"  crossSessionInbound was \"hold\", now \"accept\"\n" +
		"  isolatePeerMachines was unset, now true\n" +
		"  permissions.deny gained \"Bash(killall *)\"\n" +
		"  hooks.PreToolUse gained \"" + bin + " guard claude write\"\n" +
		"  hooks.PreToolUse gained \"" + bin + " guard claude bash\"\n"
	if r := h.mingo("install"); r.code != 0 || !strings.Contains(r.out, want) {
		t.Fatal(r.out)
	}
}

func TestTheGuardHooksRunFromAHomeWithASpaceInIt(t *testing.T) {
	h := newHome(t)
	spaced := filepath.Join(h.root, "my home")
	h.mkdir(filepath.Join(spaced, ".local", "bin"))
	if r := h.runIn("", []string{"HOME=" + spaced}, h.bin, "install"); r.code != 0 {
		t.Fatal(r.out)
	}
	var s struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(h.read(filepath.Join(spaced, ".claude", "settings.json"))), &s); err != nil || len(s.Hooks.PreToolUse) != 2 {
		t.Fatal(err, h.read(filepath.Join(spaced, ".claude", "settings.json")))
	}
	command := s.Hooks.PreToolUse[1].Hooks[0].Command
	if command != "'"+filepath.Join(spaced, ".local", "bin", "mingo")+"' guard claude bash" {
		t.Fatal(command)
	}
	in := `{"tool_name": "Bash", "tool_input": {"command": "pkill node"}}`
	if r := h.runIn("", []string{"HOME=" + spaced}, "sh", "-c", "printf '%s' \"$1\" | "+command, "_", in); r.code != 0 || !strings.Contains(r.out, "Never end a process by its name") {
		t.Fatal(r.code, r.out)
	}
}

func TestInstallQuotesTheGuardsAnOlderMingoWroteUnquotedUnderAHomeWithASpace(t *testing.T) {
	h := newHome(t)
	spaced := filepath.Join(h.root, "my home")
	h.mkdir(filepath.Join(spaced, ".local", "bin"))
	bin := filepath.Join(spaced, ".local", "bin", "mingo")
	file := filepath.Join(spaced, ".claude", "settings.json")
	h.write(file, strings.ReplaceAll(`{"hooks": {"PreToolUse": [
  {"matcher": "Edit|Write|NotebookEdit", "hooks": [{"type": "command", "command": "BIN guard write", "timeout": 10}]},
  {"matcher": "Bash|PowerShell", "hooks": [{"type": "command", "command": "BIN guard bash"}, {"type": "command", "command": "'BIN' guard bash"}]},
  {"matcher": "Bash", "hooks": [{"type": "command", "command": "BIN guard bash"}]}
]}}`, "BIN", bin))
	r := h.runIn("", []string{"HOME=" + spaced}, h.bin, "install")
	want := "  hooks.PreToolUse had \"" + bin + " guard write\", now \"'" + bin + "' guard claude write\"\n" +
		"  hooks.PreToolUse had \"" + bin + " guard bash\", now \"'" + bin + "' guard claude bash\"\n" +
		"  hooks.PreToolUse lost \"'" + bin + "' guard bash\", the same guard as \"'" + bin + "' guard claude bash\"\n" +
		"  hooks.PreToolUse lost \"" + bin + " guard bash\", the same guard as \"'" + bin + "' guard claude bash\"\n"
	if r.code != 0 || !strings.Contains(r.out, want) || strings.Contains(r.out, "gained \"'") {
		t.Fatal(r.out)
	}
	var s struct {
		Hooks struct {
			PreToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(h.read(file)), &s); err != nil || len(s.Hooks.PreToolUse) != 2 {
		t.Fatal(err, h.read(file))
	}
	for i, guard := range []string{"write", "bash"} {
		if hooks := s.Hooks.PreToolUse[i].Hooks; len(hooks) != 1 || hooks[0].Command != "'"+bin+"' guard claude "+guard {
			t.Fatal(h.read(file))
		}
	}
	if r := h.runIn("", []string{"HOME=" + spaced}, h.bin, "install"); !strings.Contains(r.out, "claude settings: "+file+" unchanged\n") {
		t.Fatal(r.out)
	}
}

func TestInstallNamesTheProviderInTheGuardsAnOlderMingoWrote(t *testing.T) {
	h := newHome(t)
	bin := filepath.Join(h.home, ".local", "bin", "mingo")
	file := filepath.Join(h.home, ".claude", "settings.json")
	h.write(file, strings.ReplaceAll(`{"hooks": {"PreToolUse": [
  {"matcher": "Edit|Write|NotebookEdit", "hooks": [{"type": "command", "command": "BIN guard write", "timeout": 10}]},
  {"matcher": "Bash|PowerShell", "hooks": [{"type": "command", "command": "BIN guard bash"}]}
]}}`, "BIN", bin))
	r := h.mingo("install")
	want := "  hooks.PreToolUse had \"" + bin + " guard write\", now \"" + bin + " guard claude write\"\n" +
		"  hooks.PreToolUse had \"" + bin + " guard bash\", now \"" + bin + " guard claude bash\"\n"
	if r.code != 0 || !strings.Contains(r.out, want) || strings.Contains(r.out, "PreToolUse gained") {
		t.Fatal(r.out)
	}
	if r := h.mingo("install"); !strings.Contains(r.out, "claude settings: "+file+" unchanged\n") {
		t.Fatal(r.out)
	}
}

func TestInstallKeepsTheModeOfTheSettingsFile(t *testing.T) {
	h := newHome(t)
	file := filepath.Join(h.home, ".claude", "settings.json")
	h.write(file, `{"theme": "dark"}`)
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	h.mingo("install")
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o600 || !strings.Contains(h.read(file), "isolatePeerMachines") {
		t.Fatal(info.Mode(), h.read(file))
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

func TestServiceEnableSaysWhetherLingeringIsOnAndDisableSaysWhatItDid(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd is on linux alone")
	}
	h := newHome(t)
	h.addDemoWith(`autostart = ["lead"]`)
	fake := filepath.Join(h.root, "fake-systemctl")
	h.write(fake, "#!/usr/bin/env bash\nexit 0\n")
	os.Chmod(fake, 0o755)
	h.setEnv("MINGO_SYSTEMCTL="+fake, "USER=someone")
	asked := filepath.Join(h.root, "loginctl")
	for _, c := range []struct{ linger, want string }{
		{"yes", "mingo@demo starts lead at boot, as lingering is on for someone\n"},
		{"no", "mingo@demo starts lead when someone logs in, not at boot, as lingering is off for someone\nloginctl enable-linger turns lingering on, so it starts at boot\n"},
		{"", "mingo@demo starts lead at boot while lingering is on for someone\nloginctl could not say whether it is, loginctl enable-linger turns it on\n"},
	} {
		script := "echo \"$*\" >" + shellQuote(asked) + "\necho Linger=" + c.linger + "\n"
		if c.linger == "" {
			script = "exit 1\n"
		}
		h.fakeTool("loginctl", script)
		if r := h.mingo("service", "enable", "demo"); r.code != 0 || r.out != c.want {
			t.Fatal(c.linger, r.out)
		}
	}
	if got := h.read(asked); got != "show-user "+strconv.Itoa(os.Getuid())+" --property=Linger\n" {
		t.Fatal(got)
	}
	if r := h.mingo("service", "disable", "demo"); r.code != 0 || r.out != "mingo@demo is disabled, so demo's autostart roles no longer start at boot\n" {
		t.Fatal(r.out)
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
	if r := h.mingo("service", "disable", "demo"); r.code != 0 || exists(plist) || r.out != "mingo.demo is disabled, so demo's autostart roles no longer start at login\n" {
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

func TestAnUnknownCommandIsNamedAndExits2(t *testing.T) {
	h := newHome(t)
	for _, words := range [][]string{{"bogus"}, {"bogus", "--help"}} {
		if r := h.mingo(words...); r.code != 2 || r.out != "mingo: unknown command \"bogus\", mingo help lists them\n" {
			t.Fatal(words, r.code, r.out)
		}
	}
	if r := h.mingo("project", "bogus", "x"); r.code != 2 || r.out != "mingo: unknown command \"project bogus\"\nusage: mingo project add|show|edit <name>\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestExtraArgumentsAreNamedWithTheCommandsUsage(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	for _, c := range []struct {
		words []string
		want  string
	}{
		{[]string{"ls", "extra"}, "mingo: extra argument \"extra\"\nusage: mingo ls\n"},
		{[]string{"setup", "demo", "extra"}, "mingo: extra argument \"extra\"\nusage: mingo setup <project>\n"},
		{[]string{"path", "demo/lead", "a", "b"}, "mingo: extra arguments \"a\" \"b\"\nusage: mingo path <target>\n"},
		{[]string{"project", "show", "demo", "x"}, "mingo: extra argument \"x\"\nusage: mingo project show <name>\n"},
		{[]string{"service", "enable", "demo", "x"}, "mingo: extra argument \"x\"\nusage: mingo service enable|disable <project>\n"},
		{[]string{"status", "--json"}, "mingo: unknown flag \"--json\"\nusage: mingo status [project...]\n"},
		{[]string{"project", "add"}, "usage: mingo project add <name>\n"},
		{[]string{"say", "demo/lead"}, "usage: mingo say <target> <message>\n"},
	} {
		if r := h.mingo(c.words...); r.code != 2 || r.out != c.want {
			t.Fatal(c.words, r.code, r.out)
		}
	}
}

func TestAHelpFlagAnywhereBeforeTwoDashesShowsTheCommandsPage(t *testing.T) {
	h := newHome(t)
	for _, c := range []struct {
		words []string
		page  string
	}{
		{[]string{"start", "demo", "--autostart", "--help"}, "mingo start [--autostart] [target...]\n"},
		{[]string{"stop", "--all", "-h"}, "mingo stop [target...]\n"},
		{[]string{"pr", "merge", "12", "--help"}, "mingo pr merge <number>\n"},
		{[]string{"service", "enable", "demo", "-h"}, "mingo service enable|disable <project>\n"},
		{[]string{"help", "--help"}, "mingo help [command]\n"},
	} {
		if r := h.mingo(c.words...); r.code != 0 || !strings.HasPrefix(r.out, c.page) {
			t.Fatal(c.words, r.code, r.out)
		}
	}
	if r := h.mingo("heavy", "label", "--", "sh", "-c", "echo ran $1", "_", "--help"); r.code != 0 || r.out != "ran --help\n" {
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
	write := `{"tool_name": "Write", "tool_input": {"file_path": "` + h.home + `/.bashrc"}}`
	kill := `{"tool_name": "Bash", "tool_input": {"command": "pkill node"}}`
	// a hook an older mingo wrote names no provider, and is Claude Code's
	for _, provider := range []string{"claude ", ""} {
		r := h.runIn("", []string{"CLAUDE_PROJECT_DIR=" + project, "TMPDIR=" + filepath.Join(h.root, "tmp")}, "bash", "-c", "printf '%s' \"$1\" | \"$2\" guard "+provider+"write", "_", write, h.bin)
		if r.code != 0 || !strings.Contains(r.out, `"permissionDecision":"deny"`) {
			t.Fatal(provider, r.out)
		}
		r = h.runIn("", nil, "bash", "-c", "printf '%s' \"$1\" | \"$2\" guard "+provider+"bash", "_", kill, h.bin)
		if r.code != 0 || !strings.Contains(r.out, "Never end a process by its name") {
			t.Fatal(provider, r.out)
		}
	}
	in := `{"tool_name": "Bash", "tool_input": {"command": "kill 1234"}}`
	if r := h.runIn("", nil, "bash", "-c", "printf '%s' \"$1\" | \"$2\" guard claude bash", "_", in, h.bin); r.code != 0 || r.out != "" {
		t.Fatal(r.out)
	}
	if r := h.runIn("", nil, "bash", "-c", "printf '%s' \"$1\" | \"$2\" guard nobody bash", "_", in, h.bin); r.code != 2 {
		t.Fatal(r.code, r.out)
	}
}
