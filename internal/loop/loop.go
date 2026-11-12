// Package loop runs a role's sessions one after another, starting a fresh one whenever one ends.
package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/host"
	"github.com/Lindeneg/mingo/internal/provider"
	"github.com/Lindeneg/mingo/internal/sys"
)

const (
	MinRun     = 60
	FirstDelay = 3
	MaxDelay   = 300
	ExitGrace  = 60
	TermGrace  = 10
	WatchEvery = 5
)

func envSeconds(name string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n >= 0 {
		return n
	}
	return fallback
}

type loop struct {
	p          *config.Project
	role       string
	state      string
	name       string
	pane       string
	exe        string
	out        io.Writer
	exitGrace  time.Duration
	termGrace  time.Duration
	watchEvery time.Duration
	exec       func(path string, argv, env []string) error

	mu        sync.Mutex
	child     *exec.Cmd
	done      chan struct{}
	restarted bool
}

func (l *loop) marker(kind string) string {
	return filepath.Join(l.state, l.role+"."+kind)
}

func (l *loop) has(kind string) bool {
	_, err := os.Stat(l.marker(kind))
	return err == nil
}

func (l *loop) touch(kind string) {
	os.WriteFile(l.marker(kind), nil, 0o644)
}

func (l *loop) remove(kinds ...string) {
	for _, k := range kinds {
		os.Remove(l.marker(k))
	}
}

func (l *loop) say(format string, args ...any) {
	fmt.Fprintf(l.out, format, args...)
}

func (l *loop) running() (*exec.Cmd, chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.child, l.done
}

func endedWithin(done chan struct{}, d time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// the exit keys do not always end a session. a background task or a queued draft can make the CLI
// ask a question nobody answers, so the session is given the exit grace, then sigterm, then sigkill.
// the question is never answered with keys, since enter on a draft sends it. the session is this
// loop's own child, not yet waited for, so its pid cannot name another process
func (l *loop) endSession() {
	child, done := l.running()
	if child == nil {
		return
	}
	host.Tmux{Socket: os.Getenv("MINGO_TMUX_SOCKET")}.SendToPane(l.pane, l.p.Provider().ExitKeys()...)
	if endedWithin(done, l.exitGrace) {
		return
	}
	os.WriteFile(l.marker("forced"), []byte("SIGTERM\n"), 0o644)
	if child.Process.Signal(syscall.SIGTERM) != nil {
		l.remove("forced")
	}
	if endedWithin(done, l.termGrace) {
		return
	}
	select {
	case <-done:
	default:
		os.WriteFile(l.marker("forced"), []byte("SIGKILL\n"), 0o644)
		child.Process.Signal(syscall.SIGKILL)
	}
}

func (l *loop) watch(ctx context.Context) {
	tick := time.NewTicker(l.watchEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if l.has("restart") {
			l.remove("restart")
			l.mu.Lock()
			if l.child != nil {
				l.restarted = true
			}
			l.mu.Unlock()
			l.endSession()
		}
	}
}

func (l *loop) args() []string {
	opener := l.p.OpenerFor(l.role)
	if l.p.CanBeAway() && config.Away(l.p.Name) {
		opener += " " + config.AwayOpener
	}
	return l.p.Provider().Args(provider.Session{Opener: opener, Name: l.name, Model: l.p.Model(l.role), Effort: l.p.Effort(l.role), Args: l.p.Args()})
}

func orDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

// one session, its exit code, the signal the loop ended it with if any, and whether the restart
// marker ended it
func (l *loop) session(command string) (int, string, bool) {
	cmd := exec.Command(command, l.args()...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "mingo: %v\n", err)
		return sys.ExitCode(err), "", false
	}
	done := make(chan struct{})
	l.mu.Lock()
	l.child, l.done, l.restarted = cmd, done, false
	l.mu.Unlock()
	os.WriteFile(l.marker("pid"), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644)
	err := cmd.Wait()
	close(done)
	l.mu.Lock()
	restarted := l.restarted
	l.child, l.done = nil, nil
	l.mu.Unlock()
	forced, _ := os.ReadFile(l.marker("forced"))
	return sys.ExitCode(err), strings.TrimSpace(string(forced)), restarted
}

func answersVersion(exe string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "version").Output()
	return err == nil && strings.HasPrefix(string(out), "mingo ")
}

