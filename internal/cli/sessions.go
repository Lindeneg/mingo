package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/provider"
)

// long enough for the loop to end a held session itself
const StopWait = 90

func stopWait() int {
	if n, err := strconv.Atoi(os.Getenv("MINGO_STOP_WAIT")); err == nil {
		return n
	}
	return StopWait
}

func marker(p *config.Project, role, kind string) string {
	return filepath.Join(p.State(), role+"."+kind)
}

func has(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func touch(path string) error {
	return os.WriteFile(path, nil, 0o644)
}

func (e *env) notRunning(session string) error {
	return fmt.Errorf("%s is not running, use mingo start", session)
}

func (e *env) startRole(p *config.Project, role string) error {
	session, dir := p.SessionName(role), p.CloneDir(role)
	if e.host.Has(session) {
		fmt.Fprintf(e.out, "%s: already running\n", session)
		return nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s has no clone at %s, see mingo setup %s", session, dir, p.Name)
	}
	if err := os.MkdirAll(p.State(), 0o755); err != nil {
		return err
	}
	os.Remove(marker(p, role, "stop"))
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	key := p.EnvKey(role)
	argv := []string{"env"}
	for _, v := range []string{"MINGO_CONFIG", "MINGO_STATE", "MINGO_TMUX_SOCKET", p.Provider().CommandEnv(), "MINGO_EXIT_GRACE",
		"MINGO_TERM_GRACE", "MINGO_WATCH_EVERY", "MINGO_MODEL_" + key, "MINGO_EFFORT_" + key} {
		if value := os.Getenv(v); value != "" {
			argv = append(argv, v+"="+value)
		}
	}
	argv = append(argv, exe, "loop", p.Name, role)
	if err := e.host.New(session, config.Home(), argv); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "%s: started on %s\n", session, p.DescribeRun(role))
	return nil
}

func specsOrDefault(specs []string) ([]string, error) {
	if len(specs) > 0 {
		return specs, nil
	}
	name, err := config.DefaultProject()
	if err != nil {
		return nil, err
	}
	return []string{name}, nil
}

// the flags a command takes out of its arguments, refusing any other
func flags(command string, args []string, allowed ...string) (map[string]bool, []string, error) {
	set := map[string]bool{}
	var rest []string
	for _, a := range args {
		switch {
		case slices.Contains(allowed, a):
			set[a] = true
		case strings.HasPrefix(a, "-"):
			return nil, nil, badUsage(fmt.Sprintf("unknown flag %q", a), command)
		default:
			rest = append(rest, a)
		}
	}
	return set, rest, nil
}

type picked struct {
	p     *config.Project
	roles []string
}

// every target is resolved before any session is touched, so a typo changes nothing
func pick(specs []string, onlyAutostart bool, parse func(string) (*config.Project, string, error)) ([]picked, error) {
	specs, err := specsOrDefault(specs)
	if err != nil {
		return nil, err
	}
	var all []picked
	for _, spec := range specs {
		p, role, err := parse(spec)
		if err != nil {
			return nil, err
		}
		switch {
		case role != "":
			all = append(all, picked{p, []string{role}})
		case onlyAutostart:
			all = append(all, picked{p, p.Autostart})
		default:
			all = append(all, picked{p, p.RoleNames()})
		}
	}
	return all, nil
}

// the project and role a command that controls a running loop acts on, with no role named the lead
// when lead is set and the whole project when not. a loop runs on the config it loaded, so while
// the config does not load a target written as project/role still reaches the session by its names,
// and a whole project reaches the roles that have run a session here
func (e *env) loopTarget(spec string, lead bool) (*config.Project, string, error) {
	name, part, written := strings.Cut(spec, "/")
	if !written {
		if config.Known(spec) {
			name, part = spec, ""
		} else {
			var err error
			if name, err = config.DefaultProject(); err != nil {
				return nil, "", err
			}
			part = spec
		}
	}
	p, err := config.Find(name)
	if err != nil {
		found, ok := config.NameOf(name)
		if !ok {
			return nil, "", err
		}
		if !written && part == "" && !lead {
			p := &config.Project{Name: found}
			if p.Roles = rolesRun(p); len(p.Roles) > 0 {
				sessions := make([]string, len(p.Roles))
				for i, r := range p.Roles {
					sessions[i] = p.SessionName(r.Name)
				}
				fmt.Fprintf(e.err, "mingo: %s's config does not load, so the command goes to the tmux sessions of the roles that have run, %s: %v\n", found, strings.Join(sessions, ", "), err)
				return p, "", nil
			}
		}
		if !written || !config.IsName(part) {
			return nil, "", fmt.Errorf("%s's config does not load, so write the target as %s/<role>: %v", found, found, err)
		}
		p = &config.Project{Name: found}
		fmt.Fprintf(e.err, "mingo: %s's config does not load, so the command goes to the tmux session %s: %v\n", found, p.SessionName(part), err)
		return p, part, nil
	}
	switch {
	case part != "":
		role, err := p.Resolve(part)
		if err != nil {
			return nil, "", err
		}
		return p, role, nil
	case lead:
		return p, p.Lead, nil
	}
	return p, "", nil
}

