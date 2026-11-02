// Package heavy runs heavy commands in the slots every project on the machine shares.
package heavy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
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

type holder struct {
	pid   int
	taken string
	label string
	start string
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
	lines := strings.SplitN(string(data), "\n", 3)
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
	return h, true
}

func (h holder) runs() bool {
	if !sys.PidRuns(h.pid) {
		return false
	}
	if h.start == "" {
		return true
	}
	start, err := sys.ProcStart(h.pid)
	return err != nil || start == h.start
}

type Self struct {
	Pid   int
	Start string
}

func Me() Self {
	start, _ := sys.ProcStart(os.Getpid())
	return Self{Pid: os.Getpid(), Start: start}
}

// the slot taken, or 0 when the command has to wait
func Take(dir, label string, s Settings, me Self) int {
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
		return 0
	}
	if held > 0 {
		if !ramped {
			return 0
		}
		if mb, err := FreeMB(); err != nil || mb < s.MinFreeMB {
			return 0
		}
	}
	line := fmt.Sprintf("%d %d %s\n%s\n", me.Pid, now, label, me.Start)
	if os.WriteFile(slotFile(dir, free[0]), []byte(line), 0o644) != nil {
		return 0
	}
	return free[0]
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
		fmt.Fprintf(&b, "%s (pid %d, %d s), ", h.label, h.pid, now-taken)
	}
	return b.String()
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
	slot, said := 0, false
	for {
		lock, err := Lock(dir)
		if err != nil {
			return unusable()
		}
		slot = Take(dir, label, s, me)
		lock.Unlock()
		if slot > 0 {
			break
		}
		if !said {
			mb, _ := FreeMB()
			fmt.Fprintf(stderr, "mingo heavy: %s waits for a slot, held by %sfree memory %d MB\n", label, Holders(dir), mb)
			said = true
		}
		time.Sleep(time.Duration(s.PollS) * time.Second)
	}
	defer Release(dir, slot, me.Pid)
	if said {
		fmt.Fprintf(stderr, "mingo heavy: %s took slot %d\n", label, slot)
	}
	return runHolding(argv, stderr)
}

// at a terminal ctrl c reaches the command through its process group, so a second sigint is not sent
func runHolding(argv []string, stderr io.Writer) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "MINGO_HEAVY_HELD=1")
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "mingo heavy: %v\n", err)
		return sys.ExitCode(err)
	}
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
