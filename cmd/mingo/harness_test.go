//go:build unix

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

var mingoBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mingo-bin-")
	if err != nil {
		panic(err)
	}
	mingoBin = filepath.Join(dir, "mingo")
	if _, err := exec.LookPath("tmux"); err != nil {
		skipAll = "the end to end tests run tmux, which is not on PATH"
	}
	if goTool, err := exec.LookPath("go"); err != nil {
		skipAll = "the end to end tests build mingo with the go command, which is not on PATH"
	} else {
		build := exec.Command(goTool, "build", "-o", mingoBin, ".")
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func repoRoot() string {
	dir, _ := filepath.Abs("../..")
	return dir
}

type home struct {
	t      *testing.T
	root   string
	home   string
	config string
	state  string
	socket string
	bin    string
	extra  []string
}

// set when a tool every case needs is missing, so each case says so and skips
var skipAll string

// a socket path has a length limit, so every case gets a short folder of its own
func newHome(t *testing.T) *home {
	t.Helper()
	if skipAll != "" {
		t.Skip(skipAll)
	}
	root, err := os.MkdirTemp("/tmp", "mingo-test-")
	if err != nil {
		t.Fatal(err)
	}
	// on macos /tmp is a link to /private/tmp, and the paths mingo prints are the resolved ones
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	h := &home{
		t: t, root: root, home: filepath.Join(root, "home"), config: filepath.Join(root, "config"),
		state: filepath.Join(root, "state"), socket: filepath.Join(root, "tmux.sock"), bin: mingoBin,
	}
	for _, dir := range []string{filepath.Join(h.home, ".local", "bin"), filepath.Join(h.config, "projects")} {
		h.mkdir(dir)
	}
	h.write(filepath.Join(root, "starts"), "")
	h.write(filepath.Join(root, "input"), "")
	h.setMode("obeys")
	t.Cleanup(h.teardown)
	return h
}

func (h *home) teardown() {
	out, _ := exec.Command("tmux", "-S", h.socket, "list-panes", "-a", "-F", "#{pane_pid}").Output()
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 1 {
			continue
		}
		if group, err := syscall.Getpgid(pid); err == nil && group > 1 {
			syscall.Kill(-group, syscall.SIGKILL)
		}
	}
	exec.Command("tmux", "-S", h.socket, "kill-server").Run()
	os.RemoveAll(h.root)
}

func (h *home) mkdir(dir string) {
	h.t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func (h *home) write(path, text string) {
	h.t.Helper()
	h.mkdir(filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *home) read(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

func (h *home) env() []string {
	env := []string{
		"HOME=" + h.home,
		"MINGO_CONFIG=" + h.config,
		"MINGO_STATE=" + h.state,
		"MINGO_TMUX_SOCKET=" + h.socket,
		"MINGO_WATCH_EVERY=1",
		"MINGO_EXIT_GRACE=3",
		"MINGO_TERM_GRACE=1",
		"PATH=" + h.home + "/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
		"USER=" + os.Getenv("USER"),
	}
	if term := os.Getenv("TERM"); term != "" {
		env = append(env, "TERM="+term)
	}
	return append(env, h.extra...)
}

func (h *home) setEnv(kv ...string) {
	h.extra = append(h.extra, kv...)
}

type result struct {
	out  string
	code int
}

func (r result) lines() []string {
	return strings.Split(strings.TrimSuffix(r.out, "\n"), "\n")
}

func (h *home) runIn(dir string, env []string, name string, args ...string) result {
	h.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(h.env(), env...)
	cmd.Dir = dir
	if dir == "" {
		cmd.Dir = h.root
	}
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
			if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				code = 128 + int(ws.Signal())
			}
		} else {
			h.t.Fatal(err)
		}
	}
	return result{out: b.String(), code: code}
}

func (h *home) mingo(args ...string) result {
	h.t.Helper()
	return h.runIn("", nil, h.bin, args...)
}

func (h *home) addProject(name string, lines ...string) {
	h.write(filepath.Join(h.config, "projects", name+".toml"), strings.Join(lines, "\n")+"\n")
}

func (h *home) addDemo() {
	h.addDemoWith()
}

