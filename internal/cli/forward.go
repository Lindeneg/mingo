package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/Lindeneg/mingo"
	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/remote"
	"github.com/Lindeneg/mingo/internal/sys"
)

// what runs inside a session, or on this computer alone, and never on a host
var stayHere = map[string]bool{"clear": true, "heavy": true, "gate": true, "pr": true, "guard": true, "loop": true,
	"install": true, "version": true, "help": true, "host": true}

// the commands with one target, which argument it is, and whether they take this terminal
var oneTarget = map[string]struct {
	at  int
	tty bool
}{
	"attach": {0, true}, "ro": {0, true}, "peek": {0, false}, "say": {0, false}, "restart": {0, false},
	"memory": {0, false}, "path": {0, false}, "repo": {0, false}, "away": {0, false}, "back": {0, false},
	"setup": {0, false}, "apply": {0, false}, "doctor": {0, false},
	"service": {1, false}, "secret": {1, true}, "instructions": {1, true},
}

// runs a command on the host its project lives on, and says whether it did, so a command for this
// machine runs here as it always has
func (e *env) forward(command string, args []string) (bool, error) {
	if e.here || stayHere[command] || command == "ls" && len(args) > 0 {
		return false, nil
	}
	hosts, err := remote.Load()
	if err != nil {
		return true, err
	}
	if len(hosts) == 0 {
		return false, nil
	}
	x := &index{hosts: hosts, listed: map[string]listing{}, notes: e.err}
	switch command {
	case "ls", "agents", "status":
		return true, e.everywhere(x, command, args)
	case "start", "stop":
		return true, e.spread(x, command, args)
	case "new":
		return e.newOn(x, args)
	case "default":
		return true, e.defaultOn(x, args)
	case "project":
		if len(args) > 0 && args[0] == "add" {
			return e.projectAddOn(x, args)
		}
		return e.forwardOne(x, command, args, 1, len(args) > 0 && args[0] == "edit")
	}
	where, ok := oneTarget[command]
	if !ok {
		return false, nil
	}
	at := where.at
	// restart takes its flag before the target
	if command == "restart" {
		for at < len(args) && strings.HasPrefix(args[at], "-") {
			at++
		}
	}
	return e.forwardOne(x, command, args, at, where.tty)
}

func (e *env) forwardOne(x *index, command string, args []string, at int, tty bool) (bool, error) {
	spec := ""
	if at < len(args) {
		spec = args[at]
	}
	h, target, err := x.resolve(spec)
	if err != nil {
		return true, err
	}
	if h.Local {
		return false, nil
	}
	sent := append([]string{command}, args...)
	if at < len(args) {
		sent[at+1] = target
	} else {
		sent = append(sent, target)
	}
	return true, e.onHost(h, sent, tty)
}

func (e *env) onHost(h remote.Host, args []string, tty bool) error {
	tty = tty && sys.IsTerminal(os.Stdin.Fd())
	code, err := remote.Run(h, mingo.Version(), remote.Call{Args: args, TTY: tty, Stdin: os.Stdin, Stdout: e.out, Stderr: e.err})
	if err != nil {
		return err
	}
	if code != 0 {
		return exitError(code)
	}
	return nil
}

// runs a command on this machine as if it were given here, and its exit code
func (e *env) runHere(args []string) int {
	here := &env{out: e.out, err: e.err, host: e.host, here: true}
	return here.exitCode(here.dispatch(args))
}

type answer struct {
	host remote.Host
	out  string
	err  string
	code int
}

// ls, agents and status on every host at once, or on the hosts of the projects named, with a host
// column in front
func (e *env) everywhere(x *index, command string, names []string) error {
	ask := map[string][]string{}
	order := x.hosts
	if len(names) > 0 {
		order = nil
		for _, name := range names {
			h, project, err := x.where(name)
			if err != nil {
				return err
			}
			if _, seen := ask[h.Name]; !seen {
				order = append(order, h)
			}
			ask[h.Name] = append(ask[h.Name], project)
		}
	}
	answers := make([]answer, len(order))
	var wg sync.WaitGroup
	for i, h := range order {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := append([]string{command}, ask[h.Name]...)
			var out, errOut bytes.Buffer
			a := answer{host: h}
			if h.Local {
				here := &env{out: &out, err: &errOut, host: e.host, here: true}
				a.code = here.exitCode(here.dispatch(args))
			} else {
				code, err := remote.Run(h, mingo.Version(), remote.Call{Args: args, Stdout: &out, Stderr: &errOut, Notes: e.err})
				a.code = code
				if err != nil {
					fmt.Fprintf(&errOut, "mingo: %v\n", err)
					a.code = 1
				}
			}
			a.out, a.err = out.String(), errOut.String()
			answers[i] = a
		}()
	}
	wg.Wait()
	width := 4
	for _, a := range answers {
		width = max(width, len(a.host.Name))
	}
	if command == "agents" {
		e.table(answers)
	}
	failed := false
	for _, a := range answers {
		if command != "agents" {
			for _, line := range lines(a.out) {
				fmt.Fprintf(e.out, "%-*s  %s\n", width, a.host.Name, line)
			}
		}
		for _, line := range lines(a.err) {
			fmt.Fprintf(e.err, "%s: %s\n", a.host.Name, line)
		}
		if a.code != 0 {
			failed = true
		}
	}
	if failed {
		return exitError(1)
	}
	return nil
}

