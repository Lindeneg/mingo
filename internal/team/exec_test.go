package team

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch os.Getenv("TEAM_HELPER") {
	case "streams":
		fmt.Fprint(os.Stdout, "out")
		fmt.Fprint(os.Stderr, "err")
		os.Exit(3)
	case "env":
		fmt.Fprint(os.Stdout, os.Getenv("TEAM_X"))
		os.Exit(0)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "lines":
		fmt.Fprintln(os.Stdout, "to stdout")
		fmt.Fprintln(os.Stderr, "to stderr")
		os.Exit(2)
	case "group":
		Capture(Options{Timeout: time.Minute}, "sh", "-c", `sleep 600 & echo $! >"$1"; wait`, "_", os.Getenv("TEAM_FILE"))
		os.Exit(0)
	}
	code := m.Run()
	if templateDir != "" {
		os.RemoveAll(templateDir)
	}
	os.Exit(code)
}

func helper(t *testing.T, kind string) Options {
	t.Helper()
	return Options{Dir: t.TempDir(), Timeout: 20 * time.Second, Env: []string{"TEAM_HELPER=" + kind}}
}

func self(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestCaptureReturnsTheExitCodeWithBothStreams(t *testing.T) {
	r, err := Capture(helper(t, "streams"), self(t))
	if err != nil || r != (Result{Code: 3, Stdout: "out", Stderr: "err"}) {
		t.Fatal(r, err)
	}
}

func TestCaptureSetsTheGivenVariablesOverTheProcesssOwn(t *testing.T) {
	o := helper(t, "env")
	o.Env = append(o.Env, "TEAM_X=set")
	r, err := Capture(o, self(t))
	if err != nil || !r.OK() || r.Stdout != "set" {
		t.Fatal(r, err)
	}
}

func TestCaptureSaysWhenTheProgramIsNotInstalled(t *testing.T) {
	r, err := Capture(Options{Dir: t.TempDir(), Timeout: 20 * time.Second}, "mingo-no-such-program")
	if err != nil || r != (Result{NotFound: true}) {
		t.Fatal(r, err)
	}
}

func TestCaptureStopsAProgramThatOutlivesItsTimeout(t *testing.T) {
	o := helper(t, "sleep")
	o.Timeout = 200 * time.Millisecond
	r, err := Capture(o, self(t))
	if err != nil || r != (Result{TimedOut: true}) {
		t.Fatal(r, err)
	}
}

func TestRunIntoLogAppendsBothStreamsToTheLogAndReturnsTheExitCode(t *testing.T) {
	o := helper(t, "lines")
	log := filepath.Join(o.Dir, "run.log")
	for range 2 {
		r, err := RunIntoLog(o, log, self(t))
		if err != nil || r != (Result{Code: 2}) {
			t.Fatal(r, err)
		}
	}
	data, _ := os.ReadFile(log)
	if strings.Count(string(data), "to stdout") != 2 || strings.Count(string(data), "to stderr") != 2 {
		t.Fatal(string(data))
	}
}

func TestRunIntoLogSaysWhenTheProgramIsNotInstalled(t *testing.T) {
	dir := t.TempDir()
	r, err := RunIntoLog(Options{Dir: dir, Timeout: 20 * time.Second}, filepath.Join(dir, "run.log"), "mingo-no-such-program")
	if err != nil || r != (Result{NotFound: true}) {
		t.Fatal(r, err)
	}
}

func TestLastLinesReturnsTheLastLinesWithoutTheTrailingNewline(t *testing.T) {
	for _, c := range [][3]string{{"a\nb\nc\n", "2", "b\nc"}, {"a\n", "5", "a"}} {
		n, _ := strconv.Atoi(c[1])
		if got := LastLines(c[0], n); got != c[2] {
			t.Fatalf("%q", got)
		}
	}
}
