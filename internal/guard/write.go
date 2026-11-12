package guard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/provider"
)

type WriteEnv struct {
	ProjectDir    string
	TempDir       string
	SystemTempDir string
	Home          string
	StateDir      string
	// the variable that names the project folder, for the denial when it is not set
	ProjectVar string
	// the folders outside the project a session may write to
	Dirs func(home, root string) []string
	// those folders as a denial names them
	DirsShown string
}

func WriteEnvFor(g provider.Guarded) WriteEnv {
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	env := WriteEnv{ProjectDir: os.Getenv(g.ProjectVar()), TempDir: tmp, Home: config.Home(), StateDir: config.StateDir(),
		ProjectVar: g.ProjectVar(), Dirs: g.WriteDirs, DirsShown: g.WriteDirsShown()}
	// macOS gives each user a TMPDIR of their own, while an agent CLI may keep its scratch files in /tmp
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

// the reason the call is denied, empty when every path it writes is allowed
func Write(call provider.WriteCall, env WriteEnv) string {
	for _, path := range call.Paths {
		if reason := writeOne(path, call.Cwd, env); reason != "" {
			return reason
		}
	}
	return ""
}

func writeOne(target, cwd string, env WriteEnv) string {
	if target == "" {
		return ""
	}
	if env.ProjectDir == "" {
		return "The write guard cannot tell your project from another, " + env.ProjectVar + " is not set. Start the session from its project folder."
	}
	if !filepath.IsAbs(target) {
		base := cwd
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
		return "Write only inside your own project. " + target + " is in mingo's state folder, which only mingo writes."
	}
	allowedDirs := []string{root, tmp}
	if env.Dirs != nil {
		allowedDirs = append(allowedDirs, env.Dirs(env.Home, root)...)
		if real := resolve(root); real != root {
			allowedDirs = append(allowedDirs, env.Dirs(env.Home, real)...)
		}
	}
	if env.SystemTempDir != "" {
		allowedDirs = append(allowedDirs, clean(env.SystemTempDir))
	}
	for _, allowed := range allowedDirs {
		if within(file, resolve(allowed)) {
			return ""
		}
	}
	shown := "the temp folder"
	if env.DirsShown != "" {
		shown += " and " + env.DirsShown
	}
	return "Write only inside your own project. " + target + " is outside " + root + ", " + shown + "."
}
