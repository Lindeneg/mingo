//go:build unix

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func (h *home) gitIn(dir string, args ...string) {
	h.t.Helper()
	if r := h.runIn(dir, []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}, "git", args...); r.code != 0 {
		h.t.Fatal(args, r.out)
	}
}

func (h *home) teamClone() string {
	h.t.Helper()
	h.addDemoWith(`[workflow]`, `base = "trunk"`, `gate = ["true", "false", "true"]`)
	lead := filepath.Join(h.home, "dev", "demo", "lead")
	origin := filepath.Join(h.root, "origin.git")
	h.gitIn(h.root, "init", "--quiet", "--bare", "--initial-branch=trunk", origin)
	h.gitIn(lead, "init", "--quiet", "--initial-branch=trunk")
	h.gitIn(lead, "remote", "add", "origin", origin)
	h.mkdir(filepath.Join(lead, "sub"))
	return lead
}

func TestGateAndPrTakeTheProjectOfTheCloneTheyRunIn(t *testing.T) {
	h := newHome(t)
	lead := h.teamClone()
	r := h.runIn(filepath.Join(lead, "sub"), nil, h.bin, "gate", "logs/g.log")
	want := []string{
		"green  true (0 s)",
		"red    false (exit 1, 0 s)",
		"the gate is red at false; its output is in " + filepath.Join(lead, "logs", "g.log"),
	}
	if r.code != 1 || strings.Join(r.lines(), "\n") != strings.Join(want, "\n") {
		t.Fatal(r.code, r.out)
	}
	if r := h.runIn(lead, nil, h.bin, "pr", "rebase", "trunk"); r.code != 1 || r.out != "trunk is never rebased\n" {
		t.Fatal(r.code, r.out)
	}
	if r := h.runIn(lead, nil, h.bin, "pr", "merge"); r.code != 2 || r.out != "usage: mingo pr merge <pull request number>\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestGateAndPrTakeMingoProjectOverTheFolder(t *testing.T) {
	h := newHome(t)
	lead := h.teamClone()
	h.addProject("other", `clone = "~/dev/demo/{role}"`, `[workflow]`, `base = "elsewhere"`, `[[roles]]`, `name = "lead"`)
	if r := h.runIn(lead, []string{"MINGO_PROJECT=other"}, h.bin, "pr", "rebase", "elsewhere"); r.code != 1 || r.out != "elsewhere is never rebased\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestGateAndPrRefuseARepositoryThatIsNoneOfTheProjectsClones(t *testing.T) {
	h := newHome(t)
	lead := h.teamClone()
	h.addProject("other", `[workflow]`, `base = "elsewhere"`, `[[roles]]`, `name = "lead"`)
	for _, args := range [][]string{{"gate", "g.log"}, {"pr", "rebase", "elsewhere"}, {"pr", "merge", "1"}} {
		r := h.runIn(lead, []string{"MINGO_PROJECT=other"}, h.bin, args...)
		if r.code != 1 || r.out != "mingo: "+lead+" is none of other's clones\n" {
			t.Fatal(args, r.code, r.out)
		}
	}
}

func TestGateAndPrOutsideAProjectsCloneSayWhereToRunThem(t *testing.T) {
	h := newHome(t)
	h.addDemo()
	for _, args := range [][]string{{"gate", "g.log"}, {"pr", "merge", "1"}} {
		r := h.mingo(args...)
		if r.code != 1 || r.out != "mingo: run it in a project's clone, or with MINGO_PROJECT set\n" {
			t.Fatal(args, r.code, r.out)
		}
	}
	lead := filepath.Join(h.home, "dev", "demo", "lead")
	if r := h.runIn(lead, nil, h.bin, "gate", "g.log"); r.code != 1 || r.out != "mingo: "+lead+" is in no git repository\n" {
		t.Fatal(r.code, r.out)
	}
}

