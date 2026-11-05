// Package cli reads mingo's command line and runs each command.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Lindeneg/mingo"
	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/guard"
	"github.com/Lindeneg/mingo/internal/heavy"
	"github.com/Lindeneg/mingo/internal/host"
	"github.com/Lindeneg/mingo/internal/install"
	"github.com/Lindeneg/mingo/internal/loop"
)

const usage = `mingo runs Claude Code sessions for every project on this machine, one tmux session per role.

A target is <project>/<role>. A role may be an alias and a project its id, and the project may be
left out inside a project's clone, with MINGO_PROJECT set, or with a default project.

  mingo ls                              projects, roles and clones
  mingo agents [project...]             live sessions with state, uptime and Remote Control link
  mingo status [project...]             each role's state, model and effort, and the Claude login
  mingo start [--autostart] [target...] start roles, a whole project when no role is named
  mingo stop [target...]                ask sessions to exit, close what is left after a minute and a half
  mingo restart [--force] <target>      end a session where it stands, the loop starts a fresh one
  mingo attach <target>                 attach, detach with Ctrl+B then D
  mingo ro <target>                     attach read only
  mingo peek <target> [lines]           the last lines of a session's screen
  mingo say <target> <message>          type a message into a session
  mingo clear                           from inside a session, end it so the loop starts a fresh one
  mingo path <target>                   a role's clone
  mingo memory <target>                 a role's handoff memory
  mingo repo [project]                  a project's GitHub repository
  mingo default [project]               show or set the default project
  mingo project add|show|edit <name>    a project's config in ~/.config/mingo/projects
  mingo setup <project>                 clone missing roles and make their memory folders
  mingo new <name> [--preset p] [...]   a project from a preset, its config, clones and files, asking what flags leave out
  mingo apply <project>                 land the files of the project's modules through a pull request
  mingo doctor <project>                what of the project's modules is in place and what is missing
  mingo claude edit <project>           a local project's CLAUDE.md, which mingo apply writes into every clone
  mingo secret review <project>         set the review's token from what claude setup-token printed
  mingo service enable|disable <project> start a project's autostart roles at boot, at login on a Mac
  mingo install                         the command, tmux and Claude settings
  mingo heavy <label> -- <command...>   run a heavy command in one of the machine's memory slots
  mingo pr merge <number>               check a ready pull request, then squash merge it
  mingo pr rebase <branch>              rebase a branch onto the base in a worktree, keeping both sides of the shared lists
  mingo gate <log>                      run the project's gate targets into a log, stopping at the first red one
  mingo version                         this mingo's version
  mingo help [command]                  this list, or what a command does

mingo help <command> explains a command, and so does mingo <command> --help.
`

func (e *env) help(words []string) error {
	if len(words) == 0 {
		fmt.Fprint(e.out, usage)
		return nil
	}
	text, ok := e.page(words)
	if !ok {
		return fmt.Errorf("no command %s, mingo help lists them", strings.Join(words, " "))
	}
	fmt.Fprint(e.out, text)
	return nil
}

func isHelp(arg string) bool {
	return arg == "--help" || arg == "-h"
}

// a help flag before any -- asks for the command's page, what follows -- is the command's own
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if isHelp(a) {
			return true
		}
	}
	return false
}

func known(command string) bool {
	_, ok := pages[command]
	return command == "new" || ok && !strings.Contains(command, " ")
}

// a command line mingo cannot take, which exits 2 with the usage of the command its words name
type usageError struct {
	problem string
	words   []string
}

func (u usageError) Error() string {
	return u.problem
}

func badUsage(problem string, words ...string) error {
	return usageError{problem: problem, words: words}
}

func (e *env) usage(words []string) {
	text, _ := e.page(words)
	head, _, _ := strings.Cut(text, "\n\n")
	for i, line := range strings.Split(head, "\n") {
		prefix := "usage: "
		if i > 0 {
			prefix = "       "
		}
		fmt.Fprintln(e.err, prefix+line)
	}
}

func extra(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = strconv.Quote(a)
	}
	if len(quoted) == 1 {
		return "extra argument " + quoted[0]
	}
	return "extra arguments " + strings.Join(quoted, " ")
}

// no flag and at most n arguments, any number when n is negative
func takes(args []string, n int, words ...string) error {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return badUsage(fmt.Sprintf("unknown flag %q", a), words...)
		}
	}
	if n >= 0 && len(args) > n {
		return badUsage(extra(args[n:]), words...)
	}
	return nil
}

func exactlyOne(args []string, words ...string) error {
	if len(args) == 0 {
		return badUsage("", words...)
	}
	return takes(args, 1, words...)
}

// a subcommand of actions, then exactly one argument
func subcommand(args []string, command string, actions ...string) error {
	if len(args) == 0 {
		return badUsage("", command)
	}
	if !slices.Contains(actions, args[0]) {
		return badUsage(fmt.Sprintf("unknown command %q", command+" "+args[0]), command)
	}
	return exactlyOne(args[1:], command, args[0])
}