// a loop writes its role's using file at every session and leaves it there, so the state folder
// names the roles that have run without the config
func rolesRun(p *config.Project) []config.Role {
	entries, _ := os.ReadDir(p.State())
	var roles []config.Role
	for _, entry := range entries {
		if name, ok := strings.CutSuffix(entry.Name(), ".using"); ok && config.IsName(name) {
			roles = append(roles, config.Role{Name: name})
		}
	}
	return roles
}

// the providers of the projects given, each once, in their order
func providersOf(projects []*config.Project) []provider.Provider {
	var out []provider.Provider
	seen := map[string]bool{}
	for _, p := range projects {
		prov := p.Provider()
		if !seen[prov.Name()] {
			seen[prov.Name()] = true
			out = append(out, prov)
		}
	}
	return out
}

// the projects with a role that is not running
func (e *env) stopped(all []picked) []*config.Project {
	var out []*config.Project
	for _, pk := range all {
		for _, r := range pk.roles {
			if !e.host.Has(pk.p.SessionName(r)) {
				out = append(out, pk.p)
				break
			}
		}
	}
	return out
}

func (e *env) start(args []string) error {
	set, specs, err := flags("start", args, "--autostart")
	if err != nil {
		return err
	}
	all, err := pick(specs, set["--autostart"], config.ParseTarget)
	if err != nil {
		return err
	}
	for _, prov := range providersOf(e.stopped(all)) {
		if command, _ := provider.CommandOf(prov); !installed(command) {
			return fmt.Errorf("%s is not installed, so no session can start, see %s", command, prov.Site())
		}
	}
	for _, pk := range all {
		if len(pk.roles) == 0 {
			fmt.Fprintf(e.out, "%s: no autostart roles, nothing to start\n", pk.p.Name)
		}
		for _, r := range pk.roles {
			if err := e.startRole(pk.p, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *env) stop(args []string) error {
	_, specs, err := flags("stop", args)
	if err != nil {
		return err
	}
	all, err := pick(specs, false, func(spec string) (*config.Project, string, error) { return e.loopTarget(spec, false) })
	if err != nil {
		return err
	}
	var asked []string
	for _, pk := range all {
		p := pk.p
		if err := os.MkdirAll(p.State(), 0o755); err != nil {
			return err
		}
		for _, r := range pk.roles {
			session := p.SessionName(r)
			if !e.host.Has(session) {
				fmt.Fprintf(e.out, "%s: not running\n", session)
				continue
			}
			touch(marker(p, r, "stop"))
			// the loop types the exit keys only while a session runs, so between sessions the stop marker alone ends it
			if has(marker(p, r, "running")) {
				touch(marker(p, r, "restart"))
			}
			asked = append(asked, session)
		}
	}
	if len(asked) == 0 {
		return nil
	}
	wait := stopWait()
	for i := 0; i < wait; i++ {
		left := false
		for _, s := range asked {
			if e.host.Has(s) {
				left = true
			}
		}
		if !left {
			break
		}
		time.Sleep(time.Second)
	}
	for _, s := range asked {
		if e.host.Has(s) {
			e.host.Kill(s)
			fmt.Fprintf(e.out, "%s: did not exit within %d s, its tmux session was closed\n", s, wait)
		} else {
			fmt.Fprintf(e.out, "%s: stopped\n", s)
		}
	}
	return nil
}

func (e *env) restart(args []string) error {
	set, args, err := flags("restart", args, "--force")
	if err != nil {
		return err
	}
	if len(args) > 1 {
		return badUsage(extra(args[1:]), "restart")
	}
	force := set["--force"]
	spec := ""
	if len(args) > 0 {
		spec = args[0]
	}
	p, role, err := e.loopTarget(spec, true)
	if err != nil {
		return err
	}
	session := p.SessionName(role)
	if !e.host.Has(session) {
		return e.notRunning(session)
	}
	if err := os.MkdirAll(p.State(), 0o755); err != nil {
		return err
	}
	if !force {
		touch(marker(p, role, "restart"))
		fmt.Fprintf(e.out, "%s: %s within seconds ends the session where it stands, then the loop starts a fresh one\n", session, p.Provider().ExitShown())
		return nil
	}
	// between sessions the keys would reach the loop itself or the next session
	if !has(marker(p, role, "running")) {
		fmt.Fprintf(e.out, "%s: between sessions, the loop starts one by itself\n", session)
		return nil
	}
	if err := e.host.SendKeys(session, "C-c"); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	touch(marker(p, role, "restart"))
	fmt.Fprintf(e.out, "%s: interrupted, %s follows within seconds and the session's process is ended if that does not end it\n", session, p.Provider().ExitShown())
	return nil
}

func (e *env) attach(spec string, readOnly bool) error {
	p, role, err := e.loopTarget(spec, true)
	if err != nil {
		return err
	}
	session := p.SessionName(role)
	if !e.host.Has(session) {
		return e.notRunning(session)
	}
	return e.host.Attach(session, readOnly)
}

var linesRe = regexp.MustCompile(`^[0-9]+$`)

func (e *env) peek(args []string) error {
	lines, spec := "40", ""
	if len(args) > 0 {
		spec = args[0]
	}
	if len(args) > 1 {
		lines = args[1]
	}
	if !linesRe.MatchString(lines) {
		return fmt.Errorf("peek takes a number of lines, not %s", lines)
	}
	n, err := strconv.Atoi(lines)
	if err != nil {
		return fmt.Errorf("peek takes a number of lines, not %s", lines)
	}
	p, role, err := e.loopTarget(spec, true)
	if err != nil {
		return err
	}
	session := p.SessionName(role)
	if !e.host.Has(session) {
		return e.notRunning(session)
	}
	out, err := e.host.Peek(session, n)
	if err != nil {
		return err
	}
	fmt.Fprint(e.out, out)
	return nil
}

func (e *env) say(args []string) error {
	if len(args) < 2 {
		return badUsage("", "say")
	}
	p, role, err := e.loopTarget(args[0], true)
	if err != nil {
		return err
	}
	session := p.SessionName(role)
	if !e.host.Has(session) {
		return e.notRunning(session)
	}
	// keys sent between sessions would become the next session's first input
	if !has(marker(p, role, "running")) {
		return fmt.Errorf("%s is between sessions, try again in a few seconds", session)
	}
	if err := e.host.SendText(session, strings.Join(args[1:], " ")); err != nil {
		return err
	}
	return e.host.SendKeys(session, "Enter")
}

// a project that does not load keeps its place with its error, so the others are still shown
type loaded struct {
	p   *config.Project
	err error
}

func projectsOrAll(names []string) []loaded {
	if len(names) == 0 {
		names = config.Names()
	}
	var projects []loaded
	for _, name := range names {
		p, err := config.Find(name)
		projects = append(projects, loaded{p, err})
	}
	return projects
}

func installed(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

func (e *env) status(names []string) error {
	failed := false
	var shown []*config.Project
	for _, l := range projectsOrAll(names) {
		if l.err != nil {
			fmt.Fprintf(e.err, "mingo: %v\n", l.err)
			failed = true
			continue
		}
		p := l.p
		shown = append(shown, p)
		for _, r := range p.RoleNames() {
			session := p.SessionName(r)
			running := e.host.Has(session)
			on := ""
			if running {
				if data, err := os.ReadFile(marker(p, r, "using")); err == nil {
					if f := strings.Fields(string(data)); len(f) >= 2 {
						on = "on " + f[0] + ", effort " + f[1]
					}
				}
			}
			if on == "" {
				on = "would start on " + p.DescribeRun(r)
			}
			switch {
			case !running:
				fmt.Fprintf(e.out, "%s: not running, %s\n", session, on)
			case has(marker(p, r, "running")):
				fmt.Fprintf(e.out, "%s: running, %s\n", session, on)
			default:
				fmt.Fprintf(e.out, "%s: between sessions, %s\n", session, on)
			}
		}
	}
	providers := providersOf(shown)
	if prov, ok := provider.Get(provider.Default); ok && len(providers) == 0 {
		providers = []provider.Provider{prov}
	}
	for _, prov := range providers {
		command, _ := provider.CommandOf(prov)
		if !installed(command) {
			fmt.Fprintf(e.out, "%s: not installed on this host, see %s\n", command, prov.Site())
			continue
		}
		if l, ok := prov.(provider.Login); ok && l.Login(command, e.out) != nil {
			fmt.Fprintf(e.out, "%s: not logged in on this host\n", command)
		}
	}
	if failed {
		return exitError(1)
	}
	return nil
}

func ageOf(started time.Time) string {
	seconds := time.Now().Unix() - started.Unix()
	switch {
	case seconds >= 3600:
		return fmt.Sprintf("%dh%02dm", seconds/3600, seconds%3600/60)
	case seconds >= 60:
		return fmt.Sprintf("%dm", seconds/60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// the live sessions of every provider given that lists them, by session name
func liveSessions(providers []provider.Provider) map[string]provider.Live {
	live := map[string]provider.Live{}
	for _, prov := range providers {
		lister, ok := prov.(provider.Lister)
		if !ok {
			continue
		}
		command, _ := provider.CommandOf(prov)
		for _, l := range lister.Live(command) {
			if _, seen := live[l.Name]; !seen {
				live[l.Name] = l
			}
		}
	}
	return live
}

func (e *env) agents(names []string) error {
	projects := projectsOrAll(names)
	var all []*config.Project
	for _, l := range projects {
		if l.err == nil {
			all = append(all, l.p)
		}
	}
	live := liveSessions(providersOf(all))
	rows := [][]string{{"PROJECT", "ROLE", "STATE", "UP", "LINK"}}
	// a nil row is a project that does not load, its error kept in failures under the row
	failures := map[int]error{}
	for _, l := range projects {
		if l.err != nil {
			failures[len(rows)] = l.err
			rows = append(rows, nil)
			continue
		}
		p := l.p
		for _, r := range p.RoleNames() {
			session := p.SessionName(r)
			if !e.host.Has(session) {
				rows = append(rows, []string{p.Name, r, "stopped", "-", "-"})
				continue
			}
			row := live[session]
			state, up := row.State, "-"
			if state == "" {
				state = "between"
				if has(marker(p, r, "running")) {
					state = "starting"
				}
			} else if !row.Started.IsZero() {
				up = ageOf(row.Started)
			}
			link := ""
			if lister, ok := p.Provider().(provider.Lister); ok {
				link = lister.Link(config.Home(), row.Pid)
			}
			if link == "" {
				link = "-"
			}
			rows = append(rows, []string{p.Name, r, state, up, link})
		}
	}
	widths := make([]int, 4)
	for _, row := range rows {
		if row == nil {
			continue
		}
		for i := range widths {
			widths[i] = max(widths[i], len(row[i]))
		}
	}
	for n, row := range rows {
		if row == nil {
			fmt.Fprintf(e.err, "mingo: %v\n", failures[n])
			continue
		}
		var b strings.Builder
		for i := range widths {
			fmt.Fprintf(&b, "%-*s  ", widths[i], row[i])
		}
		b.WriteString(row[4])
		fmt.Fprintln(e.out, b.String())
	}
	if len(failures) > 0 {
		return exitError(1)
	}
	return nil
}

// each project's name and id, which mingo on the owner's computer reads to find a project's host
func (e *env) names() error {
	for _, name := range config.Names() {
		if id := config.IDOf(name); id != "" {
			fmt.Fprintln(e.out, name, id)
		} else {
			fmt.Fprintln(e.out, name)
		}
	}
	return nil
}

func (e *env) ls() error {
	failed := false
	for _, name := range config.Names() {
		p, err := config.Load(name)
		if err != nil {
			fmt.Fprintf(e.err, "mingo: %v\n", err)
			failed = true
			continue
		}
		line := p.Name
		if p.ID != "" {
			line += " [" + p.ID + "]"
		}
		if p.Repo != "" {
			line += " (" + p.Repo + ")"
		}
		fmt.Fprintln(e.out, line)
		for _, r := range p.RoleNames() {
			marks := ""
			if r == p.Lead {
				marks += " lead"
			}
			if p.Autostarts(r) {
				marks += " autostart"
			}
			fmt.Fprintf(e.out, "  %-12s %s%s\n", r, p.CloneDir(r), marks)
		}
	}
	if failed {
		return exitError(1)
	}
	return nil
}

// the names the loop gave the session are the names its tmux session and markers go by, so they
// need no config that loads
func (e *env) clear() error {
	name, role := os.Getenv("MINGO_PROJECT"), os.Getenv("MINGO_ROLE")
	if name == "" || role == "" {
		here := config.Cwd()
		found, ok := config.ProjectOfDir(here)
		if !ok {
			return errors.New("not inside a project's clone, nothing to clear")
		}
		p, err := config.Load(found)
		if err != nil {
			return err
		}
		if role, ok = p.RoleOfDir(here); !ok {
			return errors.New("not inside a project's clone, nothing to clear")
		}
		name = found
	}
	p := &config.Project{Name: name}
	if session := p.SessionName(role); !e.host.Has(session) {
		return fmt.Errorf("%s is not running, so there is no session to clear", session)
	}
	dir := filepath.Join(config.StateDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := touch(filepath.Join(dir, role+".restart")); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "%s/%s: the loop ends this session within seconds and starts a fresh one\n", name, role)
	return nil
}