func TestApplyOfABareProjectSeedsTheMemoryAndDoctorSaysWhatIsMissing(t *testing.T) {
	h := newHome(t)
	h.addDemoWith(`modules = ["guards"]`)
	r := h.mingo("apply", "demo")
	memory := filepath.Join(h.home, ".claude", "projects", slug(h.home+"/dev/demo/helper"), "memory", "MEMORY.md")
	if r.code != 1 || !strings.Contains(r.out, "memory: "+memory+"\n") || !strings.HasSuffix(r.out, "run mingo setup demo first\n") {
		t.Fatal(r.code, r.out)
	}
	if h.read(memory) != "- [Handoff](handoff-helper.md): read first, where the work stands and the next step\n" {
		t.Fatal(h.read(memory))
	}
	r = h.mingo("doctor", "demo")
	if r.code != 1 || !strings.Contains(r.out, "    in place: helper's MEMORY.md pointing at handoff-helper.md\n") ||
		!strings.Contains(r.out, "    missing: the write and kill guards in "+filepath.Join(h.home, ".claude", "settings.json")+" (mingo install)\n") {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("doctor", "nope"); r.code != 1 || !strings.Contains(r.out, "no project named nope") {
		t.Fatal(r.out)
	}
}

// a gh that clones from a local origin and keeps labels, rulesets, secrets and pull requests in files
func (h *home) fakeGitHub(origin string) string {
	state := filepath.Join(h.root, "github")
	h.mkdir(state)
	h.fakeTool("gh", `state=`+shellQuote(state)+`
echo "$*" >>"$state/calls"
list() { [ -s "$state/$1" ] && sed 's/.*/{"name":"&"}/' "$state/$1" | paste -sd, | sed 's/^/[/;s/$/]/' || echo '[]'; }
case "$1 $2" in
  "auth status"|"auth setup-git") exit 0 ;;
  "repo clone") exec git clone --quiet `+shellQuote(origin)+` "$4" ;;
  "label list") list labels ;;
  "label create") echo "$3" >>"$state/labels" ;;
  "secret list") list secrets ;;
  "pr list") echo '[]' ;;
  "pr create") echo "https://github.com/x/demo/pull/1" ;;
  "api -X") cat >"$state/ruleset.json"; echo default >>"$state/rulesets" ;;
  "api repos/x/demo/rulesets") list rulesets ;;
  *) echo "fake gh: $*" >&2; exit 1 ;;
esac
`)
	return state
}

func TestNewMakesAFullProjectAndASecondApplyChangesNothing(t *testing.T) {
	h := newHome(t)
	h.write(filepath.Join(h.home, ".gitconfig"), "[user]\n\tname = Kit Test\n\temail = kit@example.test\n[commit]\n\tgpgsign = false\n")
	origin := filepath.Join(h.root, "origin.git")
	seed := filepath.Join(h.root, "seed")
	h.gitIn(h.root, "init", "--quiet", "--bare", "--initial-branch=master", origin)
	h.gitIn(h.root, "init", "--quiet", "--initial-branch=master", seed)
	h.write(filepath.Join(seed, "package.json"), "{\n  \"name\": \"demo\",\n  \"scripts\": {\n    \"check\": \"tsc\",\n    \"test\": \"vitest\",\n    \"build\": \"vite build\",\n    \"dev\": \"vite\"\n  }\n}\n")
	h.write(filepath.Join(seed, ".env.example"), "APP_PORT_OFFSET=\n")
	h.write(filepath.Join(seed, "CLAUDE.md"), "# demo\n")
	h.runIn(seed, nil, "git", "add", "-A")
	h.runIn(seed, nil, "git", "commit", "--quiet", "-m", "start")
	h.runIn(seed, nil, "git", "push", "--quiet", origin, "master")
	state := h.fakeGitHub(origin)

	r := h.mingo("new", "demo", "--preset", "full", "--repo", "x/demo", "--roles", "lead,dev", "--tracker-kind", "github", "--ports-variable", "APP_PORT_OFFSET")
	if r.code != 0 {
		t.Fatal(r.out)
	}
	for _, line := range []string{
		"config: " + filepath.Join(h.config, "projects", "demo.toml"),
		"ports: " + h.home + "/dev/demo/dev/.env, APP_PORT_OFFSET=20",
		"label: ci-unavailable on x/demo", "label: dev on x/demo", "ruleset: default on x/demo",
		"adds .claude/skills/task-flow/SKILL.md", "adds .claude/skills/lead/SKILL.md", "adds TEAM.md", "updates CLAUDE.md", "updates package.json",
		"opened https://github.com/x/demo/pull/1",
	} {
		if !strings.Contains(r.out, line+"\n") {
			t.Fatal(line, "\n", r.out)
		}
	}
	show := func(path string) string {
		return h.runIn(origin, nil, "git", "show", "mingo-kit:"+path).out
	}
	if show("CLAUDE.md") != "# demo\n\nNo attribution footers in commits, pull requests or comments.\n@TEAM.md\n" || !strings.Contains(show("package.json"), `"check:run": "tsc"`) || !strings.Contains(show(".github/workflows/ci.yml"), "  security:") {
		t.Fatal(show("CLAUDE.md"), show("package.json"))
	}
	h.gitIn(origin, "update-ref", "refs/heads/master", "refs/heads/mingo-kit")
	before := h.runIn(origin, nil, "git", "rev-parse", "mingo-kit").out
	if r := h.mingo("apply", "demo"); r.code != 0 || !strings.HasSuffix(r.out, "nothing to apply\n") || h.runIn(origin, nil, "git", "rev-parse", "mingo-kit").out != before {
		t.Fatal(r.out)
	}
	r = h.mingo("doctor", "demo")
	if r.code != 1 || !strings.Contains(r.out, "    missing: the review's token, the secret CLAUDE_CODE_OAUTH_TOKEN on x/demo (only you can: claude setup-token, then mingo secret review demo)\n") ||
		!strings.Contains(r.out, "    in place: TEAM.md\n") || !strings.Contains(r.out, "    in place: the ruleset default on x/demo\n") {
		t.Fatal(r.code, r.out)
	}
	if !strings.Contains(h.read(filepath.Join(state, "ruleset.json")), `"non_fast_forward"`) {
		t.Fatal("no ruleset posted")
	}
}

func TestNewWithLocalClaudeFilesKeepsThemInEveryCloneAndOutOfTheRepository(t *testing.T) {
	h := newHome(t)
	h.write(filepath.Join(h.home, ".gitconfig"), "[user]\n\tname = Kit Test\n\temail = kit@example.test\n[commit]\n\tgpgsign = false\n")
	origin := filepath.Join(h.root, "origin.git")
	seed := filepath.Join(h.root, "seed")
	h.gitIn(h.root, "init", "--quiet", "--bare", "--initial-branch=master", origin)
	h.gitIn(h.root, "init", "--quiet", "--initial-branch=master", seed)
	h.write(filepath.Join(seed, "package.json"), "{\n  \"name\": \"demo\",\n  \"scripts\": {\n    \"check\": \"tsc\",\n    \"test\": \"vitest\",\n    \"build\": \"vite build\",\n    \"dev\": \"vite\"\n  }\n}\n")
	h.write(filepath.Join(seed, "README.md"), "# demo\n")
	h.runIn(seed, nil, "git", "add", "-A")
	h.runIn(seed, nil, "git", "commit", "--quiet", "-m", "start")
	h.runIn(seed, nil, "git", "push", "--quiet", origin, "master")
	h.fakeGitHub(origin)

	r := h.mingo("new", "demo", "--preset", "team", "--repo", "x/demo", "--roles", "lead,dev", "--tracker-kind", "github", "--claude-files", "local")
	if r.code != 0 {
		t.Fatal(r.out)
	}
	for _, line := range []string{
		"adds .github/workflows/ci.yml", "updates package.json", "opened https://github.com/x/demo/pull/1",
		"lead: adds /CLAUDE.md /TEAM.md /.claude/ to .git/info/exclude", "lead: adds CLAUDE.md", "lead: adds TEAM.md",
		"dev: adds .claude/skills/task-flow/SKILL.md", "dev: adds .claude/settings.json",
	} {
		if !strings.Contains(r.out, line+"\n") {
			t.Fatal(line, "\n", r.out)
		}
	}
	for _, path := range []string{"CLAUDE.md", "TEAM.md", ".claude/settings.json", ".claude/skills/task-flow/SKILL.md"} {
		if h.runIn(origin, nil, "git", "cat-file", "-e", "mingo-kit:"+path).code == 0 {
			t.Fatal(path, "is in the pull request")
		}
	}
	if review := h.runIn(origin, nil, "git", "show", "mingo-kit:.github/workflows/review.yml").out; !strings.Contains(review, "Read README.md first.") {
		t.Fatal(review)
	}
	for _, role := range []string{"lead", "dev"} {
		clone := filepath.Join(h.home, "dev", "demo", role)
		if h.read(filepath.Join(clone, "CLAUDE.md")) != "No attribution footers in commits, pull requests or comments.\n@TEAM.md\n" || !exists(filepath.Join(clone, "TEAM.md")) {
			t.Fatal(role, h.read(filepath.Join(clone, "CLAUDE.md")))
		}
		if status := h.runIn(clone, nil, "git", "status", "--porcelain").out; status != "" {
			t.Fatal(role, status)
		}
	}
	h.gitIn(origin, "update-ref", "refs/heads/master", "refs/heads/mingo-kit")
	if r := h.mingo("apply", "demo"); r.code != 0 || !strings.HasSuffix(r.out, "nothing to apply\n") {
		t.Fatal(r.out)
	}
	h.fakeTool("fake-editor", `printf '# demo rules\n' >"$1"`)
	if r := h.runIn(h.root, []string{"EDITOR=fake-editor"}, h.bin, "claude", "edit", "demo"); r.code != 0 {
		t.Fatal(r.out)
	}
	if r := h.mingo("apply", "demo"); !strings.Contains(r.out, "lead: updates CLAUDE.md\n") || !strings.Contains(r.out, "dev: updates CLAUDE.md\n") {
		t.Fatal(r.out)
	}
	if got := h.read(filepath.Join(h.home, "dev", "demo", "dev", "CLAUDE.md")); got != "# demo rules\n\nNo attribution footers in commits, pull requests or comments.\n@TEAM.md\n" {
		t.Fatal(got)
	}
	h.addProject("kept", `[[roles]]`, `name = "lead"`)
	if r := h.mingo("claude", "edit", "kept"); r.code != 1 || !strings.Contains(r.out, "kept has claude_files repo, so its CLAUDE.md is in its repository") {
		t.Fatal(r.code, r.out)
	}
	if r := h.mingo("doctor", "demo"); !strings.Contains(r.out, "  Claude's files in dev's clone\n    in place: Claude's files out of git in .git/info/exclude\n") || !strings.Contains(r.out, "    in place: TEAM.md\n") {
		t.Fatal(r.out)
	}
}

func TestNewTakesBackAConfigThatDoesNotLoad(t *testing.T) {
	h := newHome(t)
	file := filepath.Join(h.config, "projects", "demo.toml")
	r := h.mingo("new", "demo", "--preset", "bare", "--repo", "x/demo", "--roles", "Lead")
	if r.code == 0 || !strings.Contains(r.out, "has a role named Lead") || strings.Contains(r.out, "config: ") || exists(file) {
		t.Fatal(r.code, r.out)
	}
}
