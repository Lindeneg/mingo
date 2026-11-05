//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func slug(path string) string {
	var b strings.Builder
	for _, c := range path {
		if c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func TestProjectAddWritesAConfigThatLoadsAndRefusesToOverwrite(t *testing.T) {
	h := newHome(t)
	r := h.mingo("project", "add", "fresh")
	if r.out != filepath.Join(h.config, "projects", "fresh.toml")+"\n" {
		t.Fatal(r.out)
	}
	if r := h.mingo("path", "fresh/lead"); r.out != h.home+"/dev/fresh/lead\n" {
		t.Fatal(r.out)
	}
	if r := h.mingo("ls"); !strings.HasPrefix(r.out, "fresh\n  lead ") {
		t.Fatal(r.out)
	}
	if r := h.mingo("project", "add", "fresh"); r.code == 0 {
		t.Fatal(r.out)
	}
}

func TestProjectEditRunsAnEditorThatTakesArguments(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	editor := filepath.Join(h.root, "editor")
	said := filepath.Join(h.root, "said")
	h.write(editor, "#!/usr/bin/env bash\necho \"$*\" >"+shellQuote(said)+"\n")
	if err := os.Chmod(editor, 0o755); err != nil {
		t.Fatal(err)
	}
	if r := h.runIn("", []string{"EDITOR=" + editor + " --wait"}, h.bin, "project", "edit", "demo"); r.code != 0 {
		t.Fatal(r.out)
	}
	if h.read(said) != "--wait "+filepath.Join(h.config, "projects", "demo.toml")+"\n" {
		t.Fatal(h.read(said))
	}
}

func TestPathMemoryRepoAndDefaultAnswerForATarget(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	if r := h.mingo("path", "demo/h"); r.out != h.home+"/dev/demo/helper\n" {
		t.Fatal(r.out)
	}
	memory := filepath.Join(h.home, ".claude", "projects", slug(h.home+"/dev/demo/lead"), "memory")
	h.mkdir(memory)
	if r := h.mingo("memory", "demo/lead"); r.out != memory+"\n" {
		t.Fatal(r.out)
	}
	h.touch(filepath.Join(memory, "handoff-lead.md"))
	if r := h.mingo("memory", "demo/lead"); r.out != memory+"/handoff-lead.md\n" {
		t.Fatal(r.out)
	}
	if r := h.mingo("repo", "demo"); r.out != "Lindeneg/demo\n" {
		t.Fatal(r.out)
	}
	h.mingo("default", "demo")
	if r := h.mingo("default"); r.out != "demo\n" {
		t.Fatal(r.out)
	}
}

func TestAProjectIDWorksInTargetsAndShowsInLs(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	h.addProject("rather-long-project", `id = "rl"`, `repo = "acme/rather-long-project"`, `[[roles]]`, `name = "lead"`, `[[roles]]`, `name = "dev1"`)
	h.mkdir(filepath.Join(h.home, "dev", "rather-long-project", "dev1"))
	if r := h.mingo("path", "rl/dev1"); r.out != h.home+"/dev/rather-long-project/dev1\n" {
		t.Fatal(r.out)
	}
	if r := h.mingo("ls"); !strings.Contains(r.out, "rather-long-project [rl] (acme/rather-long-project)\n") {
		t.Fatal(r.out)
	}
	h.mingo("default", "rl")
	if got := h.read(filepath.Join(h.config, "default")); got != "rather-long-project\n" {
		t.Fatal(got)
	}
	if r := h.mingo("project", "show", "rl"); !strings.Contains(r.out, `id = "rl"`) {
		t.Fatal(r.out)
	}
	if r := h.mingo("stop", "rl"); r.out != "rather-long-project-lead: not running\nrather-long-project-dev1: not running\n" {
		t.Fatal(r.out)
	}
}

func (h *home) fakeTool(name, script string) {
	file := filepath.Join(h.home, ".local", "bin", name)
	h.write(file, "#!/usr/bin/env bash\n"+script)
	if err := os.Chmod(file, 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func TestSetupClonesTrustsTheNewCloneAndMakesMemory(t *testing.T) {
	h := newHome(t)
	h.fakeTool("gh", `case "$1 $2" in
    "repo clone") mkdir -p "$4" && echo cloned >"$4/README" ;;
esac
exit 0
`)
	h.fakeTool("git", `[[ "$1 $2" == "config --global" ]] && { echo someone; exit 0; }
exec /usr/bin/git "$@"
`)
	h.addProject("demo", `repo = "Lindeneg/demo"`, `[[roles]]`, `name = "lead"`, `[[roles]]`, `name = "helper"`)
	h.mkdir(filepath.Join(h.home, "dev", "demo", "helper"))
	h.write(filepath.Join(h.home, ".claude.json"), "{\n  \"numStartups\": 3,\n  \"projects\": {\n    \"/elsewhere\": {\n      \"hasTrustDialogAccepted\": false\n    }\n  },\n  \"zLast\": true\n}\n")
	lead, helper := h.home+"/dev/demo/lead", h.home+"/dev/demo/helper"
	r := h.mingo("setup", "demo")
	if r.code != 0 {
		t.Fatal(r.out)
	}
	if !strings.Contains(r.out, "trusted: "+lead+"\n") || !strings.Contains(r.out, "exists: "+helper+"\ntrusted: "+helper+"\n") {
		t.Fatal(r.out)
	}
	want := "{\n  \"numStartups\": 3,\n  \"projects\": {\n    \"/elsewhere\": {\n      \"hasTrustDialogAccepted\": false\n    },\n" +
		"    \"" + lead + "\": {\n      \"hasTrustDialogAccepted\": true\n    },\n" +
		"    \"" + helper + "\": {\n      \"hasTrustDialogAccepted\": true\n    }\n  },\n  \"zLast\": true\n}\n"
	if got := h.read(filepath.Join(h.home, ".claude.json")); got != want {
		t.Fatalf("got\n%s", got)
	}
	for _, role := range []string{"lead", "helper"} {
		if !exists(filepath.Join(h.home, ".claude", "projects", slug(h.home+"/dev/demo/"+role), "memory")) {
			t.Fatal("no memory folder for", role)
		}
	}
}

func TestSetupTrustsAndFillsTheEnvOfACloneThatWasThereBefore(t *testing.T) {
	h := newHome(t)
	h.addProject("demo", `[[roles]]`, `name = "lead"`, `[[roles]]`, `name = "helper"`, `[ports]`, `variable = "APP_PORT_OFFSET"`, `[ports.offsets]`, `lead = 10`, `helper = 20`)
	lead, helper := h.home+"/dev/demo/lead", h.home+"/dev/demo/helper"
	h.write(filepath.Join(lead, ".env.example"), "APP_PORT_OFFSET=\n")
	h.mkdir(helper)
	h.write(filepath.Join(h.home, ".claude.json"), "{broken")
	if r := h.mingo("setup", "demo"); r.code != 1 || r.out != "exists: "+lead+"\nmingo: "+filepath.Join(h.home, ".claude.json")+" is not valid JSON at line 1, column 2 (invalid character 'b' looking for beginning of object key string), left as it is\n" {
		t.Fatal(r.code, r.out)
	}
	h.write(filepath.Join(h.home, ".claude.json"), "{}\n")
	r := h.mingo("setup", "demo")
	if r.code != 0 || !strings.Contains(r.out, "trusted: "+lead+"\n") || !strings.Contains(r.out, "trusted: "+helper+"\nports: leaves out "+helper+"/.env, the clone has no .env.example yet") {
		t.Fatal(r.code, r.out)
	}
	if h.read(filepath.Join(lead, ".env")) != "APP_PORT_OFFSET=10\n" || !strings.Contains(h.read(filepath.Join(h.home, ".claude.json")), helper) {
		t.Fatal(h.read(filepath.Join(h.home, ".claude.json")))
	}
}

func TestSetupSaysAMissingToolIsNotInstalledRatherThanNotLoggedIn(t *testing.T) {
	h := newHome(t)
	h.addProject("demo", `repo = "Lindeneg/demo"`, `[[roles]]`, `name = "lead"`)
	only := "PATH=" + filepath.Join(h.root, "nothing")
	if r := h.runIn("", []string{only}, h.bin, "setup", "demo"); r.code != 1 || r.out != "mingo: gh is not installed, see https://cli.github.com\n" {
		t.Fatal(r.code, r.out)
	}
	h.fakeTool("gh", "exit 0\n")
	if r := h.runIn("", []string{only}, h.bin, "setup", "demo"); r.code != 1 || r.out != "mingo: git is not installed, see https://git-scm.com/downloads\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestACloneOrAnEditorThatFailsIsNamedInTheError(t *testing.T) {
	h := newHome(t)
	h.fakeTool("gh", `[[ "$1 $2" == "repo clone" ]] && { echo "gh: could not clone" >&2; exit 1; }
exit 0
`)
	h.fakeTool("git", `[[ "$1 $2" == "config --global" ]] && { echo someone; exit 0; }
exec /usr/bin/git "$@"
`)
	h.addProject("demo", `repo = "Lindeneg/demo"`, `[[roles]]`, `name = "lead"`)
	lead := h.home + "/dev/demo/lead"
	if r := h.mingo("setup", "demo"); r.code != 1 || r.out != "gh: could not clone\nmingo: gh repo clone Lindeneg/demo "+lead+" failed: exit status 1\n" {
		t.Fatal(r.code, r.out)
	}
	file := filepath.Join(h.config, "projects", "demo.toml")
	if r := h.runIn("", []string{"EDITOR=false"}, h.bin, "project", "edit", "demo"); r.code != 1 || r.out != "mingo: false "+file+" failed: exit status 1\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.runIn("", []string{"EDITOR=/nowhere/editor --wait"}, h.bin, "project", "edit", "demo"); r.code != 1 || r.out != "mingo: the editor /nowhere/editor is not installed, set EDITOR to one that is\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestProjectEditSaysWhenTheConfigNoLongerLoadsAndKeepsTheFile(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	h.fakeTool("breaks", `printf 'roles = 1\n' >"$1"`)
	file := filepath.Join(h.config, "projects", "demo.toml")
	r := h.runIn("", []string{"EDITOR=breaks"}, h.bin, "project", "edit", "demo")
	if r.code != 1 || !strings.HasPrefix(r.out, "mingo: "+file+": ") || !strings.HasSuffix(r.out, ", the file is kept as you left it, mingo project edit demo opens it again\n") {
		t.Fatal(r.code, r.out)
	}
	if h.read(file) != "roles = 1\n" {
		t.Fatal(h.read(file))
	}
}

func TestDefaultPrintsTheOneSetAndSaysWhenNoneIsOrItIsGone(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	h.addProject("broken", `roles = 1`)
	if r := h.runIn("", []string{"MINGO_PROJECT=demo"}, h.bin, "default"); r.code != 1 || r.out != "mingo: no default project is set, mingo default <project> sets one\n" {
		t.Fatal(r.code, r.out)
	}
	h.write(filepath.Join(h.config, "default"), "gone\n")
	if r := h.mingo("default"); r.code != 1 || r.out != "mingo: the default project is gone, which no longer exists, mingo default <project> sets another\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("default", "nope"); r.code != 1 || r.out != "mingo: no project named nope, see mingo project add nope\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("default", "broken"); r.code != 1 || !strings.HasPrefix(r.out, "mingo: "+filepath.Join(h.config, "projects", "broken.toml")+": ") {
		t.Fatal(r.code, r.out)
	}
	if h.read(filepath.Join(h.config, "default")) != "gone\n" {
		t.Fatal("a refused default was written")
	}
}
