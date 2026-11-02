package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Lindeneg/mingo"
	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/install"
	"github.com/Lindeneg/mingo/internal/kit"
	"github.com/Lindeneg/mingo/internal/sys"
)

func (e *env) project(action, name string) error {
	if name == "" {
		return errors.New("project add|show|edit <name>")
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
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		cmd := exec.Command(editor, file)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	default:
		return errors.New("project add|show|edit <name>")
	}
	return nil
}

func (e *env) setDefault(args []string) error {
	if len(args) == 0 {
		name, err := config.DefaultProject()
		if err != nil {
			return err
		}
		fmt.Fprintln(e.out, name)
		return nil
	}
	p, err := config.Find(args[0])
	if err != nil {
		return fmt.Errorf("no project named %s", args[0])
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

func loud(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
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
			if err := kit.Ports(p, r, e.out); err != nil {
				return err
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			if err := install.Trust(config.Home(), abs); err != nil {
				return err
			}
			fmt.Fprintf(e.out, "trusted: %s\n", abs)
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
		return errors.New("service enable|disable <project>")
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
			fmt.Fprintf(e.out, "mingo@%s starts %s at boot, while lingering is on for %s\n", p.Name, joinNames(p.Autostart), os.Getenv("USER"))
		default:
			fmt.Fprintf(e.out, "mingo@%s is enabled, but %s names no autostart roles, so nothing starts at boot\n", p.Name, p.Name)
		}
		return nil
	case "disable":
		return sys.ServiceDisable(config.Home(), p.Name)
	default:
		return errors.New("service enable|disable <project>")
	}
}