func (h *home) addDemoWith(top ...string) {
	lines := append([]string{`repo = "Lindeneg/demo"`, `opener = "You are the {role} of {project}."`}, top...)
	lines = append(lines,
		`[[roles]]`, `name = "lead"`, `aliases = ["l"]`, `model = "claude-opus-5-5"`, `effort = "high"`,
		`[[roles]]`, `name = "helper"`, `aliases = ["h"]`)
	h.addProject("demo", lines...)
	h.mkdir(filepath.Join(h.home, "dev", "demo", "lead"))
	h.mkdir(filepath.Join(h.home, "dev", "demo", "helper"))
}

// the stand-in's modes. obeys ends on /exit, slow a second after it, held never reads its
// input, raw is held and takes ctrl c as a key, deaf also ignores sigterm, listens writes what is
// typed and never ends
func (h *home) installStandIn() {
	h.t.Helper()
	script := fmt.Sprintf("#!/usr/bin/env bash\nroot=%s\n", shellQuote(h.root)) + `case "${1:-}" in
    auth) echo "stand-in is logged in"; exit 0 ;;
    agents) echo "[]"; exit 0 ;;
esac
mode=$(cat "$root/mode")
case "$mode" in
    raw) stty -isig ;;
    deaf) trap '' TERM ;;
esac
printf '%s\n' "$$" >>"$root/starts"
case "$mode" in
    obeys) while IFS= read -r line; do [[ $line == /exit ]] && exit 0; done ;;
    slow) while IFS= read -r line; do [[ $line == /exit ]] && { sleep 1; exit 0; }; done ;;
    listens) while IFS= read -r line; do printf '%s\n' "$line" >>"$root/input"; done ;;
esac
while :; do sleep 1; done
`
	file := filepath.Join(h.home, ".local", "bin", "claude")
	h.write(file, script)
	if err := os.Chmod(file, 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (h *home) setMode(mode string) {
	h.write(filepath.Join(h.root, "mode"), mode+"\n")
}

func (h *home) startLines() []string {
	var lines []string
	for _, l := range strings.Split(h.read(filepath.Join(h.root, "starts")), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func (h *home) starts() int {
	return len(h.startLines())
}

func (h *home) nthStart(n int) int {
	lines := h.startLines()
	if n > len(lines) {
		return 0
	}
	pid, _ := strconv.Atoi(lines[n-1])
	return pid
}

func (h *home) typed() string {
	return strings.TrimSuffix(h.read(filepath.Join(h.root, "input")), "\n")
}

func (h *home) marker(kind string, where ...string) string {
	project, role := "demo", "lead"
	if len(where) > 0 {
		project = where[0]
	}
	if len(where) > 1 {
		role = where[1]
	}
	return filepath.Join(h.state, project, role+"."+kind)
}

func (h *home) touch(path string) {
	h.write(path, "")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (h *home) screen(session ...string) string {
	name := "demo-lead"
	if len(session) > 0 {
		name = session[0]
	}
	out, _ := exec.Command("tmux", "-S", h.socket, "capture-pane", "-p", "-J", "-t", "="+name+":", "-S", "-300").Output()
	return string(out)
}

func isAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

func waitFor(seconds float64, cond func() bool) bool {
	deadline := time.Now().Add(time.Duration(seconds * float64(time.Second)))
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (h *home) mustWait(seconds float64, what string, cond func() bool) {
	h.t.Helper()
	if !waitFor(seconds, cond) {
		h.t.Fatalf("timed out waiting for %s\nscreen:\n%s", what, h.screen())
	}
}

func (h *home) startsAre(n int) func() bool {
	return func() bool { return h.starts() == n }
}

func (h *home) startLoop(mode string) {
	h.t.Helper()
	h.installStandIn()
	h.addDemo()
	h.setMode(mode)
	if r := h.mingo("start", "demo/lead"); r.code != 0 {
		h.t.Fatal(r.out)
	}
	h.mustWait(15, "the first start", h.startsAre(1))
}

func sleepSeconds(s float64) {
	time.Sleep(time.Duration(s * float64(time.Second)))
}

func removeDir(path string) error {
	return os.Remove(path)
}

func removeFile(path string) {
	os.Remove(path)
}
