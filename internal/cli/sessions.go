package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
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
	for _, v := range []string{"MINGO_CONFIG", "MINGO_STATE", "MINGO_TMUX_SOCKET", "MINGO_CLAUDE", "MINGO_EXIT_GRACE",
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

func (e *env) start(args []string) error {
	onlyAutostart := len(args) > 0 && args[0] == "--autostart"
	if onlyAutostart {
		args = args[1:]
	}
	specs, err := specsOrDefault(args)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		p, role, err := config.ParseTarget(spec)
		if err != nil {
			return err
		}
		var picked []string
		switch {
		case role != "":
			picked = []string{role}
		case onlyAutostart:
			picked = p.Autostart
			if len(picked) == 0 {
				fmt.Fprintf(e.out, "%s: no autostart roles, nothing to start\n", p.Name)
			}
		default:
			picked = p.RoleNames()
		}
		for _, r := range picked {
			if err := e.startRole(p, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *env) stop(args []string) error {
	specs, err := specsOrDefault(args)
	if err != nil {
		return err
	}
	var asked []string
	for _, spec := range specs {
		p, role, err := config.ParseTarget(spec)
		if err != nil {
			return err
		}
		picked := p.RoleNames()
		if role != "" {
			picked = []string{role}
		}
		if err := os.MkdirAll(p.State(), 0o755); err != nil {
			return err
		}
		for _, r := range picked {
			session := p.SessionName(r)
			if !e.host.Has(session) {
				fmt.Fprintf(e.out, "%s: not running\n", session)
				continue
			}
			touch(marker(p, r, "stop"))
			// the loop types /exit only while a session runs, so between sessions the stop marker alone ends it
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
	force := len(args) > 0 && args[0] == "--force"
	if force {
		args = args[1:]
	}
	spec := ""
	if len(args) > 0 {
		spec = args[0]
	}
	p, role, err := config.ParseRoleTarget(spec)
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
		fmt.Fprintf(e.out, "%s: /exit within seconds ends the session where it stands, then the loop starts a fresh one\n", session)
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
	fmt.Fprintf(e.out, "%s: interrupted, /exit follows within seconds and the session's process is ended if that does not end it\n", session)
	return nil
}

func (e *env) attach(spec string, readOnly bool) error {
	p, role, err := config.ParseRoleTarget(spec)
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
	p, role, err := config.ParseRoleTarget(spec)
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
		return errors.New("say <target> <message>")
	}
	p, role, err := config.ParseRoleTarget(args[0])
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

func projectsOrAll(names []string) ([]*config.Project, error) {
	if len(names) == 0 {
		names = config.Names()
	}
	var projects []*config.Project
	for _, name := range names {
		p, err := config.Find(name)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (e *env) status(names []string) error {
	projects, err := projectsOrAll(names)
	if err != nil {
		return err
	}
	for _, p := range projects {
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
	cmd := exec.Command("claude", "auth", "status", "--text")
	cmd.Stdout = e.out
	if cmd.Run() != nil {
		fmt.Fprintln(e.out, "claude: not logged in on this host")
	}
	return nil
}

func ageOf(startedMs int64) string {
	seconds := time.Now().Unix() - startedMs/1000
	switch {
	case seconds >= 3600:
		return fmt.Sprintf("%dh%02dm", seconds/3600, seconds%3600/60)
	case seconds >= 60:
		return fmt.Sprintf("%dm", seconds/60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

type agentRow struct {
	Name      string `json:"name"`
	Pid       int    `json:"pid"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"`
}

func liveAgents() []agentRow {
	out, err := exec.Command("claude", "agents", "--json").Output()
	if err != nil {
		return nil
	}
	var rows []agentRow
	if json.Unmarshal(out, &rows) != nil {
		return nil
	}
	return rows
}

// the Remote Control id is in the session's own file, the TUI's scrollback does not keep it
func remoteLink(pid int) string {
	if pid <= 0 {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(config.Home(), ".claude", "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return ""
	}
	var s struct {
		BridgeSessionID string `json:"bridgeSessionId"`
	}
	if json.Unmarshal(data, &s) != nil || s.BridgeSessionID == "" {
		return ""
	}
	return "https://claude.ai/code/" + s.BridgeSessionID
}

func (e *env) agents(names []string) error {
	projects, err := projectsOrAll(names)
	if err != nil {
		return err
	}
	live := liveAgents()
	rows := [][]string{{"PROJECT", "ROLE", "STATE", "UP", "LINK"}}
	for _, p := range projects {
		for _, r := range p.RoleNames() {
			session := p.SessionName(r)
			if !e.host.Has(session) {
				rows = append(rows, []string{p.Name, r, "stopped", "-", "-"})
				continue
			}
			var row agentRow
			for _, a := range live {
				if a.Name == session {
					row = a
					break
				}
			}
			state, up := row.Status, "-"
			if state == "" {
				state = "between"
				if has(marker(p, r, "running")) {
					state = "starting"
				}
			} else if row.StartedAt > 0 {
				up = ageOf(row.StartedAt)
			}
			link := remoteLink(row.Pid)
			if link == "" {
				link = "-"
			}
			rows = append(rows, []string{p.Name, r, state, up, link})
		}
	}
	widths := make([]int, 4)
	for _, row := range rows {
		for i := range widths {
			widths[i] = max(widths[i], len(row[i]))
		}
	}
	for _, row := range rows {
		var b strings.Builder
		for i := range widths {
			fmt.Fprintf(&b, "%-*s  ", widths[i], row[i])
		}
		b.WriteString(row[4])
		fmt.Fprintln(e.out, b.String())
	}
	return nil
}

func (e *env) ls() error {
	for _, name := range config.Names() {
		p, err := config.Load(name)
		if err != nil {
			return err
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
	return nil
}

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
