package kit

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lindeneg/mingo/internal/provider"
)

// the agent files are the instructions file, the files it imports and what is under the CLI's folder
func agentFile(agent provider.Kit, mods []Module, path string) bool {
	if path == agent.Instructions || strings.HasPrefix(path, agent.Dir+"/") {
		return true
	}
	for _, m := range mods {
		if contains(m.Imports, path) {
			return true
		}
	}
	return false
}

// the files for the repository, and with agent_files local the ones for every clone,
// the instructions file first, the project's own text with mingo's lines below it
func (d Deps) split(mods []Module, files []File) ([]File, []File, error) {
	if !d.Project.AgentLocal() {
		return files, nil, nil
	}
	agent := d.Project.Provider().Kit()
	var repo, clone []File
	for _, f := range files {
		if agentFile(agent, mods, f.Path) {
			clone = append(clone, f)
		} else {
			repo = append(repo, f)
		}
	}
	own, err := os.ReadFile(d.Project.InstructionsFile())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	text := string(own)
	var missing []string
	lines, _ := mingoLines(agent, mods)
	for _, line := range lines {
		if !hasLine(text, line) {
			missing = append(missing, line)
		}
	}
	if len(missing) > 0 {
		if text != "" {
			text = strings.TrimSuffix(text, "\n") + "\n\n"
		}
		text += strings.Join(missing, "\n") + "\n"
	}
	if text != "" {
		clone = append([]File{{Path: agent.Instructions, Module: "sessions", Text: text}}, clone...)
	}
	return repo, clone, nil
}

// every agent file that mingo writes into a clone
func agentPaths(prov provider.Provider, files []File) []string {
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if a, ok := prov.(provider.Attribution); ok {
		paths = append(paths, a.AttributionFile())
	}
	return paths
}

func excludes(agent provider.Kit, mods []Module) []string {
	lines := []string{"/" + agent.Instructions}
	for _, m := range mods {
		for _, imp := range m.Imports {
			lines = append(lines, "/"+imp)
		}
	}
	return append(lines, "/"+agent.Dir+"/")
}

func planAgent(prov provider.Provider, files []File, read func(path string) ([]byte, bool), lock *Lock) ([]Step, error) {
	settings, err := PlanSettings(prov, read)
	return append(Plan(files, read, lock), settings...), err
}

type clone struct {
	role    string
	dir     string
	git     string
	exclude string
}

// a folder inside another repository is no clone, though git answers for that repository there
func (d Deps) clone(role string) (clone, error) {
	p := d.Project
	dir := p.CloneDir(role)
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return clone{}, fmt.Errorf("%s has none at %s, run mingo setup %s first", role, dir, p.Name)
	}
	if err != nil {
		return clone{}, err
	}
	if !info.IsDir() {
		return clone{}, fmt.Errorf("%s's clone %s is a file, move it aside and run mingo setup %s", role, dir, p.Name)
	}
	// git reads a worktree's exclude file from the folder of the repository it belongs to
	out, err := d.git(dir, "rev-parse", "--show-toplevel", "--absolute-git-dir", "--path-format=absolute", "--git-path", "info/exclude")
	var ge *gitError
	if errors.As(err, &ge) {
		if _, serr := os.Lstat(filepath.Join(dir, ".git")); errors.Is(serr, fs.ErrNotExist) {
			return clone{}, fmt.Errorf("%s's folder %s is no git clone, move it aside and run mingo setup %s\n%v", role, dir, p.Name, err)
		}
		return clone{}, fmt.Errorf("git cannot read %s's clone %s\n%v", role, dir, err)
	}
	if err != nil {
		return clone{}, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		return clone{}, fmt.Errorf("git rev-parse answered %q in %s, mingo needs git 2.31 or newer", out, dir)
	}
	real := func(path string) string {
		path = filepath.Clean(filepath.FromSlash(path))
		if r, err := filepath.EvalSymlinks(path); err == nil {
			return r
		}
		return path
	}
	if real(lines[0]) != real(dir) {
		return clone{}, fmt.Errorf("%s's folder %s is inside the repository %s, not a clone of its own, move it aside and run mingo setup %s", role, dir, lines[0], p.Name)
	}
	return clone{role: role, dir: dir, git: filepath.FromSlash(lines[1]), exclude: filepath.FromSlash(lines[2])}, nil
}

