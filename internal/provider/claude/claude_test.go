package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/provider"
)

func TestASessionStartsWithItsOpenerNameModelEffortAndArgs(t *testing.T) {
	got := Claude{}.Args(provider.Session{Opener: "Go on.", Name: "demo-lead", Model: "m", Effort: "high", Args: []string{"--x"}})
	if strings.Join(got, "|") != "Go on.|--name|demo-lead|--model|m|--effort|high|--x" {
		t.Fatal(got)
	}
	got = Claude{}.Args(provider.Session{Opener: "Go on.", Name: "demo-lead"})
	if strings.Join(got, "|") != "Go on.|--name|demo-lead" {
		t.Fatal(got)
	}
}

func TestAProjectThatSetsNoArgsRunsInAutoModeWithRemoteControl(t *testing.T) {
	if got := strings.Join(Claude{}.DefaultArgs(), " "); got != "--permission-mode auto --remote-control" {
		t.Fatal(got)
	}
}

func TestTheMemoryFolderIsTheOneClaudeCodeKeepsForTheClone(t *testing.T) {
	if got := (Claude{}).MemoryDir("/home/u", "/home/u/dev/acme/tech-lead"); got != "/home/u/.claude/projects/-home-u-dev-acme-tech-lead/memory" {
		t.Fatal(got)
	}
	if got := Slug("/home/u/dév/a_b.c"); got != "-home-u-d-v-a-b-c" {
		t.Fatal(got)
	}
}

func TestAWriteIsReadFromTheFileOrNotebookPathAndTheCwd(t *testing.T) {
	call, ok := Claude{}.WriteCall([]byte(`{"cwd": "/p", "tool_input": {"file_path": "a.ts"}}`))
	if !ok || len(call.Paths) != 1 || call.Paths[0] != "a.ts" || call.Cwd != "/p" {
		t.Fatal(call, ok)
	}
	call, ok = Claude{}.WriteCall([]byte(`{"tool_input": {"notebook_path": "/n.ipynb"}}`))
	if !ok || len(call.Paths) != 1 || call.Paths[0] != "/n.ipynb" {
		t.Fatal(call, ok)
	}
	for _, in := range []string{"not json", `{"tool_input": {}}`} {
		if _, ok := (Claude{}).WriteCall([]byte(in)); ok {
			t.Fatal(in)
		}
	}
}

func TestACommandIsReadWithWhetherItIsPowerShell(t *testing.T) {
	call, ok := Claude{}.CommandCall([]byte(`{"tool_name": "PowerShell", "tool_input": {"command": "Get-Process"}}`))
	if !ok || call.Command != "Get-Process" || !call.PowerShell {
		t.Fatal(call, ok)
	}
	call, ok = Claude{}.CommandCall([]byte(`{"tool_name": "Bash", "tool_input": {"command": "ls"}}`))
	if !ok || call.Command != "ls" || call.PowerShell {
		t.Fatal(call, ok)
	}
	for _, in := range []string{"", "{not json", `{"tool_name": "Bash", "tool_input": {}}`} {
		if _, ok := (Claude{}).CommandCall([]byte(in)); ok {
			t.Fatal(in)
		}
	}
}

func TestADenialIsAPreToolUseDecisionWithTheReasonUnescaped(t *testing.T) {
	out := Claude{}.Deny("Never <this> & that.")
	var d map[string]map[string]string
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(out, err)
	}
	h := d["hookSpecificOutput"]
	if h["hookEventName"] != "PreToolUse" || h["permissionDecision"] != "deny" || h["permissionDecisionReason"] != "Never <this> & that." {
		t.Fatal(out)
	}
	if !strings.Contains(out, "<this> & that") || !strings.HasSuffix(out, "\n") {
		t.Fatal(out)
	}
}

func TestASessionMayWriteItsProjectsFolderAndThePlansUnderClaude(t *testing.T) {
	got := Claude{}.WriteDirs("/home/u", "/home/u/dev/acme/lead")
	if strings.Join(got, " ") != "/home/u/.claude/projects/-home-u-dev-acme-lead /home/u/.claude/plans" {
		t.Fatal(got)
	}
}

func TestTheLinkIsTheRemoteControlSessionInTheSessionsOwnFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "42.json"), []byte(`{"bridgeSessionId": "session_abc"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "43.json"), []byte(`{}`), 0o644)
	if got := (Claude{}).Link(home, 42); got != "https://claude.ai/code/session_abc" {
		t.Fatal(got)
	}
	for _, pid := range []int{0, 43, 44} {
		if got := (Claude{}).Link(home, pid); got != "" {
			t.Fatal(pid, got)
		}
	}
}

func TestTrustIsKeptInClaudeJSONBesideItsOtherKeys(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, ".claude.json")
	os.WriteFile(file, []byte(`{"numStartups": 3, "projects": {"/other": {"x": 1}}}`), 0o600)
	if (Claude{}).Trusted(home, "/dev/a") {
		t.Fatal("trusted before")
	}
	if err := (Claude{}).Trust(home, "/dev/a"); err != nil {
		t.Fatal(err)
	}
	if !(Claude{}).Trusted(home, "/dev/a") {
		t.Fatal("not trusted after")
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), `"numStartups": 3`) || !strings.Contains(string(data), `"/other"`) {
		t.Fatal(string(data))
	}
}
