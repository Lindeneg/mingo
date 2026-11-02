// Package install sets up a machine for mingo: the command, tmux's config, Claude Code's settings
// and folder trust.
package install

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/ojson"
)

func BinPath(home string) string {
	return filepath.Join(home, ".local", "bin", "mingo")
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

// written beside the target and renamed over it, so a link there is replaced and never written through
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".mingo-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func Bin(home string, out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	target := BinPath(home)
	if !sameFile(exe, target) {
		data, err := os.ReadFile(exe)
		if err != nil {
			return err
		}
		if err := writeAtomic(target, data, 0o755); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "command: %s\n", target)
	return nil
}

const tmuxConf = `set -g mouse on
set -g focus-events on
set -g history-limit 50000
`

func TmuxConf(home string, out io.Writer) error {
	path := filepath.Join(home, ".tmux.conf")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.WriteFile(path, []byte(tmuxConf), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(out, "tmux: wrote ~/.tmux.conf")
	return nil
}

func hook(matcher, command, status string) *ojson.Object {
	h := ojson.NewObject()
	h.Set("type", "command")
	h.Set("command", command)
	h.Set("timeout", 10)
	h.Set("statusMessage", status)
	entry := ojson.NewObject()
	entry.Set("matcher", matcher)
	entry.Set("hooks", []any{h})
	return entry
}

type wrongType struct{ key, want string }

func (e wrongType) Error() string {
	return e.key + " is not a JSON " + e.want
}

// a key of another type is the user's, so mingo leaves the file rather than replace it
func checkTypes(root *ojson.Object) error {
	for _, k := range []struct{ parent, key string }{{"permissions", "deny"}, {"hooks", "PreToolUse"}} {
		v, ok := root.Get(k.parent)
		if !ok {
			continue
		}
		o, ok := v.(*ojson.Object)
		if !ok {
			return wrongType{k.parent, "object"}
		}
		if v, ok := o.Get(k.key); ok {
			if _, ok := v.([]any); !ok {
				return wrongType{k.parent + "." + k.key, "list"}
			}
		}
	}
	return nil
}

// keeps every key already there and adds what mingo needs only once
func MergeSettings(data []byte, home string) ([]byte, []byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	v, err := ojson.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	root, ok := v.(*ojson.Object)
	if !ok {
		return nil, nil, errors.New("not a JSON object")
	}
	if err := checkTypes(root); err != nil {
		return nil, nil, err
	}
	before, _ := ojson.Marshal(root)

	root.Set("crossSessionInbound", "accept")
	root.Set("isolatePeerMachines", true)

	perms := root.Child("permissions")
	deny, _ := perms.Get("deny")
	list, _ := deny.([]any)
	if list == nil {
		list = []any{}
	}
	for _, rule := range []string{"Bash(pkill *)", "Bash(killall *)"} {
		found := false
		for _, have := range list {
			if have == rule {
				found = true
			}
		}
		if !found {
			list = append(list, rule)
		}
	}
	perms.Set("deny", list)

	bin := BinPath(home)
	guards := []struct{ matcher, command, status string }{
		{"Edit|Write|NotebookEdit", bin + " guard write", "Checking the path is inside this project"},
		{"Bash|PowerShell", bin + " guard bash", "Checking the command ends no process by its name"},
	}
	hooks := root.Child("hooks")
	pre, _ := hooks.Get("PreToolUse")
	entries, _ := pre.([]any)
	if entries == nil {
		entries = []any{}
	}
	has := map[string]bool{}
	for _, e := range entries {
		obj, ok := e.(*ojson.Object)
		if !ok {
			continue
		}
		inner, _ := obj.Get("hooks")
		list, _ := inner.([]any)
		for _, h := range list {
			hobj, ok := h.(*ojson.Object)
			if !ok {
				continue
			}
			cmd, _ := hobj.Get("command")
			s, _ := cmd.(string)
			has[s] = true
		}
	}
	for _, g := range guards {
		if !has[g.command] {
			entries = append(entries, hook(g.matcher, g.command, g.status))
		}
	}
	hooks.Set("PreToolUse", entries)

	after, err := ojson.Marshal(root)
	return before, after, err
}

func Settings(home string, out io.Writer) error {
	file := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	before, after, err := MergeSettings(data, home)
	var wrong wrongType
	if errors.As(err, &wrong) {
		return fmt.Errorf("%s has a %s key that is not a JSON %s, left as it is", file, wrong.key, wrong.want)
	}
	if err != nil {
		return fmt.Errorf("%s is not valid JSON, left as it is", file)
	}
	if len(data) > 0 && bytes.Equal(before, after) {
		fmt.Fprintf(out, "claude settings: %s unchanged\n", file)
		return nil
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(file); err == nil {
		mode = info.Mode().Perm()
	}
	if err := writeAtomic(file, after, mode); err != nil {
		return err
	}
	fmt.Fprintf(out, "claude settings: %s\n", file)
	return nil
}

func Run(out io.Writer) error {
	if _, err := exec.LookPath("tmux"); err != nil {
		if runtime.GOOS == "darwin" {
			return errors.New("tmux is missing, brew install tmux")
		}
		return errors.New("tmux is missing, see provision/provision.sh")
	}
	home := config.Home()
	for _, step := range []func(string, io.Writer) error{Bin, TmuxConf, Settings} {
		if err := step(home, out); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "mingo help lists the commands")
	return nil
}

func claudeJSON(home string) string {
	return filepath.Join(home, ".claude.json")
}

// running claude processes rewrite this file, so it is written whole and read back
func Trust(home, dir string) error {
	file := claudeJSON(home)
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	v, err := ojson.Parse(data)
	if err != nil {
		return fmt.Errorf("%s is not valid JSON, left as it is", file)
	}
	root, ok := v.(*ojson.Object)
	if !ok {
		return fmt.Errorf("%s is not a JSON object, left as it is", file)
	}
	root.Child("projects").Child(dir).Set("hasTrustDialogAccepted", true)
	out, err := ojson.Marshal(root)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(file); err == nil {
		mode = info.Mode().Perm()
	}
	if err := writeAtomic(file, out, mode); err != nil {
		return err
	}
	if !Trusted(home, dir) {
		return fmt.Errorf("%s did not keep the trust for %s, a running Claude Code may have rewritten it", file, dir)
	}
	return nil
}

func Trusted(home, dir string) bool {
	data, err := os.ReadFile(claudeJSON(home))
	if err != nil {
		return false
	}
	v, err := ojson.Parse(data)
	if err != nil {
		return false
	}
	root, ok := v.(*ojson.Object)
	if !ok {
		return false
	}
	projects, ok := root.Vals("projects")
	if !ok {
		return false
	}
	entry, ok := projects.Vals(dir)
	if !ok {
		return false
	}
	trusted, _ := entry.Get("hasTrustDialogAccepted")
	return trusted == true
}
