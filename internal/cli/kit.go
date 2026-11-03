package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/Lindeneg/mingo"
	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/kit"
	"github.com/Lindeneg/mingo/internal/sys"
	"github.com/Lindeneg/mingo/internal/team"
)

func (e *env) kitDeps(name string) (kit.Deps, error) {
	if name == "" {
		return kit.Deps{}, errors.New("which project? name it")
	}
	p, err := config.Find(name)
	if err != nil {
		return kit.Deps{}, err
	}
	k, err := kit.LoadEmbedded()
	if err != nil {
		return kit.Deps{}, err
	}
	return kit.Deps{Kit: k, Project: p, Run: team.Capture, Out: e.out, TempRoot: os.TempDir(), Version: mingo.Version(), Home: config.Home()}, nil
}

func (e *env) apply(name string) error {
	d, err := e.kitDeps(name)
	if err != nil {
		return err
	}
	return kit.Apply(d)
}

func (e *env) claude(action, name string) error {
	if action != "edit" || name == "" {
		return errors.New("claude edit <project>")
	}
	p, err := config.Find(name)
	if err != nil {
		return err
	}
	if !p.ClaudeLocal() {
		return fmt.Errorf("%s has claude_files repo, so its CLAUDE.md is in its repository", p.Name)
	}
	file := config.ClaudeFile(p.Name)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return edit(file)
}

func (e *env) doctor(name string) error {
	d, err := e.kitDeps(name)
	if err != nil {
		return err
	}
	missing, err := kit.Doctor(d)
	if err != nil {
		return err
	}
	if missing > 0 {
		return exitError(1)
	}
	return nil
}

func (e *env) secret(args []string) error {
	if len(args) != 2 || args[0] != "review" {
		return errors.New("secret review <project>")
	}
	d, err := e.kitDeps(args[1])
	if err != nil {
		return err
	}
	var token string
	if fd := os.Stdin.Fd(); sys.IsTerminal(fd) {
		fmt.Fprint(e.out, "paste what claude setup-token printed: ")
		if sys.SetEcho(fd, false) == nil {
			defer sys.SetEcho(fd, true)
			// ctrl c ends mingo before the defer runs, so the echo comes back here too
			interrupt := make(chan os.Signal, 1)
			signal.Notify(interrupt, os.Interrupt)
			defer func() {
				signal.Stop(interrupt)
				close(interrupt)
			}()
			go func() {
				if _, ok := <-interrupt; ok {
					sys.SetEcho(fd, true)
					fmt.Fprintln(e.out)
					os.Exit(130)
				}
			}()
		}
		token, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Fprintln(e.out)
	} else {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		token = string(data)
	}
	return kit.SetReviewSecret(d, token)
}

const newUsage = "new <name> [--preset bare|solo|team|full] [--<key> <value>]"

func (e *env) newProject(args []string) error {
	if len(args) == 0 || !config.IsName(args[0]) {
		return errors.New(newUsage)
	}
	name := args[0]
	flags := map[string][]string{}
	for i := 1; i < len(args); i++ {
		flag := args[i]
		if !strings.HasPrefix(flag, "--") {
			return errors.New(newUsage)
		}
		if k, v, ok := strings.Cut(flag, "="); ok {
			flags[k] = append(flags[k], v)
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("%s needs a value", flag)
		}
		flags[flag] = append(flags[flag], args[i+1])
		i++
	}
	file := config.File(name)
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s exists, mingo new makes a project that does not", file)
	}
	k, err := kit.LoadEmbedded()
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	terminal := sys.IsTerminal(os.Stdin.Fd())
	prompt := func(question, def string) (string, bool) {
		if !terminal {
			return "", false
		}
		if def != "" {
			fmt.Fprintf(e.out, "%s [%s]: ", question, def)
		} else {
			fmt.Fprintf(e.out, "%s: ", question)
		}
		line, _ := in.ReadString('\n')
		return strings.TrimSpace(line), true
	}
	preset := ""
	if v := flags["--preset"]; len(v) > 0 {
		preset = v[0]
	} else if v, ok := prompt("the preset: "+strings.Join(kit.Presets, ", "), ""); ok {
		preset = v
	} else {
		return errors.New("mingo new needs --preset, one of " + strings.Join(kit.Presets, " "))
	}
	text, err := k.New(name, preset, flags, prompt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		return err
	}
	// a config that does not load is taken back, so the next try is not refused for it
	if _, err := config.Load(name); err != nil {
		os.Remove(file)
		return err
	}
	fmt.Fprintf(e.out, "config: %s\n", file)
	if err := e.setup(name); err != nil {
		return err
	}
	return e.apply(name)
}
