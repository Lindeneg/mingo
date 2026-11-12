// Package heavy runs heavy commands in the slots every project on the machine shares.
package heavy

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/sys"
)

type Settings struct {
	Slots     int
	MinFreeMB int
	RampS     int64
	PollS     int
}

// a setting left unset takes its default, one set to anything but a whole number in range is refused
func envInt(name string, fallback, least int) (int, error) {
	v, set := os.LookupEnv(name)
	if !set || v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < least {
		return 0, fmt.Errorf("%s is %s, it takes a whole number of %d or more", name, v, least)
	}
	return n, nil
}

func FromEnv() (Settings, error) {
	var s Settings
	var err error
	for _, f := range []struct {
		name            string
		into            *int
		fallback, least int
	}{
		{"MINGO_HEAVY_SLOTS", &s.Slots, 2, 1},
		{"MINGO_HEAVY_MIN_FREE_MB", &s.MinFreeMB, 5000, 0},
		{"MINGO_HEAVY_POLL_S", &s.PollS, 5, 1},
	} {
		if *f.into, err = envInt(f.name, f.fallback, f.least); err != nil {
			return s, err
		}
	}
	ramp, err := envInt("MINGO_HEAVY_RAMP_S", 60, 0)
	s.RampS = int64(ramp)
	return s, err
}

var FreeMB = sys.FreeMB

func Dir() string {
	return filepath.Join(config.StateDir(), "heavy")
}

func slotFile(dir string, n int) string {
	return filepath.Join(dir, "slot-"+strconv.Itoa(n))
}

// the slot holds mingo heavy's pid and start time, then its command's once that has started
type holder struct {
	pid      int
	taken    string
	label    string
	start    string
	cmd      int
	cmdStart string
}

var (
	pidRe    = regexp.MustCompile(`^[1-9][0-9]*$`)
	digitsRe = regexp.MustCompile(`^[0-9]+$`)
)

func readHolder(file string) (holder, bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return holder{}, false
	}
	lines := strings.SplitN(string(data), "\n", 4)
	fields := strings.Fields(lines[0])
	if len(fields) == 0 || !pidRe.MatchString(fields[0]) {
		return holder{}, false
	}
	h := holder{}
	h.pid, _ = strconv.Atoi(fields[0])
	if len(fields) > 1 {
		h.taken = fields[1]
	}
	if len(fields) > 2 {
		h.label = strings.Join(fields[2:], " ")
	}
	if len(lines) > 1 {
		h.start = strings.TrimSpace(lines[1])
	}
	if len(lines) > 2 {
		if cmd := strings.Fields(lines[2]); len(cmd) > 0 && pidRe.MatchString(cmd[0]) {
			h.cmd, _ = strconv.Atoi(cmd[0])
			if len(cmd) > 1 {
				h.cmdStart = cmd[1]
			}
		}
	}
	return h, true
}

// a pid whose start time differs from the one written down was given to another process since
func alive(pid int, start string) bool {
	if pid <= 0 || !sys.PidRuns(pid) {
		return false
	}
	if start == "" {
		return true
	}
	now, err := sys.ProcStart(pid)
	return err != nil || now == start
}

// the slot is held while mingo heavy or its command runs, since a kill of the one may leave the other
func (h holder) runs() bool {
	return alive(h.pid, h.start) || alive(h.cmd, h.cmdStart)
}

func (h holder) shownPid() int {
	if h.cmd > 0 && !alive(h.pid, h.start) {
		return h.cmd
	}
	return h.pid
}

type Self struct {
	Pid   int
	Start string
}

func Me() Self {
	start, _ := sys.ProcStart(os.Getpid())
	return Self{Pid: os.Getpid(), Start: start}
}

// why a command waits for a slot
type Wait int

const (
	NoWait Wait = iota
	AllTaken
	Ramping
	LowMemory
	Unwritable
)

// the slot taken, or 0 and why the command has to wait
func Take(dir, label string, s Settings, me Self) (int, Wait) {
	now := time.Now().Unix()
	var free []int
	held, ramped := 0, true
	for n := 1; n <= s.Slots; n++ {
		file := slotFile(dir, n)
		if h, ok := readHolder(file); ok && h.runs() {
			held++
			// a command beside another starts only once that one has run the ramp, so the free memory it reads counts what the first one grew to
			if taken, err := strconv.ParseInt(h.taken, 10, 64); err == nil && digitsRe.MatchString(h.taken) && taken <= now && now-taken < s.RampS {
				ramped = false
			}
			continue
		}
		os.Remove(file)
		free = append(free, n)
	}
	if len(free) == 0 {
		return 0, AllTaken
	}
	if held > 0 {
		if !ramped {
			return 0, Ramping
		}
		if mb, err := FreeMB(); err != nil || mb < s.MinFreeMB {
			return 0, LowMemory
		}
	}
	line := fmt.Sprintf("%d %d %s\n%s\n", me.Pid, now, label, me.Start)
	if os.WriteFile(slotFile(dir, free[0]), []byte(line), 0o644) != nil {
		return 0, Unwritable
	}
	return free[0], NoWait
}

