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
	return Take(sl.dir, label, sl.s, sl.me)
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
