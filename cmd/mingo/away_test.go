package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/config"
)

func TestAwayTellsTheRunningSessionsAndBackTellsThemTheOwnerIsBack(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addDemoWith(`modules = ["team"]`)
	h.setMode("listens")
	if r := h.mingo("start", "demo/lead"); r.code != 0 {
		t.Fatal(r.out)
	}
	h.mustWait(15, "the first start", h.startsAre(1))
	away := filepath.Join(h.config, "projects", "demo", "away")
	want := "demo: the owner is away, and the sessions work by Working alone in TEAM.md until mingo back demo\ndemo-lead: told\n"
	if r := h.mingo("away", "demo"); r.code != 0 || r.out != want || !exists(away) {
		t.Fatal(r.code, r.out)
	}
	h.mustWait(10, "the away message", func() bool { return h.typed() == config.AwayTold })
	if r := h.mingo("away", "demo"); r.code != 0 || r.out != "demo: the owner is already away\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("back", "demo"); r.code != 0 || r.out != "demo: the owner is back, and the sessions ask again\ndemo-lead: told\n" || exists(away) {
		t.Fatal(r.code, r.out)
	}
	h.mustWait(10, "the back message", func() bool { return h.typed() == config.AwayTold+"\n"+config.BackTold })
	if r := h.mingo("back", "demo"); r.code != 0 || r.out != "demo: the owner is not away\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestAFreshSessionsOpenerSaysTheOwnerIsAwayUntilTheOwnerIsBack(t *testing.T) {
	h := newHome(t)
	h.installStandIn()
	h.addDemoWith(`modules = ["team"]`)
	if r := h.mingo("away", "demo"); r.code != 0 || r.out != "demo: the owner is away, and the sessions work by Working alone in TEAM.md until mingo back demo\n" {
		t.Fatal(r.code, r.out)
	}
	args := filepath.Join(h.root, "args")
	h.write(filepath.Join(h.home, ".local", "bin", "claude"),
		"#!/usr/bin/env bash\nprintf '%s\\n' \"$1\" >>"+shellQuote(args)+"\nexit 0\n")
	h.setEnv("MINGO_LOOP_DELAY=0")
	h.mingo("start", "demo/lead")
	h.mustWait(10, "the first opener", func() bool { return h.read(args) != "" })
	if r := h.mingo("back", "demo"); r.code != 0 {
		t.Fatal(r.code, r.out)
	}
	away := "You are the lead of demo. " + config.AwayOpener
	h.mustWait(20, "an opener without the owner away", func() bool { return strings.HasSuffix(h.read(args), "\nYou are the lead of demo.\n") })
	if first, _, _ := strings.Cut(h.read(args), "\n"); first != away {
		t.Fatal(first)
	}
}

func TestAwayNeedsAProjectWithTheTeamModule(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	if r := h.mingo("away", "demo"); r.code != 1 || r.out != "mingo: demo has no team module, whose TEAM.md has the rules for when the owner is away\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("back"); r.code != 2 || r.out != "usage: mingo back <project>\n       mingo away <project>\n" {
		t.Fatal(r.code, r.out)
	}
	if _, err := os.Stat(filepath.Join(h.config, "projects", "demo")); err == nil {
		t.Fatal("a refused away wrote the mode")
	}
}