// what a loop needs of the config to run, so a loop starts only on a config it can run on
func prepare(projectName, roleSpec string) (*config.Project, string, error) {
	p, err := config.Find(projectName)
	if err != nil {
		return nil, "", err
	}
	role, err := p.Resolve(roleSpec)
	if err != nil {
		return nil, "", err
	}
	dir := p.CloneDir(role)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, "", fmt.Errorf("%s has no clone at %s, see mingo setup %s", p.SessionName(role), dir, p.Name)
	}
	return p, role, nil
}

// the problem with the file named, as most of the config's own errors already name it
func configProblem(projectName string, err error) string {
	name, ok := config.NameOf(projectName)
	if !ok {
		name = projectName
	}
	file, text := config.File(name), err.Error()
	if !strings.Contains(text, file) {
		text = file + ": " + text
	}
	return text
}

func commandProblem(prov provider.Provider, command string, set bool) error {
	_, err := exec.LookPath(command)
	if err == nil {
		return nil
	}
	if !set {
		return fmt.Errorf("%s is not on PATH, install %s", command, prov.Title())
	}
	var e *exec.Error
	if errors.As(err, &e) {
		err = e.Err
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return fmt.Errorf("%s is %q, which cannot be run: %v", prov.CommandEnv(), command, err)
}

// sleeps for d, and says whether the stop marker came, which ends the sleep within a second
func pause(stop string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if _, err := os.Stat(stop); err == nil {
			return true
		}
		left := time.Until(deadline)
		if left <= 0 {
			return false
		}
		time.Sleep(min(left, time.Second))
	}
}

// waits until ready has nothing against going on, saying in the pane what it has each time that
// changes, and says whether it may go on, which it may not once the stop marker came
func waitUntil(out io.Writer, who, stop string, every time.Duration, ready func() error) bool {
	said := ""
	for {
		err := ready()
		if err == nil {
			return true
		}
		if text := err.Error(); text != said {
			fmt.Fprintf(out, "\n[mingo] %s: %s\n[mingo] %s: the loop tries again every %d s\n", who, text, who, int(every.Seconds()))
			said = text
		}
		if pause(stop, every) {
			return false
		}
	}
}

// what keeps this mingo from running a loop on the config as it is now, for a loop about to start
// it at a session boundary. the answer goes to stdout, where a mingo too old to know the check
// writes nothing
func Check(out io.Writer, projectName, roleSpec string) int {
	if _, _, err := prepare(projectName, roleSpec); err != nil {
		fmt.Fprintln(out, configProblem(projectName, err))
		return 1
	}
	return 0
}

// what the mingo on disk says against running the loop on the config as it is now, and false when
// it gave no answer, as a mingo older than the check does
func checkOnDisk(exe, projectName, role string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "loop", "--check", projectName, role).Output()
	if err == nil {
		return "", true
	}
	var exit *exec.ExitError
	if problem := strings.TrimSpace(string(out)); errors.As(err, &exit) && exit.ExitCode() == 1 && problem != "" {
		return problem, true
	}
	return "", false
}

// the next session starts from the binary on disk now, unless that binary does not answer or would
// not run on the config, which it is asked itself since its checks may differ from this loop's. the
// loop goes on as it is then
func (l *loop) startAgain(delay int) {
	if !answersVersion(l.exe) {
		l.say("[mingo] %s: the mingo on disk does not answer mingo version, continuing with the loaded loop\n", l.name)
		return
	}
	problem, answered := checkOnDisk(l.exe, l.p.Name, l.role)
	if !answered {
		l.say("[mingo] %s: the mingo on disk does not answer mingo loop --check, continuing with the loaded loop\n", l.name)
		return
	}
	if problem != "" {
		l.say("[mingo] %s: %s\n", l.name, problem)
		l.say("[mingo] %s: the loop goes on with the config it loaded and tries the file again at the next session boundary\n", l.name)
		return
	}
	l.remove("running", "pid")
	os.Setenv("MINGO_LOOP_DELAY", strconv.Itoa(delay))
	err := l.exec(l.exe, []string{l.exe, "loop", l.p.Name, l.role}, os.Environ())
	l.say("[mingo] %s: starting the mingo on disk failed: %v, continuing with the loaded loop\n", l.name, err)
}

