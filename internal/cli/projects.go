package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Lindeneg/mingo"
	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/install"
	"github.com/Lindeneg/mingo/internal/kit"
	"github.com/Lindeneg/mingo/internal/sys"
)

func (e *env) project(action, name string) error {
	if name == "" {
		return badUsage("", "project")
	}
	switch action {
	case "add":
		if !config.IsName(name) {
			return errors.New("a project name is lowercase letters, digits and hyphens")
		}
		file := config.File(name)
		if _, err := os.Stat(file); err == nil {
			return fmt.Errorf("%s exists already", file)
		}
		template, err := mingo.Share.ReadFile("share/project.toml.example")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, []byte(strings.ReplaceAll(string(template), "{name}", name)), 0o644); err != nil {
			return err
		}
		fmt.Fprintln(e.out, file)
	case "show", "edit":
		found, ok := config.NameOf(name)
		if !ok {
			return fmt.Errorf("no project named %s", name)
		}
		file := config.File(found)
		if action == "show" {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			_, err = e.out.Write(data)
			return err
		}
		if err := edit(file); err != nil {
			return err
		}
		if _, err := config.Load(found); err != nil {
			return fmt.Errorf("%v, the file is kept as you left it, mingo project edit %s opens it again", err, found)
		}
	default:
		return badUsage("", "project")
	}
	return nil
}

func edit(file string) error {
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	if !installed(editor[0]) {
		return fmt.Errorf("the editor %s is not installed, set EDITOR to one that is", editor[0])
	}
	cmd := exec.Command(editor[0], append(editor[1:], file)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %v", strings.Join(cmd.Args, " "), err)
	}
	return nil
}

func (e *env) setDefault(args []string) error {
	if len(args) == 0 {
		data, err := os.ReadFile(config.DefaultFile())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		line, _, _ := strings.Cut(string(data), "\n")
		name := strings.TrimSpace(line)
		switch {
		case name == "":
			return errors.New("no default project is set, mingo default <project> sets one")
		case !config.Known(name):
			return fmt.Errorf("the default project is %s, which no longer exists, mingo default <project> sets another", name)
		}
		fmt.Fprintln(e.out, name)
		return nil
	}
	p, err := config.Find(args[0])
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.Dir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(config.DefaultFile(), []byte(p.Name+"\n"), 0o644)
}

func (e *env) path(spec string) error {
	p, role, err := config.ParseRoleTarget(spec)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.out, p.CloneDir(role))
	return nil
}

func (e *env) memory(spec string) error {
	p, role, err := config.ParseRoleTarget(spec)
	if err != nil {
		return err
	}
	dir := p.MemoryDir(role)
	for _, file := range []string{filepath.Join(dir, "handoff-"+role+".md"), filepath.Join(dir, "MEMORY.md")} {
		if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
			fmt.Fprintln(e.out, file)
			return nil
		}
	}
	fmt.Fprintln(e.out, dir)
	return nil
}

func (e *env) repo(spec string) error {
	p, _, err := config.ParseTarget(spec)
	if err != nil {
		return err
	}
	if p.Repo == "" {
		return fmt.Errorf("%s names no repo", p.Name)
	}
	fmt.Fprintln(e.out, p.Repo)
	return nil
}

func quiet(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// what the command printed is already on the screen, so its error says only which command failed
func loud(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %v", strings.Join(cmd.Args, " "), err)
	}
	return nil
}

