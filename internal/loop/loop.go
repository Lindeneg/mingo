// Package loop runs a role's sessions one after another, starting a fresh one whenever one ends.
package loop

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/host"
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

	mu    sync.Mutex
	child *exec.Cmd
	done  chan struct{}
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

// /exit does not always end a session. a background task or a queued feedback draft makes claude
// ask a question nobody answers, so the session is given the exit grace, then sigterm, then sigkill.
// the question is never answered with keys, since enter on a draft sends it. the session is this
// loop's own child, not yet waited for, so its pid cannot name another process
func (l *loop) endSession() {
	child, done := l.running()
	if child == nil {
		return
	}
	host.Tmux{Socket: os.Getenv("MINGO_TMUX_SOCKET")}.SendToPane(l.pane, "/exit", "Enter")
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
			l.endSession()
		}
	}
}

func (l *loop) args() []string {
	args := []string{l.p.OpenerFor(l.role), "--name", l.name}
	if m := l.p.Model(l.role); m != "" {
		args = append(args, "--model", m)
	}
	if e := l.p.Effort(l.role); e != "" {
		args = append(args, "--effort", e)
	}
	return append(args, l.p.ClaudeArgs...)
}

func orDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

// one session, its exit code and the signal the loop ended it with, if any
func (l *loop) session(claude string) (int, string) {
	cmd := exec.Command(claude, l.args()...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "mingo: %v\n", err)
		return sys.ExitCode(err), ""
	}
	done := make(chan struct{})
	l.mu.Lock()
	l.child, l.done = cmd, done
	l.mu.Unlock()
	os.WriteFile(l.marker("pid"), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644)
	err := cmd.Wait()
	close(done)
	l.mu.Lock()
	l.child, l.done = nil, nil
	l.mu.Unlock()
	forced, _ := os.ReadFile(l.marker("forced"))
	return sys.ExitCode(err), strings.TrimSpace(string(forced))
}

func answersVersion(exe string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "version").Output()
	return err == nil && strings.HasPrefix(string(out), "mingo ")
}

func Run(projectName, roleSpec string) int {
	p, err := config.Find(projectName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mingo: %v\n", err)
		return 1
	}
	role, err := p.Resolve(roleSpec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mingo: %v\n", err)
		return 2
	}
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
	l := &loop{
		p: p, role: role, state: p.State(), name: p.SessionName(role), pane: pane, exe: exe, out: os.Stdout,
		exitGrace:  time.Duration(envSeconds("MINGO_EXIT_GRACE", ExitGrace)) * time.Second,
		termGrace:  time.Duration(envSeconds("MINGO_TERM_GRACE", TermGrace)) * time.Second,
		watchEvery: time.Duration(envSeconds("MINGO_WATCH_EVERY", WatchEvery)) * time.Second,
	}
	claude := os.Getenv("MINGO_CLAUDE")
	if claude == "" {
		claude = "claude"
	}
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
	cleanup := func() {
		l.remove("running", "pid")
	}

	// ctrl c belongs to the session, so the loop takes sigint and drops it
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range signals {
			if sig == syscall.SIGINT {
				continue
			}
			cleanup()
			os.Exit(128 + int(sig.(syscall.Signal)))
		}
	}()

	for {
		l.say("\n[mingo] %s: fresh session at %s on %s\n", l.name, time.Now().UTC().Format("2006-01-02T15:04:05Z"), p.DescribeRun(role))
		started := time.Now()
		l.remove("forced")
		l.touch("running")
		if l.has("stop") {
			l.remove("running")
			break
		}
		os.WriteFile(l.marker("using"), []byte(orDefault(p.Model(role))+" "+orDefault(p.Effort(role))+"\n"), 0o644)
		code, forced := l.session(claude)
		l.remove("running", "pid", "forced")
		ran := int(time.Since(started).Seconds())
		l.say("\n[mingo] %s: session ended with code %d after %d s\n", l.name, code, ran)
		if forced != "" {
			l.say("[mingo] %s: /exit had not ended it after %d s, a question nobody answered held it, so the loop ended it with %s\n",
				l.name, int(l.exitGrace.Seconds()), forced)
		}
		if l.has("stop") {
			break
		}
		if ran < MinRun {
			l.say("[mingo] %s: ended within %d s, a failed start, next try in %d s\n", l.name, MinRun, delay)
			time.Sleep(time.Duration(delay) * time.Second)
			delay = min(delay*2, MaxDelay)
		} else {
			delay = FirstDelay
			time.Sleep(time.Duration(delay) * time.Second)
		}
		if l.has("stop") {
			break
		}
		// the next session starts from the binary on disk now, unless what is there does not answer
		if answersVersion(l.exe) {
			cleanup()
			os.Setenv("MINGO_LOOP_DELAY", strconv.Itoa(delay))
			err := sys.Exec(l.exe, []string{l.exe, "loop", p.Name, role}, os.Environ())
			fmt.Fprintf(os.Stderr, "mingo: %v\n", err)
		}
		l.say("[mingo] %s: the mingo on disk does not answer mingo version, continuing with the loaded loop\n", l.name)
	}
	cleanup()
	return 0
}
