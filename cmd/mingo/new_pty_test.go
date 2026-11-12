//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// mingo run on a terminal of its own, which the test types into and reads
type terminal struct {
	t      *testing.T
	master *os.File
	slave  *os.File
	cmd    *exec.Cmd
	mu     sync.Mutex
	raw    []byte
	seen   int
}

func (h *home) onTerminal(env []string, args ...string) *terminal {
	h.t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		h.t.Skip("no pseudo terminal here:", err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		h.t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		h.t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 50, Col: 200}); err != nil {
		h.t.Fatal(err)
	}
	cmd := exec.Command(h.bin, args...)
	cmd.Env = append(append(h.env(), "TERM=xterm-256color"), env...)
	cmd.Dir = h.root
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	term := &terminal{t: h.t, master: master, slave: slave, cmd: cmd}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			term.mu.Lock()
			term.raw = append(term.raw, buf[:n]...)
			term.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	h.t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		master.Close()
		slave.Close()
	})
	return term
}

// the text shown from raw on, without escapes and carriage returns, with where each of its bytes is in raw
func shownFrom(raw []byte, from int) (string, []int) {
	var b []byte
	var at []int
	for i := from; i < len(raw); i++ {
		switch {
		case raw[i] == 0x1b && i+1 < len(raw) && raw[i+1] == '[':
			j := i + 2
			for j < len(raw) && (raw[j] < 0x40 || raw[j] > 0x7e) {
				j++
			}
			i = j
		case raw[i] == '\r':
		default:
			b = append(b, raw[i])
			at = append(at, i)
		}
	}
	return string(b), at
}

