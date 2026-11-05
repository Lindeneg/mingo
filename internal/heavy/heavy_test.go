package heavy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lindeneg/mingo/internal/sys"
)

type slots struct {
	t   *testing.T
	dir string
	s   Settings
	me  Self
}

func newSlots(t *testing.T) *slots {
	t.Helper()
	FreeMB = func() (int, error) { return 100000, nil }
	t.Cleanup(func() { FreeMB = sys.FreeMB })
	for _, v := range []string{"MINGO_HEAVY_SLOTS", "MINGO_HEAVY_MIN_FREE_MB", "MINGO_HEAVY_RAMP_S", "MINGO_HEAVY_POLL_S"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	s, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	return &slots{t: t, dir: t.TempDir(), s: s, me: Me()}
}

func (sl *slots) take(label string) int {
	n, _ := Take(sl.dir, label, sl.s, sl.me)
	return n
}

func sleeper(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "600")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return cmd.Process.Pid
}

func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func (sl *slots) write(n int, text string) {
	sl.t.Helper()
	if err := os.WriteFile(slotFile(sl.dir, n), []byte(text), 0o644); err != nil {
		sl.t.Fatal(err)
	}
}

func (sl *slots) holder(n int, taken int64, label string) int {
	sl.t.Helper()
	pid := sleeper(sl.t)
	sl.write(n, fmt.Sprintf("%d %d %s\n", pid, taken, label))
	return pid
}

func ago(s int64) int64 {
	return time.Now().Unix() - s
}

func TestTwoGetASlotEachTheThirdNoneThenTheFreedOne(t *testing.T) {
	sl := newSlots(t)
	if n := sl.take("first"); n != 1 {
		t.Fatal(n)
	}
	sl.holder(1, ago(120), "check")
	if n := sl.take("second"); n != 2 {
		t.Fatal(n)
	}
	sl.holder(2, ago(120), "check")
	if n := sl.take("third"); n != 0 {
		t.Fatal(n)
	}
	os.Remove(slotFile(sl.dir, 1))
	if n := sl.take("third"); n != 1 {
		t.Fatal(n)
	}
}

func TestTakesBackADeadHolderAndANonsenseFile(t *testing.T) {
	sl := newSlots(t)
	sl.write(1, fmt.Sprintf("%d 1 old\n", deadPid(t)))
	sl.write(2, "nonsense\n")
	if n := sl.take("first"); n != 1 {
		t.Fatal(n)
	}
	if _, err := os.Stat(slotFile(sl.dir, 2)); !os.IsNotExist(err) {
		t.Fatal("slot-2 kept")
	}
}

func TestTakesBackASlotWhosePidWasReused(t *testing.T) {
	sl := newSlots(t)
	pid := sleeper(t)
	sl.write(1, fmt.Sprintf("%d %d check\n1\n", pid, ago(120)))
	if n := sl.take("first"); n != 1 {
		t.Fatal(n)
	}
}

func TestCountsAHolderWhoseStartTimeMatches(t *testing.T) {
	sl := newSlots(t)
	pid := sleeper(t)
	start, err := sys.ProcStart(pid)
	if err != nil {
		t.Fatal(err)
	}
	sl.write(1, fmt.Sprintf("%d %d check\n%s\n", pid, ago(120), start))
	if n := sl.take("next"); n != 2 {
		t.Fatal(n)
	}
}

func TestWaitsOnTheMemoryFloorOnlyBesideAnotherHolder(t *testing.T) {
	sl := newSlots(t)
	FreeMB = func() (int, error) { return 10, nil }
	if n := sl.take("alone"); n != 1 {
		t.Fatal(n)
	}
	sl.holder(1, ago(120), "check")
	if n := sl.take("beside"); n != 0 {
		t.Fatal(n)
	}
}

func TestTakesASlotBesideAnotherOnlyAfterItsRamp(t *testing.T) {
	sl := newSlots(t)
	sl.holder(1, ago(0), "check")
	if n := sl.take("early"); n != 0 {
		t.Fatal(n)
	}
	sl.holder(1, ago(sl.s.RampS), "check")
	if n := sl.take("later"); n != 2 {
		t.Fatal(n)
	}
}

func TestAHolderStartedInTheFutureCountsAsRamped(t *testing.T) {
	sl := newSlots(t)
	sl.holder(1, ago(-3600), "check")
	if n := sl.take("next"); n != 2 {
		t.Fatal(n)
	}
}

func TestReleasesOnlyItsOwnSlot(t *testing.T) {
	sl := newSlots(t)
	sl.holder(1, ago(120), "check")
	Release(sl.dir, 1, sl.me.Pid)
	if _, err := os.Stat(slotFile(sl.dir, 1)); err != nil {
		t.Fatal("another's slot released")
	}
	sl.write(2, fmt.Sprintf("%d %d mine\n", sl.me.Pid, ago(0)))
	Release(sl.dir, 2, sl.me.Pid)
	if _, err := os.Stat(slotFile(sl.dir, 2)); !os.IsNotExist(err) {
		t.Fatal("own slot kept")
	}
}

func TestTakeSaysWhyACommandWaits(t *testing.T) {
	sl := newSlots(t)
	sl.holder(1, ago(0), "check")
	if n, why := Take(sl.dir, "early", sl.s, sl.me); n != 0 || why != Ramping {
		t.Fatal(n, why)
	}
	sl.holder(1, ago(120), "check")
	FreeMB = func() (int, error) { return 10, nil }
	if n, why := Take(sl.dir, "beside", sl.s, sl.me); n != 0 || why != LowMemory {
		t.Fatal(n, why)
	}
	sl.holder(2, ago(120), "test")
	if n, why := Take(sl.dir, "third", sl.s, sl.me); n != 0 || why != AllTaken {
		t.Fatal(n, why)
	}
}

