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

// the test binary stands in for the check of the mingo on disk
func TestMain(m *testing.M) {
	if os.Getenv("LOOP_CHECK") == "1" {
		os.Exit(Check(os.Stdout, os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]))
	}
	os.Exit(m.Run())
}

type boundary struct {
	l     *loop
	out   *bytes.Buffer
	file  string
	execs int
}

// a mingo on disk that answers mingo version, and answers mingo loop --check with check
func (b *boundary) onDisk(t *testing.T, check string) {
	t.Helper()
	script := "#!/bin/sh\ncase \"$1\" in\n    version) echo mingo test ;;\n    loop) " + check + " ;;\nesac\n"
	if err := os.WriteFile(b.l.exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// a loop on a project whose config loads, with a mingo on disk that answers mingo version and
// checks the config as this mingo does
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
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "mingo")
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
	b.onDisk(t, "LOOP_CHECK=1 exec '"+self+"' \"$@\"")
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

func TestTheLoopStaysWhenTheMingoOnDiskRefusesAConfigThisMingoTakes(t *testing.T) {
	b := newBoundary(t)
	b.onDisk(t, `echo "the config has the key nope"; exit 1`)
	b.l.startAgain(3)
	want := "[mingo] demo-lead: the config has the key nope\n" +
		"[mingo] demo-lead: the loop goes on with the config it loaded and tries the file again at the next session boundary\n"
	if b.execs != 0 || b.out.String() != want {
		t.Fatal(b.execs, b.out.String())
	}
}

func TestTheLoopHandsOverToAMingoOnDiskThatTakesAConfigThisMingoRefuses(t *testing.T) {
	b := newBoundary(t)
	b.onDisk(t, "exit 0")
	if err := os.WriteFile(b.file, []byte("[[roles]\nname = \"lead\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.l.startAgain(3)
	if b.execs != 1 {
		t.Fatal(b.execs, b.out.String())
	}
}

func TestTheLoopStaysWithAMingoOnDiskTooOldToAnswerTheCheck(t *testing.T) {
	for _, old := range []string{
		`echo "usage: mingo loop <project> <role>" >&2; exit 2`,
		`echo "mingo: loop <project> <role>" >&2; exit 1`,
	} {
		b := newBoundary(t)
		b.onDisk(t, old)
		b.l.startAgain(3)
		if b.execs != 0 || b.out.String() != "[mingo] demo-lead: the mingo on disk does not answer mingo loop --check, continuing with the loaded loop\n" {
			t.Fatal(old, b.execs, b.out.String())
		}
	}
}

func TestCheckPrintsNothingForAConfigItTakesAndTheProblemWithTheFileForOneItRefuses(t *testing.T) {
	b := newBoundary(t)
	var out bytes.Buffer
	if code := Check(&out, "demo", "lead"); code != 0 || out.String() != "" {
		t.Fatal(code, out.String())
	}
	if code := Check(&out, "demo", "dev"); code != 1 || out.String() != b.file+": demo has no role or alias dev, it has lead\n" {
		t.Fatal(code, out.String())
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
