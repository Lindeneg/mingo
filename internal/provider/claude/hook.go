package claude

import (
	"bytes"
	"encoding/json"
	"path/filepath"

	"github.com/Lindeneg/mingo/internal/provider"
)

// input it cannot read writes nothing, since Claude Code owns the format
func (Claude) WriteCall(input []byte) (provider.WriteCall, bool) {
	var in struct {
		Cwd       string `json:"cwd"`
		ToolInput struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		} `json:"tool_input"`
	}
	if json.Unmarshal(input, &in) != nil {
		return provider.WriteCall{}, false
	}
	target := in.ToolInput.FilePath
	if target == "" {
		target = in.ToolInput.NotebookPath
	}
	if target == "" {
		return provider.WriteCall{}, false
	}
	return provider.WriteCall{Paths: []string{target}, Cwd: in.Cwd}, true
}

func (Claude) CommandCall(input []byte) (provider.CommandCall, bool) {
	var in, toolInput map[string]json.RawMessage
	if json.Unmarshal(input, &in) != nil {
		return provider.CommandCall{}, false
	}
	json.Unmarshal(in["tool_input"], &toolInput)
	var command, toolName string
	if json.Unmarshal(toolInput["command"], &command) != nil || command == "" {
		return provider.CommandCall{}, false
	}
	json.Unmarshal(in["tool_name"], &toolName)
	return provider.CommandCall{Command: command, PowerShell: toolName == "PowerShell"}, true
}

type decision struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func (Claude) Deny(reason string) string {
	var d decision
	d.HookSpecificOutput.HookEventName = "PreToolUse"
	d.HookSpecificOutput.PermissionDecision = "deny"
	d.HookSpecificOutput.PermissionDecisionReason = reason
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(d)
	return b.String()
}

func (Claude) ProjectVar() string { return "CLAUDE_PROJECT_DIR" }

func (c Claude) WriteDirs(home, root string) []string {
	return []string{filepath.Dir(c.MemoryDir(home, root)), filepath.Join(home, ".claude", "plans")}
}

func (Claude) WriteDirsShown() string {
	return "this project's memory and plan folders under ~/.claude"
}