func (d Deps) clones() ([]clone, error) {
	p := d.Project
	if !p.AgentLocal() {
		return nil, nil
	}
	var out []clone
	for _, role := range p.RoleNames() {
		c, err := d.clone(role)
		if err != nil {
			return nil, fmt.Errorf("agent_files is local, so mingo writes the agent files into every clone, and %v", err)
		}
		out = append(out, c)
	}
	return out, nil
}

func (c clone) read(path string) ([]byte, bool) {
	data, err := os.ReadFile(filepath.Join(c.dir, filepath.FromSlash(path)))
	return data, err == nil
}

// the lock of the agent files is inside .git, so it is never committed either
func (c clone) lockFile() string {
	return filepath.Join(c.git, "mingo", "kit.lock")
}

func (c clone) lock() (*Lock, error) {
	data, _ := os.ReadFile(c.lockFile())
	return parseLock(data, c.lockFile())
}

// the lines of .git/info/exclude that the agent files still need, and what the file has
func (c clone) unexcluded(agent provider.Kit, mods []Module) ([]string, string, error) {
	data, err := os.ReadFile(c.exclude)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, "", err
	}
	var missing []string
	for _, line := range excludes(agent, mods) {
		if !hasLine(string(data), line) {
			missing = append(missing, line)
		}
	}
	return missing, string(data), nil
}

// removes a file of a clone and the folders that leaves empty
func removeFile(root, path string) error {
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for dir := filepath.Dir(full); strings.HasPrefix(dir, root+string(filepath.Separator)); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

type clonePlan struct {
	clone clone
	lock  *Lock
	steps []Step
}

// what every clone gets, read before anything is written anywhere, so a clone that refuses changes nothing
func (d Deps) planClones(files []File, clones []clone) ([]clonePlan, error) {
	var plans []clonePlan
	for _, c := range clones {
		lock, err := c.lock()
		if err != nil {
			return nil, err
		}
		steps, err := planAgent(d.Project.Provider(), files, c.read, lock)
		if err != nil {
			return nil, fmt.Errorf("in %s's clone, %v", c.role, err)
		}
		plans = append(plans, clonePlan{clone: c, lock: lock, steps: steps})
	}
	return plans, nil
}

// writes the agent files into every clone, after .git/info/exclude keeps them out of git, and says whether anything changed
func (d Deps) applyClones(mods []Module, plans []clonePlan, scratch string) (bool, error) {
	changed := false
	for _, plan := range plans {
		c, lock, steps := plan.clone, plan.lock, plan.steps
		missing, text, err := c.unexcluded(d.Project.Provider().Kit(), mods)
		if err != nil {
			return false, err
		}
		if len(missing) > 0 {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			if err := os.MkdirAll(filepath.Dir(c.exclude), 0o755); err != nil {
				return false, err
			}
			if err := os.WriteFile(c.exclude, []byte(text+strings.Join(missing, "\n")+"\n"), 0o644); err != nil {
				return false, err
			}
			fmt.Fprintf(d.Out, "%s: adds %s to .git/info/exclude\n", c.role, strings.Join(missing, " "))
			changed = true
		}
		written, removed, err := d.land(c.dir, c.role+": ", steps, scratch, func(path string) error {
			return removeFile(c.dir, path)
		})
		if err != nil {
			return false, err
		}
		next, lockChanged := NextLock(steps, lock, d.Version)
		if lockChanged {
			if err := os.MkdirAll(filepath.Dir(c.lockFile()), 0o755); err != nil {
				return false, err
			}
			if err := os.WriteFile(c.lockFile(), next.Bytes(), 0o644); err != nil {
				return false, err
			}
		}
		changed = changed || len(written) > 0 || len(removed) > 0 || lockChanged
	}
	return changed, nil
}
