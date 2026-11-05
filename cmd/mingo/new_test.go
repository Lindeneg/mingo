//go:build unix

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// a gh that knows x/demo, whose package.json has check, test and build, and cannot clone it
func (h *home) fakeRepoOnly() {
	h.fakeTool("gh", `case "$1 $2" in
  "auth status"|"auth setup-git") exit 0 ;;
  "repo view") [ "$3" = x/demo ] && exit 0; echo "GraphQL: Could not resolve to a Repository with the name '$3'. (repository)" >&2; exit 1 ;;
  "api -H") echo '{"scripts": {"check": "tsc", "test": "vitest", "build": "vite build"}}' ;;
  "repo clone") echo "fatal: could not read from remote repository" >&2; exit 1 ;;
  *) echo "fake gh: $*" >&2; exit 1 ;;
esac
`)
}

func TestNewChecksTheNameAndEveryFlagBeforeItWritesAnything(t *testing.T) {
	h := newHome(t)
	h.fakeRepoOnly()
	file := filepath.Join(h.config, "projects", "demo.toml")
	h.addProject("shop", `id = "sh"`, `[[roles]]`, `name = "lead"`)
	for want, args := range map[string][]string{
		`mingo: --roles: "Lead" is not a role name, use lowercase letters, digits and hyphens`:                {"demo", "--preset", "bare", "--repo", "x/demo", "--roles", "Lead"},
		"mingo: --repo: there is no repository x/nope on GitHub, or gh's account cannot see it":               {"demo", "--preset", "bare", "--repo", "x/nope"},
		"mingo: --ci-run is only for --ci-stack go or other, not pnpm":                                        {"demo", "--preset", "solo", "--repo", "x/demo", "--ci-run=test=go test"},
		"mingo: --repo is given 2 times, give it once":                                                        {"demo", "--preset=bare", "--repo", "x/demo", "--repo=x/demo"},
		"mingo: --roles has an empty value":                                                                   {"demo", "--preset", "bare", "--repo", "x/demo", "--roles", ""},
		"mingo: --repo needs a value":                                                                         {"demo", "--preset", "bare", "--repo", "--roles", "lead"},
		`mingo: "extra" is not a flag, mingo new takes the name and then --<key> <value> flags`:               {"demo", "extra"},
		`mingo: "Demo" is not a project name, use lowercase letters, digits and hyphens`:                      {"Demo", "--preset", "bare"},
		"mingo: sh is the id of the project shop, pick another name":                                          {"sh", "--preset", "bare"},
		"mingo: shop is already a project, its config is " + filepath.Join(h.config, "projects", "shop.toml"): {"shop", "--preset", "bare"},
		"mingo: mingo new needs --preset, one of bare solo team full":                                         {"demo"},
	} {
		r := h.mingo(append([]string{"new"}, args...)...)
		if r.code != 1 || r.out != want+"\n" || exists(file) {
			t.Fatalf("%v\n%d %q", args, r.code, r.out)
		}
	}
	if r := h.mingo("new", "demo", "--preset", "bare", "--help"); r.code != 0 || !strings.HasPrefix(r.out, "mingo new <name>") {
		t.Fatal(r.out)
	}
}

func TestNewKeepsTheConfigWhenTheCloneFailsAndSaysHowToFinish(t *testing.T) {
	h := newHome(t)
	h.fakeRepoOnly()
	h.write(filepath.Join(h.home, ".gitconfig"), "[user]\n\tname = Kit Test\n\temail = kit@example.test\n")
	file := filepath.Join(h.config, "projects", "demo.toml")
	r := h.mingo("new", "demo", "--preset", "bare", "--repo", "x/demo")
	want := "\nthe config is kept at " + file + ", finish with mingo setup demo, then mingo apply demo\n"
	if r.code != 1 || !strings.HasPrefix(r.out, "config: "+file+"\n") || !strings.Contains(r.out, "\nmingo: ") || !strings.HasSuffix(r.out, want) || !exists(file) {
		t.Fatalf("%d %q", r.code, r.out)
	}
}

func TestNewTakesBackAConfigThatDoesNotLoadAndSaysWhyPlainly(t *testing.T) {
	h := newHome(t)
	h.fakeRepoOnly()
	file := filepath.Join(h.config, "projects", "demo.toml")
	// a home that is not absolute leaves the clone it names relative, which a load refuses, and its
	// bin folder is already on the path, last, so the fake gh is found by its absolute path first
	env := []string{"HOME=home", "PATH=" + filepath.Join(h.home, ".local", "bin") + ":/usr/bin:/bin:home/.local/bin"}
	r := h.runIn("", env, h.bin, "new", "demo", "--preset", "bare", "--repo", "x/demo")
	if r.code != 1 || !strings.HasPrefix(r.out, "mingo: mingo new made a config that does not load, and took it back: it has the clone ") || strings.Contains(r.out, file) || strings.Count(r.out, "\n") != 1 || exists(file) {
		t.Fatalf("%d %q", r.code, r.out)
	}
}
