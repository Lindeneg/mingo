//go:build unix

package loop

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
)

type boundary struct {
	l     *loop
	out   *bytes.Buffer
	file  string
	execs int
}

// a loop on a project whose config loads, with a mingo on disk that answers mingo version
func newBoundary(t *testing.T) *boundary {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MINGO_CONFIG", filepath.Join(root, "config"))
	t.Setenv("MINGO_STATE", filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("MINGO_LOOP_DELAY", "")
	clone := filepath.Join(root, "clone")
	file := filepath.Join(root, "config", "projects", "demo.toml")
	for _, dir := range []string{clone, filepath.Dir(file)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(file, []byte("clone = \""+clone+"\"\n[[roles]]\nname = \"lead\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "mingo")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho mingo test\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := config.Find("demo")
	if err != nil {
		t.Fatal(err)
	}
	b := &boundary{out: &bytes.Buffer{}, file: file}
	b.l = &loop{p: p, role: "lead", state: p.State(), name: p.SessionName("lead"), exe: exe, out: b.out, watchEvery: time.Second,
		exec: func(path string, argv, env []string) error {
			b.execs++
			return errors.New("exec format error")
		}}
	return b
}

func TestTheLoopSaysWhenTheMingoOnDiskDoesNotStartAndGoesOn(t *testing.T) {
	b := newBoundary(t)
	b.l.startAgain(3)
	if b.execs != 1 || b.out.String() != "[mingo] demo-lead: starting the mingo on disk failed: exec format error, continuing with the loaded loop\n" {
		t.Fatal(b.execs, b.out.String())
	}
}

func TestTheLoopStartsNoMingoOnAConfigThatNoLongerLoadsAndSaysWhichFile(t *testing.T) {
	b := newBoundary(t)
	if err := os.WriteFile(b.file, []byte("[[roles]\nname = \"lead\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.l.startAgain(3)
	lines := strings.Split(strings.TrimSuffix(b.out.String(), "\n"), "\n")
	if b.execs != 0 || len(lines) != 2 || !strings.HasPrefix(lines[0], "[mingo] demo-lead: "+b.file+": ") ||
		lines[1] != "[mingo] demo-lead: the loop goes on with the config it loaded and tries the file again at the next session boundary" {
		t.Fatal(b.execs, b.out.String())
	}
}

func TestTheLoopStartsNoMingoWhenTheRolesCloneIsGone(t *testing.T) {
	b := newBoundary(t)
	os.WriteFile(b.file, []byte("clone = \"/nowhere/{role}\"\n[[roles]]\nname = \"lead\"\n"), 0o644)
	b.l.startAgain(3)
	if b.execs != 0 || !strings.Contains(b.out.String(), b.file+": demo-lead has no clone at /nowhere/lead, see mingo setup demo") {
		t.Fatal(b.out.String())
	}
}

func TestClaudeProblemSaysWhatIsMissingAndWhatToDo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := claudeProblem("claude", false); err == nil || err.Error() != "claude is not on PATH, install Claude Code" {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "claude")
	if err := claudeProblem(missing, true); err == nil || err.Error() != `MINGO_CLAUDE is "`+missing+`", which cannot be run: no such file or directory` {
		t.Fatal(err)
	}
	os.WriteFile(missing, []byte("#!/bin/sh\n"), 0o755)
	if err := claudeProblem(missing, true); err != nil {
		t.Fatal(err)
	}
}

func TestPauseEndsWithinASecondOfTheStopMarker(t *testing.T) {
	stop := filepath.Join(t.TempDir(), "lead.stop")
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.WriteFile(stop, nil, 0o644)
	}()
	started := time.Now()
	if !pause(stop, time.Minute) || time.Since(started) > 3*time.Second {
		t.Fatal(time.Since(started))
	}
	if pause(filepath.Join(t.TempDir(), "none"), 100*time.Millisecond) {
		t.Fatal("stopped without a marker")
	}
}
