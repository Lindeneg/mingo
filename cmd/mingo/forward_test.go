//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a host box with this test's mingo, the project shop with the roles lead and dev and their clones,
// and a stand-in for Claude Code that ends at /exit and keeps what it is told, beside this machine as
// the host here with its own project mine
func (h *home) twoHosts() string {
	h.t.Helper()
	h.installSSH()
	dir := h.addHost("box", "Linux x86_64", true)
	home := filepath.Join(dir, "home")
	bin := filepath.Join(home, ".local", "bin")
	data, err := os.ReadFile(h.bin)
	if err != nil {
		h.t.Fatal(err)
	}
	h.write(filepath.Join(bin, "mingo"), string(data))
	os.Chmod(filepath.Join(bin, "mingo"), 0o755)
	h.write(filepath.Join(bin, "claude"), "#!/usr/bin/env bash\ncase \"${1:-}\" in\n    auth) echo \"box is logged in\"; exit 0 ;;\n    agents) echo \"[]\"; exit 0 ;;\nesac\n"+
		"printf '%s\\n' \"$$\" >>"+shellQuote(filepath.Join(dir, "starts"))+"\nwhile IFS= read -r line; do printf '%s\\n' \"$line\" >>"+shellQuote(filepath.Join(dir, "input"))+"; [[ $line == /exit ]] && exit 0; done\n")
	os.Chmod(filepath.Join(bin, "claude"), 0o755)
	h.write(filepath.Join(dir, "config", "projects", "shop.toml"), "repo = \"x/shop\"\n[[roles]]\nname = \"lead\"\n[[roles]]\nname = \"dev\"\n")
	for _, role := range []string{"lead", "dev"} {
		h.mkdir(filepath.Join(home, "dev", "shop", role))
	}
	h.addProject("mine", `repo = "x/mine"`, `[[roles]]`, `name = "solo"`)
	h.mkdir(filepath.Join(h.home, "dev", "mine", "solo"))
	h.write(filepath.Join(h.config, "hosts.toml"), "[[hosts]]\nname = \"here\"\nlocal = true\n\n[[hosts]]\nname = \"box\"\nssh = \"box\"\n")
	return dir
}

func TestACommandRunsOnTheHostOfItsProjectAndLsAgentsAndStatusShowEveryHost(t *testing.T) {
	h := newHome(t)
	dir := h.twoHosts()
	if r := h.mingo("ls"); r.code != 0 || !strings.Contains(r.out, "here  mine (x/mine)\n") || !strings.Contains(r.out, "box   shop (x/shop)\n") ||
		!strings.Contains(r.out, "box     lead         "+filepath.Join(dir, "home", "dev", "shop", "lead")) {
		t.Fatal(r.out)
	}
	if r := h.mingo("path", "shop/dev"); r.code != 0 || r.out != filepath.Join(dir, "home", "dev", "shop", "dev")+"\n" {
		t.Fatal(r.out)
	}
	if r := h.mingo("path", "mine"); r.code != 0 || r.out != filepath.Join(h.home, "dev", "mine", "solo")+"\n" {
		t.Fatal(r.out)
	}
	if r := h.mingo("start", "shop/lead"); r.code != 0 || !strings.Contains(r.out, "shop-lead: started") {
		t.Fatal(r.out)
	}
	h.mustWait(10, "the session on box", func() bool { return strings.TrimSpace(h.read(filepath.Join(dir, "starts"))) != "" })
	r := h.mingo("agents")
	if r.code != 0 || !strings.HasPrefix(r.out, "HOST  PROJECT  ROLE  STATE") || !strings.Contains(r.out, "\nhere  mine     solo  stopped") ||
		!strings.Contains(r.out, "\nbox   shop     lead  ") || !strings.Contains(r.out, "\nbox   shop     dev   stopped") {
		t.Fatal(r.out)
	}
	if r := h.mingo("status", "shop"); r.code != 0 || !strings.Contains(r.out, "box   shop-dev: not running") || !strings.Contains(r.out, "box   box is logged in") {
		t.Fatal(r.out)
	}
	if r := h.mingo("say", "shop/lead", "pull master"); r.code != 0 {
		t.Fatal(r.out)
	}
	h.mustWait(10, "the message on box", func() bool { return strings.Contains(h.read(filepath.Join(dir, "input")), "pull master\n") })
	if r := h.mingo("stop", "shop"); r.code != 0 || !strings.Contains(r.out, "shop-lead: stopped") {
		t.Fatal(r.out)
	}
	if calls := h.read(filepath.Join(dir, "calls")); !strings.Contains(calls, "~/.local/bin/mingo '--expect' '"+testVersion+"' 'say' 'shop/lead' 'pull master'") {
		t.Fatal(calls)
	}
}