// writes the command's pid into the slot mingo heavy took for it. the new file replaces the old
// in one step, so one that reads the slots without the lock never sees half of it
func Hold(dir string, slot int, me Self, pid int) {
	lock, err := Lock(dir)
	if err != nil {
		return
	}
	defer lock.Unlock()
	file := slotFile(dir, slot)
	h, ok := readHolder(file)
	if !ok || h.pid != me.Pid {
		return
	}
	start, _ := sys.ProcStart(pid)
	next := filepath.Join(dir, ".slot-"+strconv.Itoa(slot)+".new")
	if os.WriteFile(next, []byte(fmt.Sprintf("%d %s %s\n%s\n%d %s\n", h.pid, h.taken, h.label, h.start, pid, start)), 0o644) == nil {
		os.Rename(next, file)
	}
}

func Holders(dir string) string {
	now := time.Now().Unix()
	matches, _ := filepath.Glob(filepath.Join(dir, "slot-*"))
	var b strings.Builder
	for _, file := range matches {
		h, ok := readHolder(file)
		if !ok || !h.runs() {
			continue
		}
		taken, _ := strconv.ParseInt(h.taken, 10, 64)
		fmt.Fprintf(&b, "%s (pid %d, %d s), ", h.label, h.shownPid(), now-taken)
	}
	return b.String()
}

func waitLine(label string, why Wait, dir string, s Settings) string {
	holders := strings.TrimSuffix(Holders(dir), ", ")
	switch why {
	case AllTaken:
		return fmt.Sprintf("%s waits for a slot, every slot is taken: %s", label, holders)
	case Ramping:
		return fmt.Sprintf("%s waits for a slot, a command starts beside another only once that one has run %d s: %s", label, s.RampS, holders)
	}
	mb, err := FreeMB()
	if err != nil {
		return fmt.Sprintf("%s waits for a slot, the free memory cannot be read: %v", label, err)
	}
	return fmt.Sprintf("%s waits for a slot, free memory is %d MB, under the %d MB a command needs beside %s", label, mb, s.MinFreeMB, holders)
}

func cannotRun(name string, err error) string {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Sprintf("%q is not a command on PATH, check its name", name)
	}
	var e *exec.Error
	if errors.As(err, &e) {
		err = e.Err
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	return fmt.Sprintf("%q cannot be run: %v", name, err)
}

func Release(dir string, slot, pid int) {
	file := slotFile(dir, slot)
	if h, ok := readHolder(file); ok && h.pid == pid {
		os.Remove(file)
	}
}

func Lock(dir string) (*sys.Lock, error) {
	return sys.LockFile(filepath.Join(dir, ".flock"))
}

func execNow(argv []string, env []string, stderr io.Writer) int {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		fmt.Fprintf(stderr, "mingo heavy: %v\n", err)
		return sys.ExitCode(err)
	}
	err = sys.Exec(path, argv, env)
	fmt.Fprintf(stderr, "mingo heavy: %v\n", err)
	return sys.ExitCode(err)
}

func Run(label string, argv []string) int {
	stderr := os.Stderr
	// a command that cannot be run fails now, not after it waited for a slot
	if _, err := exec.LookPath(argv[0]); err != nil {
		fmt.Fprintf(stderr, "mingo heavy: %s\n", cannotRun(argv[0], err))
		return sys.ExitCode(err)
	}
	if os.Getenv("MINGO_HEAVY") == "off" || os.Getenv("MINGO_HEAVY_HELD") != "" {
		return execNow(argv, os.Environ(), stderr)
	}
	if _, err := FreeMB(); err != nil {
		fmt.Fprintf(stderr, "mingo: %v\n", err)
		return 1
	}
	s, err := FromEnv()
	if err != nil {
		fmt.Fprintf(stderr, "mingo heavy: %v\n", err)
		return 2
	}
	dir := Dir()
	unusable := func() int {
		fmt.Fprintf(stderr, "mingo heavy: %s cannot be used, running %s without a slot\n", dir, label)
		return execNow(argv, os.Environ(), stderr)
	}
	if os.MkdirAll(dir, 0o755) != nil || !sys.Writable(dir) {
		return unusable()
	}
	me := Me()
	slot, said := 0, NoWait
	for {
		lock, err := Lock(dir)
		if err != nil {
			return unusable()
		}
		var why Wait
		slot, why = Take(dir, label, s, me)
		lock.Unlock()
		if slot > 0 {
			break
		}
		if why == Unwritable {
			return unusable()
		}
		// said again only when the reason changes, not at every look
		if why != said {
			fmt.Fprintf(stderr, "mingo heavy: %s\n", waitLine(label, why, dir, s))
			said = why
		}
		time.Sleep(time.Duration(s.PollS) * time.Second)
	}
	defer Release(dir, slot, me.Pid)
	if said != NoWait {
		fmt.Fprintf(stderr, "mingo heavy: %s took slot %d\n", label, slot)
	}
	return runHolding(argv, stderr, func(pid int) { Hold(dir, slot, me, pid) })
}

// at a terminal ctrl c reaches the command through its process group, so a second sigint is not sent
func runHolding(argv []string, stderr io.Writer, started func(pid int)) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "MINGO_HEAVY_HELD=1")
	sys.EndWithParent(cmd)
	// the kernel ends the command when the thread that started it ends, so that thread stays this one's
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "mingo heavy: %v\n", err)
		return sys.ExitCode(err)
	}
	started(cmd.Process.Pid)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	terminal := sys.IsTerminal(os.Stdin.Fd())
	for {
		select {
		case sig := <-signals:
			if sig == syscall.SIGINT && terminal {
				continue
			}
			cmd.Process.Signal(sig)
		case err := <-done:
			return sys.ExitCode(err)
		}
	}
}