func TestTheWaitLineNamesTheReasonAndTheHolders(t *testing.T) {
	sl := newSlots(t)
	pid := sl.holder(1, ago(5), "build")
	FreeMB = func() (int, error) { return 3000, nil }
	held := fmt.Sprintf("build (pid %d, ", pid)
	for why, want := range map[Wait]string{
		AllTaken:  "check waits for a slot, every slot is taken: " + held,
		Ramping:   "check waits for a slot, a command starts beside another only once that one has run 60 s: " + held,
		LowMemory: "check waits for a slot, free memory is 3000 MB, under the 5000 MB a command needs beside " + held,
	} {
		if got := waitLine("check", why, sl.dir, sl.s); !strings.HasPrefix(got, want) || strings.HasSuffix(got, ", ") {
			t.Fatalf("%q", got)
		}
	}
}

func TestASlotStaysHeldWhileItsCommandRunsAfterMingoHeavyIsGone(t *testing.T) {
	sl := newSlots(t)
	cmd := sleeper(t)
	start, err := sys.ProcStart(cmd)
	if err != nil {
		t.Fatal(err)
	}
	sl.write(1, fmt.Sprintf("%d %d check\n\n%d %s\n", deadPid(t), ago(120), cmd, start))
	if n := sl.take("next"); n != 2 {
		t.Fatal(n)
	}
	if out := Holders(sl.dir); !strings.HasPrefix(out, fmt.Sprintf("check (pid %d, ", cmd)) {
		t.Fatal(out)
	}
	os.Remove(slotFile(sl.dir, 2))
	sl.write(1, fmt.Sprintf("%d %d check\n\n%d 1\n", deadPid(t), ago(120), cmd))
	if n := sl.take("next"); n != 1 {
		t.Fatal("a slot whose command pid was given to another process is held", n)
	}
}

func TestHoldWritesTheCommandsPidIntoItsOwnSlotOnly(t *testing.T) {
	sl := newSlots(t)
	if n := sl.take("check"); n != 1 {
		t.Fatal(n)
	}
	cmd := sleeper(t)
	Hold(sl.dir, 1, Self{Pid: sl.me.Pid + 1}, cmd)
	if h, _ := readHolder(slotFile(sl.dir, 1)); h.cmd != 0 {
		t.Fatal("another's slot was written", h)
	}
	Hold(sl.dir, 1, sl.me, cmd)
	start, _ := sys.ProcStart(cmd)
	h, ok := readHolder(slotFile(sl.dir, 1))
	if !ok || h.pid != sl.me.Pid || h.label != "check" || h.start != sl.me.Start || h.cmd != cmd || h.cmdStart != start {
		t.Fatal(h)
	}
	if matches, _ := filepath.Glob(filepath.Join(sl.dir, ".slot-*")); len(matches) != 0 {
		t.Fatal(matches)
	}
}

func TestNamesEachRunningHolder(t *testing.T) {
	sl := newSlots(t)
	pid := sl.holder(1, ago(5), "build")
	sl.write(2, fmt.Sprintf("%d 1 gone\n", deadPid(t)))
	out := Holders(sl.dir)
	if !strings.HasPrefix(out, fmt.Sprintf("build (pid %d, ", pid)) || strings.Contains(out, "gone") {
		t.Fatal(out)
	}
}

func TestWaitsForTheLockAndTakesBackOneADeadTakeLeft(t *testing.T) {
	sl := newSlots(t)
	held, err := Lock(sl.dir)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan *sys.Lock)
	go func() {
		l, _ := Lock(sl.dir)
		got <- l
	}()
	select {
	case <-got:
		t.Fatal("took a held lock")
	case <-time.After(time.Second):
	}
	held.Unlock()
	select {
	case l := <-got:
		l.Unlock()
	case <-time.After(5 * time.Second):
		t.Fatal("lock not taken after release")
	}
	dying := exec.Command("sh", "-c", `exec 9>>"$1" && flock 9 && exec sleep 600`, "_", filepath.Join(sl.dir, ".flock"))
	if err := dying.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	dying.Process.Kill()
	dying.Wait()
	l, err := Lock(sl.dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Unlock()
}

func TestReadsTheLimitsFromTheEnvironment(t *testing.T) {
	sl := newSlots(t)
	if sl.s.Slots != 2 || sl.s.MinFreeMB != 5000 || sl.s.RampS != 60 {
		t.Fatal(sl.s)
	}
	t.Setenv("MINGO_HEAVY_SLOTS", "3")
	t.Setenv("MINGO_HEAVY_MIN_FREE_MB", "100")
	if s, err := FromEnv(); err != nil || s.Slots != 3 || s.MinFreeMB != 100 {
		t.Fatal(s)
	}
}

func TestSettingsOutOfRangeAreRefused(t *testing.T) {
	for name, value := range map[string]string{"MINGO_HEAVY_SLOTS": "0", "MINGO_HEAVY_POLL_S": "0", "MINGO_HEAVY_MIN_FREE_MB": "lots", "MINGO_HEAVY_RAMP_S": "-1"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), name+" is "+value+", it takes a whole number of") {
				t.Fatal(err)
			}
		})
	}
}
