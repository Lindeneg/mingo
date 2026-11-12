package team

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var targets = []string{"pnpm check", "pnpm test", "pnpm build", "pnpm e2e"}

type gateRun struct {
	root string
	deps GateDeps
	ran  *[]string
	out  *bytes.Buffer
}

func setUpGate(t *testing.T, results map[string]Result) gateRun {
	t.Helper()
	root := t.TempDir()
	var ran []string
	out := &bytes.Buffer{}
	clock := time.Unix(0, 0)
	deps := GateDeps{
		Root:    root,
		Targets: targets,
		RunTarget: func(target, logPath string) (Result, error) {
			ran = append(ran, target)
			if err := appendFile(logPath, "output of "+target+"\n"); err != nil {
				t.Fatal(err)
			}
			clock = clock.Add(2 * time.Second)
			return results[target], nil
		},
		Out: out,
		Now: func() time.Time { return clock },
	}
	return gateRun{root, deps, &ran, out}
}

func (g gateRun) lines() []string {
	return strings.Split(strings.TrimSuffix(g.out.String(), "\n"), "\n")
}

func TestGateRunsEveryTargetInOrderAndExitsZero(t *testing.T) {
	g := setUpGate(t, nil)
	if code, err := Gate(g.deps, []string{"gate.log"}); code != ExitOK || err != nil {
		t.Fatal(code, err)
	}
	if !reflect.DeepEqual(*g.ran, targets) {
		t.Fatal(*g.ran)
	}
	lines := g.lines()
	want := []string{"green  pnpm check (2 s)", "green  pnpm test (2 s)", "green  pnpm build (2 s)", "green  pnpm e2e (2 s)"}
	if !reflect.DeepEqual(lines[:4], want) || !strings.Contains(lines[4], "the gate is green") {
		t.Fatal(lines)
	}
	log, _ := os.ReadFile(filepath.Join(g.root, "gate.log"))
	wantLog := "\n== pnpm check\noutput of pnpm check\n\n== pnpm test\noutput of pnpm test\n" +
		"\n== pnpm build\noutput of pnpm build\n\n== pnpm e2e\noutput of pnpm e2e\n"
	if string(log) != wantLog {
		t.Fatalf("%q", log)
	}
}

func TestGateStopsAtTheFirstRedTarget(t *testing.T) {
	g := setUpGate(t, map[string]Result{"pnpm test": {Code: 1}})
	if code, _ := Gate(g.deps, []string{"gate.log"}); code != ExitFailed {
		t.Fatal(code)
	}
	if !reflect.DeepEqual(*g.ran, []string{"pnpm check", "pnpm test"}) {
		t.Fatal(*g.ran)
	}
	want := []string{
		"green  pnpm check (2 s)",
		"red    pnpm test (exit 1, 2 s)",
		"the gate is red at pnpm test; its output is in " + filepath.Join(g.root, "gate.log"),
	}
	if !reflect.DeepEqual(g.lines(), want) {
		t.Fatal(g.lines())
	}
	log, _ := os.ReadFile(filepath.Join(g.root, "gate.log"))
	if !strings.Contains(string(log), "output of pnpm test") {
		t.Fatal(string(log))
	}
}

func TestGateTakesATargetThatCannotStartOrOutlivesItsTimeoutForRed(t *testing.T) {
	for result, said := range map[Result]string{{NotFound: true}: "pnpm was not found", {TimedOut: true}: "stopped after 30 minutes"} {
		g := setUpGate(t, map[string]Result{"pnpm check": result})
		if code, _ := Gate(g.deps, []string{"gate.log"}); code != ExitFailed {
			t.Fatal(code)
		}
		if !reflect.DeepEqual(*g.ran, []string{"pnpm check"}) || !strings.Contains(g.lines()[0], said) {
			t.Fatal(*g.ran, g.lines())
		}
	}
}

func TestGateMakesTheLogsFolderAndStartsTheLogAnew(t *testing.T) {
	g := setUpGate(t, nil)
	Gate(g.deps, []string{"logs/deep/gate.log"})
	log := filepath.Join(g.root, "logs", "deep", "gate.log")
	if err := os.WriteFile(log, []byte("an earlier run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	Gate(g.deps, []string{"logs/deep/gate.log"})
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "an earlier run") {
		t.Fatal(string(data))
	}
}

func TestGateRefusesACallWithoutExactlyOneLogPath(t *testing.T) {
	for _, args := range [][]string{nil, {"a.log", "b.log"}, {"--help"}} {
		g := setUpGate(t, nil)
		if code, _ := Gate(g.deps, args); code != ExitUsage {
			t.Fatal(args, code)
		}
		if len(*g.ran) != 0 || !reflect.DeepEqual(g.lines(), []string{"usage: mingo gate <log>"}) {
			t.Fatal(*g.ran, g.lines())
		}
		if _, err := os.Stat(filepath.Join(g.root, "a.log")); err == nil {
			t.Fatal("a.log made")
		}
	}
}

func TestGateWithoutTargetsSaysWhereToNameThem(t *testing.T) {
	g := setUpGate(t, nil)
	g.deps.Targets = nil
	if code, _ := Gate(g.deps, []string{"gate.log"}); code != ExitFailed || len(*g.ran) != 0 {
		t.Fatal(code, *g.ran)
	}
}

func TestRealTargetRunsTheCommandWithoutAShellInTheRoot(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "g.log")
	r, err := RealTarget(root)("mingo-no-such-program a b", log)
	if err != nil || !r.NotFound {
		t.Fatal(r, err)
	}
}