func TestAProjectOnTwoHostsIsRefusedNamingBoth(t *testing.T) {
	h := newHome(t)
	h.twoHosts()
	h.addProject("shop", `[[roles]]`, `name = "lead"`)
	if r := h.mingo("path", "shop/lead"); r.code != 1 || !strings.Contains(r.out, "the project shop is on the hosts here and box, rename one of them") {
		t.Fatal(r.out)
	}
	if r := h.mingo("path", "nothing/lead"); r.code != 1 || !strings.Contains(r.out, "no host has a project named nothing") {
		t.Fatal(r.out)
	}
}

func TestAHostOnAnotherVersionGetsThisOneInstalledAndTheCommandRunsAgain(t *testing.T) {
	h := newHome(t)
	dir := h.twoHosts()
	old := filepath.Join(dir, "home", ".local", "bin", "mingo")
	h.write(old, "#!/bin/sh\nif [ \"$1\" = --expect ]; then echo \"mingo: this host runs mingo v0.0.1, not $2\" >&2; exit 75; fi\necho mingo v0.0.1\n")
	os.Chmod(old, 0o755)
	r := h.mingo("path", "shop/lead")
	if r.code != 0 || !strings.Contains(r.out, "mingo: installing mingo "+testVersion+" on the host box\n") || !strings.HasSuffix(r.out, filepath.Join(dir, "home", "dev", "shop", "lead")+"\n") {
		t.Fatal(r.out)
	}
	if calls := h.read(filepath.Join(dir, "go-calls")); !strings.Contains(calls, "install github.com/Lindeneg/mingo/cmd/mingo@"+testVersion) {
		t.Fatal(calls)
	}
}

func TestNewAndProjectAddNeedAHostWhenThereAreSeveral(t *testing.T) {
	h := newHome(t)
	h.twoHosts()
	if r := h.mingo("new", "x", "--preset", "bare"); r.code != 1 || !strings.Contains(r.out, "mingo new needs --host, one of here box") {
		t.Fatal(r.out)
	}
	if r := h.mingo("project", "add", "x", "--host", "nope"); r.code != 1 || !strings.Contains(r.out, "no host named nope") {
		t.Fatal(r.out)
	}
	if r := h.mingo("project", "add", "fresh", "--host", "box"); r.code != 0 || !exists(filepath.Join(h.root, "hosts", "box", "config", "projects", "fresh.toml")) {
		t.Fatal(r.out)
	}
}

func TestTheDefaultProjectIsThisComputersAndNamesAProjectOfAnyHost(t *testing.T) {
	h := newHome(t)
	dir := h.twoHosts()
	if r := h.mingo("default", "shop"); r.code != 0 {
		t.Fatal(r.out)
	}
	if r := h.mingo("default"); r.code != 0 || r.out != "shop\n" {
		t.Fatal(r.out)
	}
	if r := h.mingo("path", "dev"); r.code != 0 || r.out != filepath.Join(dir, "home", "dev", "shop", "dev")+"\n" {
		t.Fatal(r.out)
	}
}