func lines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

var columns = regexp.MustCompile(`\s{2,}`)

// every host's agents in one table, the host first
func (e *env) table(answers []answer) {
	var rows [][]string
	for _, a := range answers {
		for i, line := range lines(a.out) {
			fields := columns.Split(strings.TrimSpace(line), -1)
			switch {
			case i == 0 && len(rows) == 0:
				rows = append(rows, append([]string{"HOST"}, fields...))
			case i > 0:
				rows = append(rows, append([]string{a.host.Name}, fields...))
			}
		}
	}
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i := 0; i < len(row)-1 && i < len(widths); i++ {
			widths[i] = max(widths[i], len(row[i]))
		}
	}
	for _, row := range rows {
		var b strings.Builder
		for i, field := range row {
			if i == len(row)-1 {
				b.WriteString(field)
			} else {
				fmt.Fprintf(&b, "%-*s  ", widths[i], field)
			}
		}
		fmt.Fprintln(e.out, b.String())
	}
}

// start and stop on the hosts of their targets, every target found before any host is asked
func (e *env) spread(x *index, command string, args []string) error {
	var flags, specs []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			specs = append(specs, a)
		}
	}
	if len(specs) == 0 {
		name, err := x.defaultName()
		if err != nil {
			return err
		}
		specs = []string{name}
	}
	var order []remote.Host
	targets := map[string][]string{}
	for _, spec := range specs {
		h, target, err := x.resolve(spec)
		if err != nil {
			return err
		}
		if _, seen := targets[h.Name]; !seen {
			order = append(order, h)
		}
		targets[h.Name] = append(targets[h.Name], target)
	}
	var first error
	for _, h := range order {
		sent := append(append([]string{command}, flags...), targets[h.Name]...)
		var err error
		if h.Local {
			if code := e.runHere(sent); code != 0 {
				err = exitError(code)
			}
		} else {
			err = e.onHost(h, sent, false)
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

// takes --host and its host out of the arguments
func hostFlag(args []string) (string, []string, error) {
	name, rest := "", []string{}
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--host":
			if i+1 == len(args) {
				return "", nil, errors.New("--host needs a host, mingo host ls lists them")
			}
			name = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--host="):
			name = strings.TrimPrefix(args[i], "--host=")
		default:
			rest = append(rest, args[i])
		}
	}
	return name, rest, nil
}

// the host --host names, or the one host there is
func (x *index) chosen(name, command string) (remote.Host, error) {
	if name != "" {
		h, ok := remote.Find(x.hosts, name)
		if !ok {
			return remote.Host{}, fmt.Errorf("no host named %s, mingo host ls lists them", name)
		}
		return h, nil
	}
	if len(x.hosts) == 1 {
		return x.hosts[0], nil
	}
	names := make([]string, len(x.hosts))
	for i, h := range x.hosts {
		names[i] = h.Name
	}
	return remote.Host{}, fmt.Errorf("%s needs --host, one of %s", command, strings.Join(names, " "))
}

func (e *env) newOn(x *index, args []string) (bool, error) {
	name, rest, err := hostFlag(args)
	if err != nil {
		return true, badUsage(err.Error(), "new")
	}
	h, err := x.chosen(name, "mingo new")
	if err != nil {
		return true, err
	}
	if h.Local {
		return true, e.newProject(rest)
	}
	return true, e.onHost(h, append([]string{"new"}, rest...), true)
}

func (e *env) projectAddOn(x *index, args []string) (bool, error) {
	name, rest, err := hostFlag(args)
	if err != nil {
		return true, badUsage(err.Error(), "project", "add")
	}
	h, err := x.chosen(name, "mingo project add")
	if err != nil {
		return true, err
	}
	if h.Local {
		if err := subcommand(rest, "project", "add", "show", "edit"); err != nil {
			return true, err
		}
		return true, e.project(rest[0], rest[1])
	}
	return true, e.onHost(h, append([]string{"project"}, rest...), true)
}

// the default project is this computer's own, and names a project on any host
func (e *env) defaultOn(x *index, args []string) error {
	if len(args) > 0 {
		_, name, err := x.where(args[0])
		if err != nil {
			return err
		}
		if err := os.MkdirAll(config.Dir(), 0o755); err != nil {
			return err
		}
		return os.WriteFile(config.DefaultFile(), []byte(name+"\n"), 0o644)
	}
	name := defaultFile()
	if name == "" {
		return errors.New("no default project is set, mingo default <project> sets one")
	}
	if _, _, err := x.where(name); err != nil {
		return fmt.Errorf("the default project is %s, which no host has, mingo default <project> sets another", name)
	}
	fmt.Fprintln(e.out, name)
	return nil
}