// waits until text is shown after what the last wait saw
func (term *terminal) expect(text string) {
	term.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		term.mu.Lock()
		shown, at := shownFrom(term.raw, term.seen)
		if i := strings.Index(shown, text); i >= 0 {
			term.seen = at[i+len(text)-1] + 1
			term.mu.Unlock()
			return
		}
		term.mu.Unlock()
		if time.Now().After(deadline) {
			term.t.Fatalf("waited for %q, the terminal shows\n%s", text, shown)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (term *terminal) send(keys string) {
	term.t.Helper()
	if _, err := term.master.Write([]byte(keys)); err != nil {
		term.t.Fatal(err)
	}
}

func (term *terminal) exit() int {
	term.t.Helper()
	done := make(chan error, 1)
	go func() { done <- term.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		term.t.Fatal("mingo did not end")
	}
	return term.cmd.ProcessState.ExitCode()
}

// whether the terminal reads lines and echoes them again, as it did before mingo
func (term *terminal) cooked() bool {
	t, err := unix.IoctlGetTermios(int(term.slave.Fd()), unix.TCGETS)
	return err == nil && t.Lflag&unix.ICANON != 0 && t.Lflag&unix.ECHO != 0 && t.Lflag&unix.ISIG != 0
}

func TestNewAtATerminalAsksAgainUntilEachAnswerHoldsAndWritesNothingWhenDeclined(t *testing.T) {
	h := newHome(t)
	h.fakeRepoOnly()
	file := filepath.Join(h.config, "projects", "demo.toml")
	term := h.onTerminal(nil, "new", "demo", "--tracker-kind", "linear2")
	term.expect("? the preset")
	term.expect("> bare  sessions and guards")
	term.send("jj\r")
	term.expect("✓ the preset: team")
	term.expect("? the GitHub repository, as owner/name: ")
	term.send("\r")
	term.expect("  this needs an answer")
	term.send("foo\r")
	term.expect(`  "foo" is not owner/name, such as you/shop`)
	term.send("\x15x/nope\r")
	term.expect("  there is no repository x/nope on GitHub, or gh's account cannot see it")
	term.send("\x15x/demo\r")
	term.expect("✓ the GitHub repository, as owner/name: x/demo")
	term.expect("? the project's display name (demo): ")
	term.send("\r")
	term.expect("? the roles, in the order they start (lead): ")
	term.send("lead,Dev\r")
	term.expect(`  "Dev" is not a role name, use lowercase letters, digits and hyphens`)
	term.send("\x7f\x7f\x7fdev\r")
	term.expect("✓ the roles, in the order they start: lead,dev")
	term.expect("? the role that merges (lead): ")
	term.send("\r")
	term.expect("? where the agent files go")
	term.send("j\r")
	term.expect("✓ where the agent files go: local")
	term.expect("? how CI installs the project")
	term.send("\r")
	term.expect("? the CI jobs every pull request must pass, such as test or build (check, test, build, security): ")
	term.send("\r")
	term.expect("? the package.json scripts too heavy for every session to run at once, which then take turns (check, test, build): ")
	term.send("check, dev\r")
	term.expect("  the package.json of x/demo has no script dev")
	term.send("\x15\r")
	term.expect("✓ the package.json scripts too heavy for every session to run at once, which then take turns: check, test, build")
	term.expect("(README.md first): ")
	term.send("\r")
	term.expect("? the tracker")
	term.expect(`  "linear2" is not one of linear, github or none, pick one below`)
	term.expect("> github  GitHub Issues, with a label per role")
	term.send("\r")
	term.expect("✓ the tracker: github")
	term.expect("? the base branch (master): ")
	term.send("\r")
	term.expect("? the commands a role runs before it opens a pull request: ")
	term.send("pnpm check && pnpm test\r")
	term.expect("which mingo runs without a shell, so && does not work there")
	term.send("\x15\r")
	term.expect("✓ the commands a role runs before it opens a pull request: none")
	term.expect("? the role tags a pull request description starts with, such as (lead): ")
	term.send("\r")
	term.expect("\nmingo new will\n  write the config " + file + "\n  clone x/demo into ~/dev/demo/lead and ~/dev/demo/dev\n  open a pull request on x/demo with the kit's files\n  write the agent files into each clone, out of git\n")
	term.expect("? Continue? (Y/n) ")
	term.send("n")
	term.expect("✓ Continue? no\nmingo: nothing was written")
	if code := term.exit(); code != 1 || exists(file) || !term.cooked() {
		t.Fatal(code, exists(file), term.cooked())
	}
}

func TestCtrlCAtATerminalPutsItBackAndWritesNothing(t *testing.T) {
	h := newHome(t)
	h.fakeRepoOnly()
	file := filepath.Join(h.config, "projects", "demo.toml")
	term := h.onTerminal(nil, "new", "demo", "--preset", "bare")
	term.expect("? the GitHub repository, as owner/name: ")
	term.send("x/de")
	term.expect("x/de")
	term.send("\x03")
	term.expect("mingo: stopped, nothing was written")
	if code := term.exit(); code != 130 || exists(file) || !term.cooked() {
		t.Fatal(code, exists(file), term.cooked())
	}
	lines := h.onTerminal([]string{"TERM=dumb"}, "new", "demo", "--preset", "bare")
	lines.expect("the GitHub repository, as owner/name: ")
	lines.send("\x03")
	lines.expect("mingo: stopped, nothing was written")
	if code := lines.exit(); code != 130 || exists(file) || !lines.cooked() {
		t.Fatal(code, exists(file), lines.cooked())
	}
}

func TestCtrlCWhileGhChecksAnAnswerStopsAtOnceAndWritesNothing(t *testing.T) {
	h := newHome(t)
	started := filepath.Join(h.root, "gh-started")
	h.fakeTool("gh", `case "$1 $2" in
  "repo view") echo $$ >`+shellQuote(started)+`; sleep 30; exit 0 ;;
  *) exit 1 ;;
esac
`)
	// the slow gh leads a process group of its own, which mingo leaves to end by itself
	t.Cleanup(func() {
		if pid, err := strconv.Atoi(strings.TrimSpace(h.read(started))); err == nil && pid > 1 {
			syscall.Kill(-pid, syscall.SIGKILL)
		}
	})
	file := filepath.Join(h.config, "projects", "demo.toml")
	term := h.onTerminal(nil, "new", "demo", "--preset", "bare")
	term.expect("? the GitHub repository, as owner/name: ")
	term.send("x/slow\r")
	if !waitFor(15, func() bool { return strings.TrimSpace(h.read(started)) != "" }) {
		t.Fatal("gh did not start")
	}
	at := time.Now()
	term.send("\x03")
	term.expect("mingo: stopped, nothing was written")
	if code := term.exit(); code != 130 || exists(file) || !term.cooked() || time.Since(at) > 5*time.Second {
		t.Fatal(code, exists(file), term.cooked(), time.Since(at))
	}
}

func TestNewAtATerminalGoesOnWhenToldAndMakesTheProject(t *testing.T) {
	h := newHome(t)
	h.write(filepath.Join(h.home, ".gitconfig"), "[user]\n\tname = Kit Test\n\temail = kit@example.test\n[commit]\n\tgpgsign = false\n")
	origin := filepath.Join(h.root, "origin.git")
	seed := filepath.Join(h.root, "seed")
	h.gitIn(h.root, "init", "--quiet", "--bare", "--initial-branch=master", origin)
	h.gitIn(h.root, "init", "--quiet", "--initial-branch=master", seed)
	h.write(filepath.Join(seed, "README.md"), "# demo\n")
	h.runIn(seed, nil, "git", "add", "-A")
	h.runIn(seed, nil, "git", "commit", "--quiet", "-m", "start")
	h.runIn(seed, nil, "git", "push", "--quiet", origin, "master")
	h.fakeGitHub(origin)
	file := filepath.Join(h.config, "projects", "demo.toml")
	term := h.onTerminal(nil, "new", "demo", "--preset", "bare", "--repo", "x/demo")
	for _, q := range []string{"? the project's display name (demo): ", "? the roles, in the order they start (lead): ", "? the role that merges (lead): ", "? where the agent files go"} {
		term.expect(q)
		term.send("\r")
	}
	term.expect("  clone x/demo into ~/dev/demo/lead\n")
	term.expect("? Continue? (Y/n) ")
	term.send("\r")
	term.expect("✓ Continue? yes\nconfig: " + file + "\n")
	term.expect("opened https://github.com/x/demo/pull/1")
	if code := term.exit(); code != 0 || !exists(file) || !term.cooked() {
		t.Fatal(code, exists(file))
	}
	if text := h.read(file); !strings.Contains(text, "repo = \"x/demo\"\n") || !strings.Contains(text, "modules = [\"guards\"]\n") {
		t.Fatal(text)
	}
}