func Run(projectName, roleSpec string) int {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		fmt.Fprintln(os.Stderr, "mingo: runs inside tmux, use mingo start")
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mingo: %v\n", err)
		return 1
	}
	watchEvery := time.Duration(envSeconds("MINGO_WATCH_EVERY", WatchEvery)) * time.Second
	retryEvery := max(watchEvery, time.Second)

	// ctrl c belongs to the session, so the loop takes sigint and drops it
	var current atomic.Pointer[loop]
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range signals {
			if sig == syscall.SIGINT {
				continue
			}
			if l := current.Load(); l != nil {
				l.remove("running", "pid")
			}
			os.Exit(128 + int(sig.(syscall.Signal)))
		}
	}()

	// a config that cannot be used leaves the pane open with what is wrong in it, until it can be
	var p *config.Project
	var role string
	stop := filepath.Join(config.StateDir(), projectName, roleSpec+".stop")
	if !waitUntil(os.Stdout, projectName+"/"+roleSpec, stop, retryEvery, func() error {
		var err error
		if p, role, err = prepare(projectName, roleSpec); err != nil {
			return errors.New(configProblem(projectName, err))
		}
		return nil
	}) {
		return 0
	}
	l := &loop{
		p: p, role: role, state: p.State(), name: p.SessionName(role), pane: pane, exe: exe, out: os.Stdout,
		exitGrace:  time.Duration(envSeconds("MINGO_EXIT_GRACE", ExitGrace)) * time.Second,
		termGrace:  time.Duration(envSeconds("MINGO_TERM_GRACE", TermGrace)) * time.Second,
		watchEvery: watchEvery,
		exec:       sys.Exec,
	}
	prov := p.Provider()
	command, commandSet := provider.CommandOf(prov)
	delay := envSeconds("MINGO_LOOP_DELAY", FirstDelay)
	os.Setenv("MINGO_PROJECT", p.Name)
	os.Setenv("MINGO_ROLE", role)

	os.MkdirAll(l.state, 0o755)
	if err := os.Chdir(p.CloneDir(role)); err != nil {
		fmt.Fprintf(os.Stderr, "mingo: %v\n", err)
		return 1
	}
	if l.has("stop") {
		return 0
	}
	l.remove("restart", "running", "pid", "forced")

	ctx, stopWatch := context.WithCancel(context.Background())
	defer stopWatch()
	go l.watch(ctx)
	current.Store(l)

	for {
		// a command that cannot be found is waited for, not started and failed again and again
		if !waitUntil(l.out, l.name, l.marker("stop"), retryEvery, func() error { return commandProblem(prov, command, commandSet) }) {
			break
		}
		l.say("\n[mingo] %s: fresh session at %s on %s\n", l.name, time.Now().UTC().Format("2006-01-02T15:04:05Z"), p.DescribeRun(role))
		started := time.Now()
		l.remove("forced")
		l.touch("running")
		if l.has("stop") {
			l.remove("running")
			break
		}
		os.WriteFile(l.marker("using"), []byte(orDefault(p.Model(role))+" "+orDefault(p.Effort(role))+"\n"), 0o644)
		code, forced, restarted := l.session(command)
		l.remove("running", "pid", "forced")
		ran := int(time.Since(started).Seconds())
		l.say("\n[mingo] %s: session ended with code %d after %d s\n", l.name, code, ran)
		if forced != "" {
			l.say("[mingo] %s: %s had not ended it after %d s, a question nobody answered held it, so the loop ended it with %s\n",
				l.name, prov.ExitShown(), int(l.exitGrace.Seconds()), forced)
		}
		if l.has("stop") {
			break
		}
		// a session the restart marker ended was meant to end, so it is no failed start
		var stopped bool
		if ran < MinRun && !restarted {
			l.say("[mingo] %s: ended within %d s, a failed start, next try in %d s\n", l.name, MinRun, delay)
			stopped = pause(l.marker("stop"), time.Duration(delay)*time.Second)
			delay = min(delay*2, MaxDelay)
		} else {
			delay = FirstDelay
			stopped = pause(l.marker("stop"), time.Duration(delay)*time.Second)
		}
		if stopped {
			break
		}
		l.startAgain(delay)
	}
	l.remove("running", "pid")
	return 0
}
