package kit

import (
	"fmt"
	"strings"

	"github.com/Lindeneg/mingo/internal/provider"
)

type Action string

const (
	Add    Action = "add"
	Update Action = "update"
	Same   Action = "same"
	Edited Action = "edited"
	Kept   Action = "kept"
	Remove Action = "remove"
	Leave  Action = "leave"
)

type Step struct {
	File   File
	Action Action
	Old    []byte
	Owned  bool
	What   string
	Own    bool
}

// the instructions file is the project's own, so mingo only adds the imports and the lines its modules need to it
func mingoLines(agent provider.Kit, mods []Module) ([]string, map[string]string) {
	var wanted []string
	module := map[string]string{}
	for _, m := range mods {
		for _, imp := range m.Imports {
			line := fmt.Sprintf(agent.Import, imp)
			wanted = append(wanted, line)
			module[line] = m.Name
		}
		for _, line := range m.Lines {
			wanted = append(wanted, line)
			module[line] = m.Name
		}
	}
	return wanted, module
}

func PlanImports(agent provider.Kit, mods []Module, read func(path string) ([]byte, bool)) []Step {
	wanted, module := mingoLines(agent, mods)
	if len(wanted) == 0 {
		return nil
	}
	old, ok := read(agent.Instructions)
	text := string(old)
	var missing []string
	by := ""
	for _, line := range wanted {
		if !hasLine(text, line) {
			missing = append(missing, line)
			by = module[line]
		}
	}
	step := Step{File: File{Path: agent.Instructions, Module: by, Text: text}, Action: Same, Old: old, Owned: true, What: "mingo's lines"}
	if len(missing) == 0 {
		return []Step{step}
	}
	next := strings.Join(missing, "\n") + "\n"
	step.Action = Add
	if ok {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		next = text + "\n" + next
		step.Action = Update
	}
	step.File.Text = next
	return []Step{step}
}

// a file still as mingo rendered it is replaced, one the project changed is left alone,
// and shown only when what mingo renders for it changed too
func Plan(files []File, read func(path string) ([]byte, bool), lock *Lock) []Step {
	var steps []Step
	for _, f := range files {
		old, ok := read(f.Path)
		step := Step{File: f, Old: old}
		locked, isLocked := lock.Get(f.Path)
		switch {
		case !ok:
			step.Action = Add
		case string(old) == f.Text:
			step.Action = Same
		case isLocked && Sum(old) == locked.Sum:
			step.Action = Update
		case isLocked && locked.Sum == f.Sum():
			step.Action = Kept
		default:
			step.Action = Edited
			step.Own = !isLocked
		}
		steps = append(steps, step)
	}
	rendered := map[string]bool{}
	for _, f := range files {
		rendered[f.Path] = true
	}
	for _, l := range lock.Files {
		if rendered[l.Path] {
			continue
		}
		old, ok := read(l.Path)
		step := Step{File: File{Path: l.Path, Module: l.Module}, Old: old, Action: Leave}
		if ok && Sum(old) == l.Sum {
			step.Action = Remove
		}
		steps = append(steps, step)
	}
	return steps
}

// the lock after the steps, and whether it changed
func NextLock(steps []Step, lock *Lock, version string) (*Lock, bool) {
	next := &Lock{}
	changed := false
	gone := map[string]bool{}
	for _, s := range steps {
		if s.Action == Remove || s.Action == Leave {
			gone[s.File.Path] = true
			changed = true
		}
	}
	for _, l := range lock.Files {
		if !gone[l.Path] {
			next.Files = append(next.Files, l)
		}
	}
	for _, s := range steps {
		if s.Owned || (s.Action != Add && s.Action != Update && s.Action != Same) {
			continue
		}
		have, ok := next.Get(s.File.Path)
		if s.Action == Same && ok && have.Sum == s.File.Sum() {
			continue
		}
		next.Set(Locked{Path: s.File.Path, Module: s.File.Module, Mingo: version, Sum: s.File.Sum()})
		changed = true
	}
	return next, changed
}
