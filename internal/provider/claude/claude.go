// Package claude is Claude Code as a provider: how its sessions start and end, its memory, settings, trust and hooks.
package claude

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Lindeneg/mingo/internal/provider"
)

type Claude struct{}

func init() {
	provider.Register(Claude{})
}

func (Claude) Name() string       { return "claude" }
func (Claude) Title() string      { return "Claude Code" }
func (Claude) Site() string       { return "https://code.claude.com/docs/en/setup" }
func (Claude) Command() string    { return "claude" }
func (Claude) CommandEnv() string { return "MINGO_CLAUDE" }

func (Claude) DefaultArgs() []string {
	return []string{"--permission-mode", "auto", "--remote-control"}
}

func (Claude) Args(s provider.Session) []string {
	args := []string{s.Opener, "--name", s.Name}
	if s.Model != "" {
		args = append(args, "--model", s.Model)
	}
	if s.Effort != "" {
		args = append(args, "--effort", s.Effort)
	}
	return append(args, s.Args...)
}

func (Claude) ExitKeys() []string { return []string{"/exit", "Enter"} }
func (Claude) ExitShown() string  { return "/exit" }

// the folder name Claude Code gives a path under ~/.claude/projects
func Slug(path string) string {
	var b strings.Builder
	for _, c := range path {
		if c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (Claude) MemoryDir(home, clone string) string {
	return filepath.Join(home, ".claude", "projects", Slug(clone), "memory")
}

func (Claude) MemoryIndex() string { return "MEMORY.md" }

func (Claude) Login(command string, out io.Writer) error {
	cmd := exec.Command(command, "auth", "status", "--text")
	cmd.Stdout = out
	return cmd.Run()
}

func (Claude) Live(command string) []provider.Live {
	out, err := exec.Command(command, "agents", "--json").Output()
	if err != nil {
		return nil
	}
	var rows []struct {
		Name      string `json:"name"`
		Pid       int    `json:"pid"`
		Status    string `json:"status"`
		StartedAt int64  `json:"startedAt"`
	}
	if json.Unmarshal(out, &rows) != nil {
		return nil
	}
	var live []provider.Live
	for _, r := range rows {
		l := provider.Live{Name: r.Name, Pid: r.Pid, State: r.Status}
		if r.StartedAt > 0 {
			l.Started = time.UnixMilli(r.StartedAt)
		}
		live = append(live, l)
	}
	return live
}

// the Remote Control id is in the session's own file, the TUI's scrollback does not keep it
func (Claude) Link(home string, pid int) string {
	if pid <= 0 {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return ""
	}
	var s struct {
		BridgeSessionID string `json:"bridgeSessionId"`
	}
	if json.Unmarshal(data, &s) != nil || s.BridgeSessionID == "" {
		return ""
	}
	return "https://claude.ai/code/" + s.BridgeSessionID
}