func (e *env) setup(spec string) error {
	p, _, err := config.ParseTarget(spec)
	if err != nil {
		return err
	}
	for _, r := range p.RoleNames() {
		dir := p.CloneDir(r)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			fmt.Fprintf(e.out, "exists: %s\n", dir)
		} else {
			if p.Repo == "" {
				return fmt.Errorf("%s names no repo to clone into %s", p.Name, dir)
			}
			if !installed("gh") {
				return errors.New("gh is not installed, see https://cli.github.com")
			}
			if !installed("git") {
				return errors.New("git is not installed, see https://git-scm.com/downloads")
			}
			if quiet("gh", "auth", "status") != nil {
				return errors.New("gh is not logged in, run gh auth login")
			}
			if quiet("git", "config", "--global", "user.name") != nil || quiet("git", "config", "--global", "user.email") != nil {
				return errors.New("set the git identity first with git config --global user.name and user.email")
			}
			if err := loud("gh", "auth", "setup-git"); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
				return err
			}
			if err := loud("gh", "repo", "clone", p.Repo, dir); err != nil {
				return err
			}
		}
		// trust comes first, so a clone whose ports fail is still trusted, and both repair a clone made before
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if !install.Trusted(config.Home(), abs) {
			if err := install.Trust(config.Home(), abs); err != nil {
				return err
			}
		}
		fmt.Fprintf(e.out, "trusted: %s\n", abs)
		if err := kit.Ports(p, r, e.out); err != nil {
			return err
		}
		if _, _, err := kit.SeedRole(p, r); err != nil {
			return err
		}
		fmt.Fprintf(e.out, "memory: %s\n", p.MemoryDir(r))
	}
	return nil
}

func (e *env) service(action, name string) error {
	if name == "" {
		return badUsage("", "service")
	}
	p, err := config.Find(name)
	if err != nil {
		return err
	}
	switch action {
	case "enable":
		template := "share/mingo@.service"
		if runtime.GOOS == "darwin" {
			template = "share/mingo.plist"
		}
		unit, err := mingo.Share.ReadFile(template)
		if err != nil {
			return err
		}
		if err := sys.ServiceEnable(config.Home(), p.Name, unit); err != nil {
			return err
		}
		switch {
		case runtime.GOOS == "darwin" && len(p.Autostart) > 0:
			fmt.Fprintf(e.out, "%s starts %s at login\n", sys.Label(p.Name), joinNames(p.Autostart))
		case runtime.GOOS == "darwin":
			fmt.Fprintf(e.out, "%s is enabled, but %s names no autostart roles, so nothing starts at login\n", sys.Label(p.Name), p.Name)
		case len(p.Autostart) > 0:
			user := os.Getenv("USER")
			switch lingering() {
			case "yes":
				fmt.Fprintf(e.out, "mingo@%s starts %s at boot, as lingering is on for %s\n", p.Name, joinNames(p.Autostart), user)
			case "no":
				fmt.Fprintf(e.out, "mingo@%s starts %s when %s logs in, not at boot, as lingering is off for %s\n", p.Name, joinNames(p.Autostart), user, user)
				fmt.Fprintln(e.out, "loginctl enable-linger turns lingering on, so it starts at boot")
			default:
				fmt.Fprintf(e.out, "mingo@%s starts %s at boot while lingering is on for %s\n", p.Name, joinNames(p.Autostart), user)
				fmt.Fprintln(e.out, "loginctl could not say whether it is, loginctl enable-linger turns it on")
			}
		default:
			fmt.Fprintf(e.out, "mingo@%s is enabled, but %s names no autostart roles, so nothing starts at boot\n", p.Name, p.Name)
		}
		return nil
	case "disable":
		if err := sys.ServiceDisable(config.Home(), p.Name); err != nil {
			return err
		}
		if runtime.GOOS == "darwin" {
			fmt.Fprintf(e.out, "%s is disabled, so %s's autostart roles no longer start at login\n", sys.Label(p.Name), p.Name)
		} else {
			fmt.Fprintf(e.out, "mingo@%s is disabled, so %s's autostart roles no longer start at boot\n", p.Name, p.Name)
		}
		return nil
	default:
		return badUsage("", "service")
	}
}

// yes or no as loginctl says for this user, or empty when it cannot say
func lingering() string {
	out, err := exec.Command("loginctl", "show-user", strconv.Itoa(os.Getuid()), "--property=Linger").Output()
	if err != nil {
		return ""
	}
	value, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "Linger=")
	if !ok || value != "yes" && value != "no" {
		return ""
	}
	return value
}
