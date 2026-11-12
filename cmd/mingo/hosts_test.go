//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a stand-in for ssh: each destination is a host of its own under hosts, with its own home, config,
// state and tmux socket, and the line runs there as the shell at the other end would run it
func (h *home) installSSH() {
	h.t.Helper()
	script := "#!/usr/bin/env bash\nroot=" + shellQuote(h.root) + `
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o) shift 2 ;;
        -t|-T) shift ;;
        *) break ;;
    esac
done
dest=$1
shift
dir=$root/hosts/$dest
if [[ ! -d $dir ]]; then
    echo "ssh: Could not resolve hostname $dest: Name or service not known" >&2
    exit 255
fi
printf '%s\n' "$*" >>"$dir/calls"
cd "$dir/home" || exit 255
exec env -i HOME="$dir/home" PATH="$dir/bin:$dir/home/.local/bin:/usr/local/bin:/usr/bin:/bin" USER="$USER" \
    MINGO_CONFIG="$dir/config" MINGO_STATE="$dir/state" MINGO_TMUX_SOCKET="$dir/tmux.sock" sh -c "$*"
`
	file := filepath.Join(h.root, "bin", "ssh")
	h.write(file, script)
	os.Chmod(file, 0o755)
	h.setEnv("MINGO_SSH=" + file)
}

// a host whose uname -sm says system, with a stand-in for go whose install puts this test's mingo in GOBIN
func (h *home) addHost(dest, system string, withGo bool) string {
	h.t.Helper()
	dir := filepath.Join(h.root, "hosts", dest)
	for _, d := range []string{"home", "config/projects", "state"} {
		h.mkdir(filepath.Join(dir, d))
	}
	h.sockets = append(h.sockets, filepath.Join(dir, "tmux.sock"))
	tools := map[string]string{"uname": "#!/bin/sh\necho " + shellQuote(system) + "\n"}
	if withGo {
		tools["go"] = "#!/bin/sh\nprintf '%s\\n' \"$*\" >>" + shellQuote(filepath.Join(dir, "go-calls")) + "\n[ \"$1\" = install ] || exit 2\ncp " + shellQuote(h.bin) + " \"$GOBIN/mingo\"\n"
	}
	for name, text := range tools {
		file := filepath.Join(dir, "bin", name)
		h.write(file, text)
		os.Chmod(file, 0o755)
	}
	return dir
}

func TestHostAddInstallsThisMingosVersionOnAnSSHHostAndListsIt(t *testing.T) {
	h := newHome(t)
	h.installSSH()
	dir := h.addHost("box", "Linux x86_64", true)
	r := h.mingo("host", "add", "box", "me@box")
	if r.code == 0 {
		t.Fatal("a destination ssh cannot reach was added", r.out)
	}
	if r = h.mingo("host", "add", "box", "box"); r.code != 0 || !strings.HasSuffix(r.out, "host box: mingo "+testVersion+" on box\n") {
		t.Fatal(r.out)
	}
	if calls := h.read(filepath.Join(dir, "go-calls")); calls != "install github.com/Lindeneg/mingo/cmd/mingo@"+testVersion+"\n" {
		t.Fatal(calls)
	}
	if !exists(filepath.Join(dir, "home", ".local", "bin", "mingo")) || !exists(filepath.Join(dir, "home", ".tmux.conf")) {
		t.Fatal("mingo install did not run on the host")
	}
	if got := h.read(filepath.Join(h.config, "hosts.toml")); !strings.Contains(got, "[[hosts]]\nname = \"box\"\nssh = \"box\"\n") {
		t.Fatal(got)
	}
	if r = h.mingo("host", "ls"); r.code != 0 || r.out != "HOST  WHERE  MINGO\nbox   box    "+testVersion+"\n" {
		t.Fatalf("%q", r.out)
	}
	if r = h.mingo("host", "add", "box", "other"); r.code != 1 || !strings.Contains(r.out, "box is a host already, reached at box, mingo host rm box first") {
		t.Fatal(r.out)
	}
}

