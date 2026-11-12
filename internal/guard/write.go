package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Lindeneg/mingo/internal/config"
)

type WriteEnv struct {
	ProjectDir    string
	TempDir       string
	SystemTempDir string
	Home          string
	StateDir      string
}

func WriteEnvFromOS() WriteEnv {
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	env := WriteEnv{ProjectDir: os.Getenv("CLAUDE_PROJECT_DIR"), TempDir: tmp, Home: config.Home(), StateDir: config.StateDir()}
	// macOS gives each user a TMPDIR of their own, while Claude Code keeps its scratchpad in /tmp
	if runtime.GOOS == "darwin" {
		env.SystemTempDir = "/tmp"
	}
	return env
}

func clean(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

// the path with its existing part's symlinks resolved, so a link inside the project cannot lead out
func resolve(path string) string {
	return resolveLinks(path, 0)
}

// a link whose target does not exist yet is judged by that target, since a write through it creates it
func resolveLinks(path string, hops int) string {
	for dir := path; ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			rest, err := filepath.Rel(dir, path)
			if err != nil {
				return path
			}
			return filepath.Join(real, rest)
		}
		if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 && hops < 40 {
			target, err := os.Readlink(dir)
			rest, relErr := filepath.Rel(dir, path)
			if err != nil || relErr != nil {
				return path
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(resolveLinks(filepath.Dir(dir), hops+1), target)
			}
			return resolveLinks(filepath.Join(target, rest), hops+1)
		}
		if dir == filepath.Dir(dir) {
			return path
		}
	}
}

func within(file, dir string) bool {
	return file == dir || strings.HasPrefix(file, dir+string(filepath.Separator))
}

// input it cannot read is allowed, since Claude Code owns the format
func Write(input []byte, env WriteEnv) string {
	var in struct {
		Cwd       string `json:"cwd"`
		ToolInput struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		} `json:"tool_input"`
	}
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	target := in.ToolInput.FilePath
	if target == "" {
		target = in.ToolInput.NotebookPath
	}
	if target == "" {
		return ""
	}
	if env.ProjectDir == "" {
		return deny("The write guard cannot tell your project from another, CLAUDE_PROJECT_DIR is not set. Start the session from its project folder.")
	}
	if !filepath.IsAbs(target) {
		base := in.Cwd
		if base == "" {
			base = env.ProjectDir
		}
		target = base + "/" + target
	}
	file := resolve(clean(target))
	root := clean(env.ProjectDir)
	tmp := clean(env.TempDir)
	// the state folder sits in the temp folder, and its markers and slots are every project's
	if env.StateDir != "" && within(file, resolve(clean(env.StateDir))) && !within(file, resolve(root)) {
		return deny("Write only inside your own project. " + target + " is in mingo's state folder, which only mingo writes.")
	}
	claudeHome := filepath.Join(env.Home, ".claude")
	allowedDirs := []string{
		root,
		tmp,
		filepath.Join(claudeHome, "projects", config.Slug(root)),
		filepath.Join(claudeHome, "projects", config.Slug(resolve(root))),
		filepath.Join(claudeHome, "plans"),
	}
	if env.SystemTempDir != "" {
		allowedDirs = append(allowedDirs, clean(env.SystemTempDir))
	}
	for _, allowed := range allowedDirs {
		if within(file, resolve(allowed)) {
			return ""
		}
	}
	return deny("Write only inside your own project. " + target + " is outside " + root + ", the temp folder and this project's memory and plan folders under ~/.claude.")
}
