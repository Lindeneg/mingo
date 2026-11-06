//go:build unix

package kit

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type shimRun struct {
	t    *testing.T
	node string
	shim string
	bin  string
	dir  string
}

// the acme project's rendered shim, run by node with only bin on PATH
func newShimRun(t *testing.T) *shimRun {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	shim, err := filepath.Abs(filepath.Join("testdata", "acme", "repo", "tools", "heavy.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	// node reads the shim, so go test sees a change to it only through this stat
	if _, err := os.Stat(shim); err != nil {
		t.Fatal(err)
	}
	s := &shimRun{t: t, node: node, shim: shim, bin: t.TempDir(), dir: t.TempDir()}
	s.script("tool", "printf '%s\\n' \"$@\" > '"+filepath.Join(s.dir, "tool.args")+"'\nexit \"${TOOL_EXIT:-0}\"\n")
	return s
}

func (s *shimRun) script(name, body string) {
	s.t.Helper()
	if err := os.WriteFile(filepath.Join(s.bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		s.t.Fatal(err)
	}
}

// a stand in for mingo heavy that records its arguments and runs the command after them at once
func (s *shimRun) mingo() {
	s.script("mingo", "printf '%s\\n' \"$@\" > '"+filepath.Join(s.dir, "mingo.args")+"'\nshift 3\nexec \"$@\"\n")
}

func (s *shimRun) run(env []string, args ...string) (int, string) {
	s.t.Helper()
	cmd := exec.Command(s.node, append([]string{s.shim}, args...)...)
	cmd.Dir = s.dir
	cmd.Env = append([]string{"PATH=" + s.bin, "HOME=" + s.dir}, env...)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		s.t.Fatal(err)
	}
	return cmd.ProcessState.ExitCode(), string(out)
}

func (s *shimRun) args(name string) string {
	data, err := os.ReadFile(filepath.Join(s.dir, name+".args"))
	if err != nil {
		return "(did not run)"
	}
	return string(data)
}

func TestTheShimRunsTheCommandUnderMingoHeavyWithItsFlagsAndPassesItsExitCodeOn(t *testing.T) {
	s := newShimRun(t)
	s.mingo()
	code, out := s.run([]string{"TOOL_EXIT=7"}, "test", "--", "tool", "--reporter=verbose", "--", "x")
	if code != 7 {
		t.Fatalf("exit %d, want 7:\n%s", code, out)
	}
	if got := s.args("mingo"); got != "heavy\ntest\n--\ntool\n--reporter=verbose\n--\nx\n" {
		t.Fatalf("mingo got %q", got)
	}
	if got := s.args("tool"); got != "--reporter=verbose\n--\nx\n" {
		t.Fatalf("the command got %q", got)
	}
}

func TestTheShimExitsWith128PlusTheSignalThatKilledTheCommand(t *testing.T) {
	s := newShimRun(t)
	s.mingo()
	s.script("killed", "kill -TERM $$\n")
	if code, out := s.run(nil, "test", "--", "killed"); code != 128+15 {
		t.Fatalf("exit %d, want 143:\n%s", code, out)
	}
}

func TestTheShimRunsTheCommandWithoutASlotWhereMingoIsMissing(t *testing.T) {
	s := newShimRun(t)
	code, out := s.run([]string{"TOOL_EXIT=3"}, "test", "--", "tool", "--watch=false")
	if code != 3 {
		t.Fatalf("exit %d, want 3:\n%s", code, out)
	}
	if got := s.args("tool"); got != "--watch=false\n" {
		t.Fatalf("the command got %q", got)
	}
}

func TestTheShimFailsOnAnyOtherErrorStartingMingoAndRunsNothing(t *testing.T) {
	s := newShimRun(t)
	if err := os.WriteFile(filepath.Join(s.bin, "mingo"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := s.run(nil, "test", "--", "tool")
	if code != 1 || !strings.Contains(out, "EACCES") {
		t.Fatalf("exit %d, want 1 with EACCES:\n%s", code, out)
	}
	if got := s.args("tool"); got != "(did not run)" {
		t.Fatalf("the command ran with %q", got)
	}
}

func TestTheShimExitsWith2AndItsUsageOnABadCommandLine(t *testing.T) {
	s := newShimRun(t)
	s.mingo()
	for _, args := range [][]string{nil, {"test"}, {"test", "tool"}, {"test", "tool", "x"}, {"test", "--"}} {
		code, out := s.run(nil, args...)
		if code != 2 || !strings.Contains(out, "heavy: usage: node tools/heavy.mjs <label> -- <command> [args]") {
			t.Fatalf("%q exits %d:\n%s", args, code, out)
		}
	}
	if got := s.args("mingo"); got != "(did not run)" {
		t.Fatalf("mingo ran with %q", got)
	}
}
