package claude

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Lindeneg/mingo/internal/install"
	"github.com/Lindeneg/mingo/internal/ojson"
)

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

var errNotObject = errors.New("not a JSON object")

func shown(v any, ok bool) string {
	switch t := v.(type) {
	case nil:
		if !ok {
			return "unset"
		}
		return "null"
	case string:
		return strconv.Quote(t)
	case *ojson.Object:
		return "an object"
	case []any:
		return "a list"
	}
	return fmt.Sprint(v)
}

func settingsFile(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

// keeps every key already there, adds what mingo needs only once, and says what changed, each key
// with its old and new value
func mergeSettings(data []byte, home string) ([]byte, []byte, []string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	v, err := ojson.Parse(data)
	if err != nil {
		return nil, nil, nil, err
	}
	root, ok := v.(*ojson.Object)
	if !ok {
		return nil, nil, nil, errNotObject
	}
	if err := checkTypes(root); err != nil {
		return nil, nil, nil, err
	}
	before, _ := ojson.Marshal(root)
	var changes []string

	for _, k := range []struct {
		key  string
		want any
	}{{"crossSessionInbound", "accept"}, {"isolatePeerMachines", true}} {
		old, had := root.Get(k.key)
		if had && old == k.want {
			continue
		}
		changes = append(changes, fmt.Sprintf("%s was %s, now %s", k.key, shown(old, had), shown(k.want, true)))
		root.Set(k.key, k.want)
	}

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
			changes = append(changes, fmt.Sprintf("permissions.deny gained %q", rule))
		}
	}
	perms.Set("deny", list)

	path := install.BinPath(home)
	bin := install.ShellWord(path)
	guards := []struct{ matcher, kind, status, command string }{
		{matcher: "Edit|Write|NotebookEdit", kind: "write", status: "Checking the path is inside this project"},
		{matcher: "Bash|PowerShell", kind: "bash", status: "Checking the command ends no process by its name"},
	}
	// an older mingo wrote the path unquoted, which sh splits at a space in the home folder, so that
	// guard fails on every tool call until it is quoted, and wrote the guard without its provider
	unquoted := map[string]string{}
	for i := range guards {
		g := &guards[i]
		g.command = bin + " guard claude " + g.kind
		for _, old := range []string{path + " guard claude " + g.kind, path + " guard " + g.kind, bin + " guard " + g.kind} {
			if old != g.command {
				unquoted[old] = g.command
			}
		}
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
	for i := 0; i < len(entries); i++ {
		obj, ok := entries[i].(*ojson.Object)
		if !ok {
			continue
		}
		inner, _ := obj.Get("hooks")
		list, ok := inner.([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, h := range list {
			s := ""
			hobj, isObject := h.(*ojson.Object)
			if isObject {
				cmd, _ := hobj.Get("command")
				s, _ = cmd.(string)
			}
			quoted, old := unquoted[s]
			switch {
			case !old:
				kept = append(kept, h)
			case has[quoted]:
				changes = append(changes, fmt.Sprintf("hooks.PreToolUse lost %q, the same guard as %q", s, quoted))
			default:
				hobj.Set("command", quoted)
				has[quoted] = true
				kept = append(kept, h)
				changes = append(changes, fmt.Sprintf("hooks.PreToolUse had %q, now %q", s, quoted))
			}
		}
		switch {
		case len(kept) == len(list):
		case len(kept) == 0:
			entries = append(entries[:i], entries[i+1:]...)
			i--
		default:
			obj.Set("hooks", kept)
		}
	}
	for _, g := range guards {
		if !has[g.command] {
			entries = append(entries, hook(g.matcher, g.command, g.status))
			changes = append(changes, fmt.Sprintf("hooks.PreToolUse gained %q", g.command))
		}
	}
	hooks.Set("PreToolUse", entries)

	after, err := ojson.Marshal(root)
	return before, after, changes, err
}

func (Claude) Install(home string, out io.Writer) error {
	file := settingsFile(home)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	before, after, changes, err := mergeSettings(data, home)
	var wrong wrongType
	switch {
	case errors.As(err, &wrong):
		return fmt.Errorf("%s has a %s key that is not a JSON %s, left as it is", file, wrong.key, wrong.want)
	case errors.Is(err, errNotObject):
		return fmt.Errorf("%s is not a JSON object, left as it is", file)
	case err != nil:
		return install.NotJSON(file, data, err)
	}
	if len(data) > 0 && bytes.Equal(before, after) {
		fmt.Fprintf(out, "claude settings: %s unchanged\n", file)
		return nil
	}
	// a settings file kept elsewhere through a link, such as in dotfiles, is written where it is
	target, err := install.LinkTarget(file)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	if err := install.WriteAtomic(target, after, mode); err != nil {
		return err
	}
	if target != file {
		fmt.Fprintf(out, "claude settings: %s, through the link to %s\n", file, target)
	} else {
		fmt.Fprintf(out, "claude settings: %s\n", file)
	}
	for _, c := range changes {
		fmt.Fprintf(out, "  %s\n", c)
	}
	return nil
}

func (Claude) Installed(home string) (bool, string) {
	file := settingsFile(home)
	data, err := os.ReadFile(file)
	ok := false
	if err == nil {
		before, after, _, err := mergeSettings(data, home)
		ok = err == nil && bytes.Equal(before, after)
	}
	return ok, "the write and kill guards in " + file
}

func claudeJSON(home string) string {
	return filepath.Join(home, ".claude.json")
}

// running claude processes rewrite this file, so it is written whole and read back
func (c Claude) Trust(home, dir string) error {
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
		return install.NotJSON(file, data, err)
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
	if err := install.WriteAtomic(file, out, mode); err != nil {
		return err
	}
	if !c.Trusted(home, dir) {
		return fmt.Errorf("%s did not keep the trust for %s, a running Claude Code may have rewritten it", file, dir)
	}
	return nil
}

func (Claude) Trusted(home, dir string) bool {
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
