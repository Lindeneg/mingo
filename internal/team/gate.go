package team

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const GateTargetTimeout = 30 * time.Minute

const (
	ExitOK     = 0
	ExitFailed = 1
	ExitUsage  = 2
)

type GateDeps struct {
	Root      string
	Targets   []string
	RunTarget func(target, logPath string) (Result, error)
	Out       io.Writer
	Now       func() time.Time
}

func RealTarget(root string) func(target, logPath string) (Result, error) {
	return func(target, logPath string) (Result, error) {
		fields := strings.Fields(target)
		return RunIntoLog(Options{Dir: root, Timeout: GateTargetTimeout}, logPath, fields[0], fields[1:]...)
	}
}

func describeRed(target string, r Result) string {
	switch {
	case r.NotFound:
		return strings.Fields(target)[0] + " was not found"
	case r.TimedOut:
		return fmt.Sprintf("stopped after %d minutes", int(GateTargetTimeout.Minutes()))
	}
	return fmt.Sprintf("exit %d", r.Code)
}

func Gate(d GateDeps, args []string) (int, error) {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(d.Out, "usage: mingo gate <log>")
		return ExitUsage, nil
	}
	if len(d.Targets) == 0 {
		fmt.Fprintln(d.Out, "the gate has no targets: name them in the project's [workflow] gate")
		return ExitFailed, nil
	}
	logPath := args[0]
	if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(d.Root, logPath)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return ExitFailed, err
	}
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		return ExitFailed, err
	}
	for _, target := range d.Targets {
		if err := appendFile(logPath, "\n== "+target+"\n"); err != nil {
			return ExitFailed, err
		}
		started := d.Now()
		r, err := d.RunTarget(target, logPath)
		if err != nil {
			return ExitFailed, err
		}
		seconds := int(d.Now().Sub(started).Round(time.Second).Seconds())
		if !r.OK() {
			fmt.Fprintf(d.Out, "red    %s (%s, %d s)\n", target, describeRed(target, r), seconds)
			fmt.Fprintf(d.Out, "the gate is red at %s; its output is in %s\n", target, logPath)
			return ExitFailed, nil
		}
		fmt.Fprintf(d.Out, "green  %s (%d s)\n", target, seconds)
	}
	fmt.Fprintf(d.Out, "the gate is green: %s; the log is %s\n", strings.Join(d.Targets, ", "), logPath)
	return ExitOK, nil
}

func appendFile(path, text string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
