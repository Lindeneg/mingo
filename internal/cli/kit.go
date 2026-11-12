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
	"syscall"

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

func (e *env) instructions(action, name string) error {
	if action != "edit" || name == "" {
		return badUsage("", "instructions")
	}
	p, err := config.Find(name)
	if err != nil {
		return err
	}
	if !p.AgentLocal() {
		return fmt.Errorf("%s has agent_files repo, so its %s is in its repository", p.Name, p.Provider().Kit().Instructions)
	}
	file := p.InstructionsFile()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if err := edit(file); err != nil {
		return err
	}
	if _, err := config.Load(p.Name); err != nil {
		return fmt.Errorf("%s is saved, but the project's config no longer loads: %v", file, err)
	}
	return nil
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
		return badUsage("", "secret")
	}
	d, err := e.kitDeps(args[1])
	if err != nil {
		return err
	}
	var token string
	if fd := os.Stdin.Fd(); sys.IsTerminal(fd) {
		fmt.Fprintf(e.out, "paste what %s printed: ", d.Project.Provider().Kit().Review.Token)
		if sys.SetEcho(fd, false) == nil {
			defer sys.SetEcho(fd, true)
			// a signal ends mingo before the defer runs, so the echo comes back here too
			interrupt := make(chan os.Signal, 1)
			signal.Notify(interrupt, os.Interrupt, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
			defer func() {
				signal.Stop(interrupt)
				close(interrupt)
			}()
			go func() {
				if sig, ok := <-interrupt; ok {
					sys.SetEcho(fd, true)
					fmt.Fprintln(e.out)
					code := 130
					if n, ok := sig.(syscall.Signal); ok {
						code = 128 + int(n)
					}
					os.Exit(code)
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

// the flags after the name, each with its value, or true when one of them asks for help
func newFlags(args []string) (map[string][]string, bool, error) {
	for _, arg := range args {
		if isHelp(arg) {
			return nil, true, nil
		}
	}
	flags := map[string][]string{}
	for i := 0; i < len(args); i++ {
		flag := args[i]
		if !strings.HasPrefix(flag, "--") || flag == "--" {
			return nil, false, fmt.Errorf("%q is not a flag, mingo new takes the name and then --<key> <value> flags", flag)
		}
		if k, v, ok := strings.Cut(flag, "="); ok {
			flags[k] = append(flags[k], v)
			continue
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return nil, false, fmt.Errorf("%s needs a value", flag)
		}
		flags[flag] = append(flags[flag], args[i+1])
		i++
	}
	return flags, false, nil
}

const stoppedNew = "stopped, nothing was written"

func (e *env) newProject(args []string) error {
	if len(args) == 0 {
		return badUsage("", "new")
	}
	name := args[0]
	if !config.IsName(name) {
		return badUsage(fmt.Sprintf("%q is not a project name, use lowercase letters, digits and hyphens", name), "new")
	}
	flags, help, err := newFlags(args[1:])
	if help {
		return e.help([]string{"new"})
	}
	if err != nil {
		return badUsage(err.Error(), "new")
	}
	file := config.File(name)
	if known, ok := config.NameOf(name); ok && known != name {
		return fmt.Errorf("%s is the id of the project %s, pick another name", name, known)
	}
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s is already a project, its config is %s", name, file)
	}
	k, err := kit.LoadEmbedded()
	if err != nil {
		return err
	}
	d := kit.NewDeps{Run: team.Capture, Out: e.out}
	var prompter *sys.Prompter
	if sys.IsTerminal(os.Stdin.Fd()) {
		prompter = sys.NewPrompter(os.Stdin, os.Stdout)
		d.Ask = prompter
	}
	// until the config is written a signal ends mingo new with nothing written and the terminal as it was
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	writing := make(chan struct{})
	go func() {
		select {
		case sig := <-signals:
			if prompter != nil {
				prompter.Restore()
			}
			fmt.Fprintf(e.err, "mingo: %s\n", stoppedNew)
			code := 130
			if s, ok := sig.(syscall.Signal); ok {
				code = 128 + int(s)
			}
			os.Exit(code)
		case <-writing:
		}
	}()
	stopped := func() error {
		fmt.Fprintf(e.err, "mingo: %s\n", stoppedNew)
		return exitError(130)
	}
	made, err := k.New(name, flags, d)
	var flagErr kit.FlagError
	switch {
	case errors.Is(err, sys.ErrStopped):
		return stopped()
	case errors.As(err, &flagErr):
		return badUsage(err.Error(), "new")
	case err != nil:
		return err
	}
	if prompter != nil {
		fmt.Fprint(e.out, made.Next(file))
		yes, err := prompter.Confirm("Continue?")
		if errors.Is(err, sys.ErrStopped) {
			return stopped()
		}
		if err != nil {
			return err
		}
		if !yes {
			return errors.New("nothing was written")
		}
	}
	signal.Stop(signals)
	close(writing)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(file, []byte(made.TOML), 0o644); err != nil {
		return err
	}
	// a config that does not load is taken back, so the next try is not refused for it
	if _, err := config.Load(name); err != nil {
		os.Remove(file)
		reason := strings.Replace(err.Error(), file+": ", "", 1)
		return fmt.Errorf("mingo new made a config that does not load, and took it back: %s", strings.ReplaceAll(reason, file, "it"))
	}
	fmt.Fprintf(e.out, "config: %s\n", file)
	if err := e.setup(name); err != nil {
		return fmt.Errorf("%w\nthe config is kept at %s, finish with mingo setup %s, then mingo apply %s", err, file, name, name)
	}
	if err := e.apply(name); err != nil {
		return fmt.Errorf("%w\nthe config is kept at %s, finish with mingo apply %s", err, file, name)
	}
	return nil
}
