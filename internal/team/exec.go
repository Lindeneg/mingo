package team

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"

	"github.com/Lindeneg/mingo/internal/sys"
)

type Result struct {
	Code     int
	Stdout   string
	Stderr   string
	NotFound bool
	TimedOut bool
}

func (r Result) OK() bool {
	return !r.NotFound && !r.TimedOut && r.Code == 0
}

type Options struct {
	Dir     string
	Timeout time.Duration
	Env     []string
	Stdin   string
}

// how long a command a signal reached has to end before its group is killed
const stopGrace = 5 * time.Second

// a signal that ended a command, passed up so that what ran it ends where it stands
type Interrupted struct {
	Signal os.Signal
}

func (i *Interrupted) Error() string {
	return "stopped by " + signalName(i.Signal)
}

// the exit code a shell gives a process the signal ended
func (i *Interrupted) Code() int {
	if s, ok := i.Signal.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return ExitFailed
}

func signalName(sig os.Signal) string {
	switch sig {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGHUP:
		return "SIGHUP"
	}
	return sig.String()
}

// how many callers here catch the stop signals to clean up before they end, so a signal that
// ends a command leaves them the end
var cleaning atomic.Int32

// a signal this process was started with ignored stays ignored, since catching it would undo that
func catch() (chan os.Signal, func()) {
	signals := make(chan os.Signal, 4)
	var caught []os.Signal
	for _, s := range []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(s) {
			caught = append(caught, s)
		}
	}
	if len(caught) > 0 {
		signal.Notify(signals, caught...)
	}
	return signals, func() { signal.Stop(signals) }
}

// without a terminal the command and what it starts share a process group of their own, so a
// timeout ends every one of them, and a signal to this process reaches the group too. at a terminal
// the command stays in this process's group, the terminal's foreground one, so a prompt of git or
// ssh can read the terminal, and ctrl c reaches every process the command started from the terminal
// itself. either way the signal then ends this process as it would have alone, unless a caller here
// cleans up first. the error is only for a program that cannot start for another reason than not
// being installed, and for that signal
func run(o Options, stdout, stderr io.Writer, name string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = o.Dir
	if len(o.Env) > 0 {
		cmd.Env = append(os.Environ(), o.Env...)
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if o.Stdin != "" {
		cmd.Stdin = strings.NewReader(o.Stdin)
	}
	terminal := sys.IsTerminal(os.Stdin.Fd())
	forward, kill := sys.SignalGroup, sys.KillGroup
	if terminal {
		forward = func(cmd *exec.Cmd, sig os.Signal) error {
			// the terminal sent ctrl c to the command as well, and a second one may cut its cleanup short
			if sig == syscall.SIGINT {
				return nil
			}
			return cmd.Process.Signal(sig)
		}
		kill = func(cmd *exec.Cmd) error { return cmd.Process.Kill() }
	} else {
		sys.OwnGroup(cmd)
	}
	cmd.Cancel = func() error { return kill(cmd) }
	cmd.WaitDelay = 5 * time.Second
	signals, stopCatching := catch()
	defer stopCatching()
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Result{NotFound: true}, nil
		}
		return Result{}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case sig := <-signals:
		forward(cmd, sig)
		select {
		case <-done:
		case <-time.After(stopGrace):
			kill(cmd)
			<-done
		}
		stopCatching()
		if cleaning.Load() == 0 {
			sys.Raise(sig)
		}
		return Result{}, &Interrupted{Signal: sig}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return Result{TimedOut: true}, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code := exit.ExitCode()
		if code < 0 {
			code = 1
		}
		return Result{Code: code}, nil
	}
	// the command exited 0 and what it left running in the background still holds its output
	if errors.Is(err, exec.ErrWaitDelay) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	return Result{}, nil
}

func Capture(o Options, name string, args ...string) (Result, error) {
	var stdout, stderr bytes.Buffer
	r, err := run(o, &stdout, &stderr, name, args...)
	r.Stdout, r.Stderr = stdout.String(), stderr.String()
	return r, err
}

func RunIntoLog(o Options, logPath, name string, args ...string) (Result, error) {
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return Result{}, err
	}
	defer log.Close()
	return run(o, log, log, name, args...)
}

func LastLines(text string, count int) string {
	lines := strings.Split(strings.TrimRightFunc(text, unicode.IsSpace), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, "\n")
}

// a stop ends a command on something its user can put right, and its message is the one line shown
type Stop struct {
	Message string
}

func (s *Stop) Error() string {
	return s.Message
}

func stop(message string) error {
	return &Stop{Message: message}
}
