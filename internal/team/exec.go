package team

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
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

// the error is only for a program that cannot start for another reason than not being installed
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
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if errors.Is(err, exec.ErrNotFound) {
		return Result{NotFound: true}, nil
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