// the most arguments of each command that takes no flag and leaves its arguments to this check
var most = map[string]int{
	"ls": 0, "agents": -1, "status": -1, "attach": 1, "ro": 1, "peek": 2, "clear": 0, "path": 1, "memory": 1,
	"repo": 1, "default": 1, "setup": 1, "install": 0, "apply": 1, "doctor": 1, "version": 0, "guard": 1,
}

var withSubcommands = map[string]bool{"project": true, "pr": true, "service": true, "secret": true, "claude": true}

type exitError int

func (e exitError) Error() string {
	return fmt.Sprintf("exit %d", int(e))
}

type env struct {
	out  io.Writer
	err  io.Writer
	host host.Host
}

func withLocalBin() {
	bin := filepath.Join(config.Home(), ".local", "bin")
	path := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(path) {
		if dir == bin {
			return
		}
	}
	os.Setenv("PATH", bin+string(os.PathListSeparator)+path)
}

func Main(args []string) int {
	withLocalBin()
	e := &env{out: os.Stdout, err: os.Stderr, host: host.New()}
	err := e.dispatch(args)
	var code exitError
	var bad usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &code):
		return int(code)
	case errors.As(err, &bad):
		if bad.problem != "" {
			fmt.Fprintf(e.err, "mingo: %s\n", bad.problem)
		}
		if len(bad.words) > 0 {
			e.usage(bad.words)
		}
		return 2
	default:
		fmt.Fprintf(e.err, "mingo: %v\n", err)
		return 1
	}
}

func (e *env) dispatch(args []string) error {
	command := "help"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	if isHelp(command) {
		return e.help(args)
	}
	if !known(command) {
		return badUsage(fmt.Sprintf("unknown command %q, mingo help lists them", command))
	}
	if wantsHelp(args) {
		words := []string{command}
		if withSubcommands[command] && !isHelp(args[0]) {
			words = append(words, args[0])
		}
		return e.help(words)
	}
	if n, ok := most[command]; ok {
		if err := takes(args, n, command); err != nil {
			return err
		}
	}
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	switch command {
	case "ls":
		return e.ls()
	case "agents":
		return e.agents(args)
	case "status":
		return e.status(args)
	case "start":
		return e.start(args)
	case "stop":
		return e.stop(args)
	case "restart":
		return e.restart(args)
	case "attach":
		return e.attach(arg(0), false)
	case "ro":
		return e.attach(arg(0), true)
	case "peek":
		return e.peek(args)
	case "say":
		return e.say(args)
	case "clear":
		return e.clear()
	case "path":
		return e.path(arg(0))
	case "memory":
		return e.memory(arg(0))
	case "repo":
		return e.repo(arg(0))
	case "default":
		return e.setDefault(args)
	case "project":
		if err := subcommand(args, "project", "add", "show", "edit"); err != nil {
			return err
		}
		return e.project(arg(0), arg(1))
	case "setup":
		return e.setup(arg(0))
	case "service":
		if err := subcommand(args, "service", "enable", "disable"); err != nil {
			return err
		}
		return e.service(arg(0), arg(1))
	case "install":
		return install.Run(e.out)
	case "heavy":
		return e.heavy(args)
	case "pr":
		return e.pr(args)
	case "gate":
		return e.gate(args)
	case "new":
		return e.newProject(args)
	case "apply":
		return e.apply(arg(0))
	case "doctor":
		return e.doctor(arg(0))
	case "claude":
		if err := subcommand(args, "claude", "edit"); err != nil {
			return err
		}
		return e.claude(arg(0), arg(1))
	case "secret":
		if err := subcommand(args, "secret", "review"); err != nil {
			return err
		}
		return e.secret(args)
	case "version":
		fmt.Fprintf(e.out, "mingo %s\n", mingo.Version())
		return nil
	case "loop":
		if len(args) != 2 {
			return badUsage("", "loop")
		}
		return exitError(loop.Run(args[0], args[1]))
	case "guard":
		return e.guard(arg(0))
	case "help":
		return e.help(args)
	default:
		return badUsage(fmt.Sprintf("unknown command %q, mingo help lists them", command))
	}
}

func (e *env) heavy(args []string) error {
	if len(args) < 3 || args[0] == "" || args[1] != "--" {
		return badUsage("", "heavy")
	}
	return exitError(heavy.Run(args[0], args[2:]))
}

func (e *env) guard(kind string) error {
	input, _ := io.ReadAll(os.Stdin)
	switch kind {
	case "write":
		fmt.Fprint(e.out, guard.Write(input, guard.WriteEnvFromOS()))
	case "bash":
		fmt.Fprint(e.out, guard.Bash(input))
	default:
		return badUsage("", "guard")
	}
	return nil
}

func joinNames(names []string) string {
	return strings.Join(names, " ")
}
