//go:build unix

package team

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// the test's own catch keeps a sigint the rebase misses from ending the test process
func catchForTheTest(t *testing.T) chan os.Signal {
	t.Helper()
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT)
	t.Cleanup(func() { signal.Stop(signals) })
	return signals
}

func interruptingAt(r *repo, command string, during bool, caught chan os.Signal) {
	r.run = func(o Options, name string, args ...string) (Result, error) {
		if len(args) == 0 || args[0] != command {
			return Capture(o, name, args...)
		}
		if during {
			return Capture(o, "sh", "-c", `kill -INT $PPID; sleep 30`)
		}
		syscall.Kill(os.Getpid(), syscall.SIGINT)
		select {
		case <-caught:
		case <-time.After(5 * time.Second):
			r.t.Fatal("the signal did not come")
		}
		time.Sleep(100 * time.Millisecond)
		return Capture(o, name, args...)
	}
}

func setUpInterrupted(t *testing.T) (*repo, string) {
	t.Helper()
	r := setUpRepo(t, nil)
	r.commit("feature", map[string]string{appFile: entries("app", "feature")}, "feature")
	r.commit("master", map[string]string{"apps/api/src/other.ts": entries("x")}, "master")
	return r, r.remoteHead("feature")
}

func TestRebaseStoppedBetweenTwoCommandsRemovesItsWorktreeAndExitsBySignal(t *testing.T) {
	caught := catchForTheTest(t)
	r, before := setUpInterrupted(t)
	interruptingAt(r, "checkout", false, caught)
	if code := r.rebase("feature"); code != 130 {
		t.Fatal(code, r.printed())
	}
	equal(t, r.printed(), []string{"the rebase was stopped by SIGINT; nothing was pushed"})
	if r.remoteHead("feature") != before {
		t.Fatal("pushed")
	}
	r.expectClean("master")
}

func TestRebaseStoppedDuringACommandEndsItRemovesItsWorktreeAndExitsBySignal(t *testing.T) {
	catchForTheTest(t)
	r, before := setUpInterrupted(t)
	interruptingAt(r, "checkout", true, nil)
	start := time.Now()
	if code := r.rebase("feature"); code != 130 {
		t.Fatal(code, r.printed())
	}
	if time.Since(start) > 20*time.Second {
		t.Fatal("the command ran on", time.Since(start))
	}
	equal(t, r.printed(), []string{"the rebase was stopped by SIGINT; nothing was pushed"})
	if r.remoteHead("feature") != before {
		t.Fatal("pushed")
	}
	r.expectClean("master")
}

func TestRebaseStoppedDuringThePushSaysOriginMayHaveTakenIt(t *testing.T) {
	catchForTheTest(t)
	r, _ := setUpInterrupted(t)
	interruptingAt(r, "push", true, nil)
	if code := r.rebase("feature"); code != 130 {
		t.Fatal(code, r.printed())
	}
	equal(t, r.printed(), []string{"the rebase was stopped by SIGINT during the push; run git fetch to see whether origin took it"})
	r.expectClean("master")
}