func TestHostAddRefusesAHostThatIsNotLinuxOrMacOSOrHasNoGo(t *testing.T) {
	h := newHome(t)
	h.installSSH()
	h.addHost("win", "MINGW64_NT-10.0 x86_64", true)
	h.addHost("bare", "Linux x86_64", false)
	if r := h.mingo("host", "add", "win", "win"); r.code != 1 || !strings.Contains(r.out, `win answers uname with "MINGW64_NT-10.0 x86_64", and a host is Linux or macOS`) {
		t.Fatal(r.out)
	}
	if r := h.mingo("host", "add", "bare", "bare"); r.code != 1 || !strings.Contains(r.out, "bare has no Go, which mingo is installed with there") {
		t.Fatal(r.out)
	}
	if exists(filepath.Join(h.config, "hosts.toml")) {
		t.Fatal("a refused host was kept")
	}
}

func TestHostAddLocalAddsThisMachineOnceAndRmTakesAHostOff(t *testing.T) {
	h := newHome(t)
	if r := h.mingo("host", "ls"); r.code != 0 || !strings.Contains(r.out, "so mingo works on this machine alone") {
		t.Fatal(r.out)
	}
	if r := h.mingo("host", "add", "here", "--local"); r.code != 0 || r.out != "host here: this machine\n" {
		t.Fatal(r.out)
	}
	if r := h.mingo("host", "add", "there", "--local"); r.code != 1 || !strings.Contains(r.out, "this machine is the host here already") {
		t.Fatal(r.out)
	}
	if r := h.mingo("host", "ls"); r.out != "HOST  WHERE         MINGO\nhere  this machine  "+testVersion+"\n" {
		t.Fatalf("%q", r.out)
	}
	if r := h.mingo("host", "rm", "here"); r.code != 0 || !strings.Contains(r.out, "host here: taken off the list") {
		t.Fatal(r.out)
	}
	if r := h.mingo("host", "rm", "here"); r.code != 1 || !strings.Contains(r.out, "no host named here") {
		t.Fatal(r.out)
	}
	if r := h.mingo("host", "add", "Bad", "--local"); r.code != 1 || !strings.Contains(r.out, `"Bad" is no host name`) {
		t.Fatal(r.out)
	}
	if r := h.mingo("host", "add", "x", "--lokal"); r.code != 2 || !strings.Contains(r.out, `unknown flag "--lokal"`) {
		t.Fatal(r.out)
	}
}

func TestHostLsSaysWhatKeptSSHFromAHost(t *testing.T) {
	h := newHome(t)
	h.installSSH()
	h.write(filepath.Join(h.config, "hosts.toml"), "[[hosts]]\nname = \"gone\"\nssh = \"nowhere\"\n")
	if r := h.mingo("host", "ls"); r.code != 0 || !strings.Contains(r.out, "gone  nowhere  nowhere: ssh: Could not resolve hostname nowhere") {
		t.Fatal(r.out)
	}
}

func TestAMingoAskedToBeAnotherVersionSaysSoWithItsOwnExitCode(t *testing.T) {
	h := newHome(t)
	if r := h.mingo("--expect", "v9.9.9", "version"); r.code != 75 || r.out != "mingo: this host runs mingo "+testVersion+", not v9.9.9\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("--expect", testVersion, "version"); r.code != 0 || r.out != "mingo "+testVersion+"\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestInstallOnAComputerWhoseHostsAreAllSSHHostsCopiesTheCommandAlone(t *testing.T) {
	h := newHome(t)
	h.write(filepath.Join(h.config, "hosts.toml"), "[[hosts]]\nname = \"box\"\nssh = \"box\"\n")
	r := h.mingo("install")
	if r.code != 0 || !strings.Contains(r.out, "so this machine needs the command alone") || strings.Contains(r.out, "claude settings") {
		t.Fatal(r.out)
	}
	if !exists(filepath.Join(h.home, ".local", "bin", "mingo")) || exists(filepath.Join(h.home, ".claude", "settings.json")) {
		t.Fatal("not the command alone")
	}
}