func defaultFile() string {
	data, _ := os.ReadFile(config.DefaultFile())
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
}

// the projects each host has, asked once per command
type index struct {
	hosts  []remote.Host
	mu     sync.Mutex
	listed map[string]listing
	// where an install a host needs before it can answer says what it does
	notes io.Writer
}

type listing struct {
	names []string
	ids   map[string]string
	err   error
}

func (l listing) has(word string) (string, bool) {
	if slices.Contains(l.names, word) {
		return word, true
	}
	name, ok := l.ids[word]
	return name, ok
}

func (x *index) list(h remote.Host) listing {
	x.mu.Lock()
	l, ok := x.listed[h.Name]
	x.mu.Unlock()
	if ok {
		return l
	}
	l = listing{ids: map[string]string{}}
	var text string
	if h.Local {
		var b strings.Builder
		for _, name := range config.Names() {
			b.WriteString(name + " " + config.IDOf(name) + "\n")
		}
		text = b.String()
	} else {
		var out, errOut bytes.Buffer
		code, err := remote.Run(h, mingo.Version(), remote.Call{Args: []string{"ls", "--names"}, Stdout: &out, Stderr: &errOut, Notes: x.notes})
		switch {
		case err != nil:
			l.err = err
		case code != 0:
			l.err = fmt.Errorf("%s: %s", h.SSH, strings.TrimSpace(errOut.String()))
		}
		text = out.String()
	}
	for _, line := range lines(text) {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		l.names = append(l.names, f[0])
		if len(f) > 1 {
			l.ids[f[1]] = f[0]
		}
	}
	x.mu.Lock()
	x.listed[h.Name] = l
	x.mu.Unlock()
	return l
}

func (x *index) listAll() {
	var wg sync.WaitGroup
	for _, h := range x.hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x.list(h)
		}()
	}
	wg.Wait()
}

type notFound struct{ text string }

func (n notFound) Error() string { return n.text }

func cacheFile() string {
	return filepath.Join(config.StateDir(), "hosts.json")
}

// which host had which project when last asked, so a command asks that host alone while it still does
func readCache() map[string]string {
	cache := map[string]string{}
	data, err := os.ReadFile(cacheFile())
	if err == nil {
		json.Unmarshal(data, &cache)
	}
	return cache
}

func writeCache(cache map[string]string) {
	data, err := json.Marshal(cache)
	if err != nil || os.MkdirAll(filepath.Dir(cacheFile()), 0o755) != nil {
		return
	}
	os.WriteFile(cacheFile(), data, 0o644)
}

// the host a project lives on and the project's name, for its name or id
func (x *index) where(word string) (remote.Host, string, error) {
	cache := readCache()
	if h, ok := remote.Find(x.hosts, cache[word]); ok {
		if name, ok := x.list(h).has(word); ok {
			return h, name, nil
		}
	}
	x.listAll()
	var found []remote.Host
	var name string
	var unreached []string
	for _, h := range x.hosts {
		l := x.list(h)
		if l.err != nil {
			unreached = append(unreached, l.err.Error())
		}
		if n, ok := l.has(word); ok {
			found, name = append(found, h), n
		}
	}
	switch len(found) {
	case 0:
		text := fmt.Sprintf("no host has a project named %s", word)
		if len(unreached) > 0 {
			text += ", and some could not be asked: " + strings.Join(unreached, "; ")
		}
		return remote.Host{}, "", notFound{text}
	case 1:
		cache[word] = found[0].Name
		writeCache(cache)
		return found[0], name, nil
	}
	return remote.Host{}, "", fmt.Errorf("the project %s is on the hosts %s and %s, rename one of them", word, found[0].Name, found[1].Name)
}

// the project of a target with no project named, as on one machine, with a project of any host
// counted for the one project there is
func (x *index) defaultName() (string, error) {
	if v := os.Getenv("MINGO_PROJECT"); v != "" {
		return v, nil
	}
	for _, h := range x.hosts {
		if h.Local {
			if name, ok := config.ProjectOfDir(config.Cwd()); ok {
				return name, nil
			}
		}
	}
	if name := defaultFile(); name != "" {
		return name, nil
	}
	x.listAll()
	var all []string
	for _, h := range x.hosts {
		all = append(all, x.list(h).names...)
	}
	if len(all) != 1 {
		return "", errors.New("which project? name it, or set one with mingo default <project>")
	}
	return all[0], nil
}

// the host of a target and the target written out as project/role for it
func (x *index) resolve(spec string) (remote.Host, string, error) {
	name, part, written := strings.Cut(spec, "/")
	if !written {
		if spec != "" {
			h, project, err := x.where(spec)
			if err == nil {
				return h, project, nil
			}
			var missing notFound
			if !errors.As(err, &missing) {
				return remote.Host{}, "", err
			}
		}
		var err error
		if name, err = x.defaultName(); err != nil {
			return remote.Host{}, "", err
		}
		part = spec
	}
	h, project, err := x.where(name)
	if err != nil {
		return remote.Host{}, "", err
	}
	if part != "" {
		project += "/" + part
	}
	return h, project, nil
}
