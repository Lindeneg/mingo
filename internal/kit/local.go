package kit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Claude's files are CLAUDE.md, the files it imports and what is under .claude
func claudeFile(mods []Module, path string) bool {
	if path == Instructions || strings.HasPrefix(path, ".claude/") {
		return true
	}
	for _, m := range mods {
		if contains(m.Imports, path) {
			return true
		}
	}
	return false
}

// the files for the repository, and with claude_files local the ones for every clone
func (d Deps) split(mods []Module, files []File) (repo, clone []File) {
	if !d.Project.ClaudeLocal() {
		return files, nil
	}
	for _, f := range files {
		if claudeFile(mods, f.Path) {
			clone = append(clone, f)
		} else {
			repo = append(repo, f)
		}
	}
	return repo, clone
}

// every path of Claude's that mingo writes
func claudePaths(mods []Module, files []File) []string {
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	for _, m := range mods {
		if len(m.Imports)+len(m.Lines) > 0 {
			paths = append(paths, Instructions)
			break
		}
	}
	return append(paths, Settings)
}

func excludes(mods []Module) []string {
	lines := []string{"/" + Instructions}
	for _, m := range mods {
		for _, imp := range m.Imports {
			lines = append(lines, "/"+imp)
		}
	}
	return append(lines, "/.claude/")
}

func planClaude(mods []Module, files []File, read func(path string) ([]byte, bool), lock *Lock) ([]Step, error) {
	steps := append(Plan(files, read, lock), PlanImports(mods, read)...)
	settings, err := PlanSettings(read)
	return append(steps, settings...), err
}

type clone struct {
	role string
	dir  string
	git  string
}

// a folder inside another repository is no clone, though git answers for that repository there
func (d Deps) clone(role string) (clone, error) {
	dir := d.Project.CloneDir(role)
	out, err := d.git(dir, "rev-parse", "--show-toplevel", "--absolute-git-dir")
	if err != nil {
		return clone{}, err
	}
	top, gitDir, _ := strings.Cut(strings.TrimSpace(out), "\n")
	real := func(path string) string {
		path = filepath.Clean(filepath.FromSlash(path))
		if r, err := filepath.EvalSymlinks(path); err == nil {
			return r
		}
		return path
	}
	if real(top) != real(dir) {
		return clone{}, fmt.Errorf("%s is inside %s, not a clone", dir, top)
	}
	return clone{role: role, dir: dir, git: filepath.FromSlash(gitDir)}, nil
}

func (d Deps) clones() ([]clone, error) {
	p := d.Project
	if !p.ClaudeLocal() {
		return nil, nil
	}
	var out []clone
	for _, role := range p.RoleNames() {
		c, err := d.clone(role)
		if err != nil {
			return nil, fmt.Errorf("claude_files is local, so mingo writes Claude's files into every clone, and %s has none at %s, run mingo setup %s first", role, p.CloneDir(role), p.Name)
		}
		out = append(out, c)
	}
	return out, nil
}

func (c clone) read(path string) ([]byte, bool) {
	data, err := os.ReadFile(filepath.Join(c.dir, filepath.FromSlash(path)))
	return data, err == nil
}

// the lock of Claude's files is inside .git, so it is never committed either
func (c clone) lockFile() string {
	return filepath.Join(c.git, "mingo", "kit.lock")
}

func (c clone) lock() (*Lock, error) {
	data, _ := os.ReadFile(c.lockFile())
	return parseLock(data, c.lockFile())
}

func (c clone) excludeFile() string {
	return filepath.Join(c.git, "info", "exclude")
}

// the lines of .git/info/exclude that Claude's files still need, and what the file has
func (c clone) unexcluded(mods []Module) ([]string, string, error) {
	data, err := os.ReadFile(c.excludeFile())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, "", err
	}
	var missing []string
	for _, line := range excludes(mods) {
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

// writes Claude's files into every clone, after .git/info/exclude keeps them out of git, and says whether anything changed
func (d Deps) applyClones(mods []Module, files []File, clones []clone, scratch string) (bool, error) {
	changed := false
	for _, c := range clones {
		missing, text, err := c.unexcluded(mods)
		if err != nil {
			return false, err
		}
		if len(missing) > 0 {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			if err := os.MkdirAll(filepath.Dir(c.excludeFile()), 0o755); err != nil {
				return false, err
			}
			if err := os.WriteFile(c.excludeFile(), []byte(text+strings.Join(missing, "\n")+"\n"), 0o644); err != nil {
				return false, err
			}
			fmt.Fprintf(d.Out, "%s: adds %s to .git/info/exclude\n", c.role, strings.Join(missing, " "))
			changed = true
		}
		lock, err := c.lock()
		if err != nil {
			return false, err
		}
		steps, err := planClaude(mods, files, c.read, lock)
		if err != nil {
			return false, fmt.Errorf("in %s's clone, %v", c.role, err)
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
