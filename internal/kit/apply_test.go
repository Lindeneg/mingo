package kit

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/team"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

type world struct {
	t      *testing.T
	home   string
	origin string
	lead   string
	out    *bytes.Buffer
	gh     [][]string
	deps   Deps
	fsys   fstest.MapFS
	openPR string
	before func(args []string)
	config string
	claude string
	ghErr  string
}

func newWorld(t *testing.T, empty bool) *world {
	t.Helper()
	root := t.TempDir()
	gitconfig := filepath.Join(root, "gitconfig")
	os.WriteFile(gitconfig, []byte("[user]\n\tname = Kit Test\n\temail = kit@example.test\n[commit]\n\tgpgsign = false\n"), 0o644)
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("HOME", filepath.Join(root, "home"))
	w := &world{t: t, home: filepath.Join(root, "home"), origin: filepath.Join(root, "origin.git"), out: &bytes.Buffer{}}
	git(t, root, "init", "--quiet", "--bare", "--initial-branch=master", w.origin)
	w.lead = filepath.Join(w.home, "dev", "demo", "chief")
	os.MkdirAll(w.lead, 0o755)
	git(t, w.lead, "init", "--quiet", "--initial-branch=master")
	git(t, w.lead, "remote", "add", "origin", w.origin)
	if !empty {
		os.WriteFile(filepath.Join(w.lead, "README.md"), []byte("demo\n"), 0o644)
		git(t, w.lead, "add", "README.md")
		git(t, w.lead, "commit", "--quiet", "-m", "start")
		git(t, w.lead, "push", "--quiet", "origin", "master")
	}
	w.fsys = fstest.MapFS{
		"sessions/module.toml":                           {Data: []byte("does = \"sessions\"\nalways = true\nsetup = [\"memory\"]\n")},
		"skills/module.toml":                             {Data: []byte("does = \"skills\"\n")},
		"skills/files/.claude/skills/{{.Lead}}/SKILL.md": {Data: []byte("# {{.Lead}}\n\nversion one\n")},
		"skills/files/TEAM.md":                           {Data: []byte("{{range .Roles}}- {{.Name}}\n{{end}}")},
	}
	return w
}

func (w *world) apply() string {
	w.t.Helper()
	if err := w.try(); err != nil {
		w.t.Fatal(err)
	}
	return w.out.String()
}

func (w *world) try() error {
	w.t.Helper()
	w.prepare()
	return Apply(w.deps)
}

func (w *world) doctor() (int, string, error) {
	w.t.Helper()
	w.prepare()
	missing, err := Doctor(w.deps)
	return missing, w.out.String(), err
}

func (w *world) prepare() {
	w.t.Helper()
	k, err := Load(w.fsys)
	if err != nil {
		w.t.Fatal(err)
	}
	text := w.config
	if text == "" {
		text = "repo = \"x/demo\"\nmodules = [\"skills\"]\n[[roles]]\nname = \"chief\"\n[[roles]]\nname = \"helper\"\n"
	}
	p := project(w.t, text)
	if w.claude != "" {
		os.MkdirAll(filepath.Dir(config.ClaudeFile(p.Name)), 0o755)
		os.WriteFile(config.ClaudeFile(p.Name), []byte(w.claude), 0o644)
	}
	run := func(o team.Options, name string, args ...string) (team.Result, error) {
		if name == "git" && w.before != nil {
			w.before(args)
		}
		if name == "gh" {
			w.gh = append(w.gh, args)
			if w.ghErr != "" {
				return team.Result{Code: 1, Stderr: w.ghErr}, nil
			}
			if args[1] == "list" {
				if w.openPR != "" {
					return team.Result{Stdout: `[{"url":"` + w.openPR + `"}]`}, nil
				}
				return team.Result{Stdout: "[]"}, nil
			}
			return team.Result{Stdout: "https://github.com/x/demo/pull/7\n"}, nil
		}
		return team.Capture(o, name, args...)
	}
	w.out.Reset()
	w.deps = Deps{Kit: k, Project: p, Run: run, Out: w.out, TempRoot: w.t.TempDir(), Version: "v1", Home: w.home}
}

func (w *world) show(ref, path string) string {
	return git(w.t, w.origin, "show", ref+":"+path)
}

func (w *world) merge() {
	git(w.t, w.origin, "update-ref", "refs/heads/master", "refs/heads/"+Branch)
}

func TestApplyLandsTheFilesThroughAPullRequestAndASecondApplyChangesNothing(t *testing.T) {
	w := newWorld(t, false)
	out := w.apply()
	for _, line := range []string{"adds .claude/skills/chief/SKILL.md", "adds TEAM.md", "opened https://github.com/x/demo/pull/7"} {
		if !strings.Contains(out, line) {
			t.Fatal(line, out)
		}
	}
	if w.show(Branch, "TEAM.md") != "- chief\n- helper" || !strings.Contains(w.show(Branch, LockPath), `path = "TEAM.md"`) {
		t.Fatal(w.show(Branch, "TEAM.md"))
	}
	if git(t, w.origin, "log", "-1", "--format=%s", Branch) != "mingo kit: skills, sessions" {
		t.Fatal(git(t, w.origin, "log", "-1", "--format=%s", Branch))
	}
	create := w.gh[len(w.gh)-1]
	if strings.Join(create[:8], " ") != "pr create -R x/demo --base master --head mingo-kit" || !strings.Contains(create[len(create)-1], "- adds `TEAM.md` (skills)") {
		t.Fatal(create)
	}
	if _, err := os.Stat(filepath.Join(w.deps.Project.MemoryDir("helper"), "MEMORY.md")); err != nil {
		t.Fatal("no MEMORY.md for helper")
	}
	w.merge()
	before := git(t, w.origin, "rev-parse", Branch)
	if out := w.apply(); !strings.HasSuffix(out, "nothing to apply\n") || git(t, w.origin, "rev-parse", Branch) != before {
		t.Fatal(out)
	}
}

func TestApplyReplacesAFileStillAsRenderedAndLeavesAnEditedOneAloneWithItsChangeShown(t *testing.T) {
	w := newWorld(t, false)
	w.apply()
	w.merge()
	git(t, w.lead, "pull", "--quiet", "origin", "master")
	os.WriteFile(filepath.Join(w.lead, "TEAM.md"), []byte("- chief\n- helper\n- our own line\n"), 0o644)
	git(t, w.lead, "commit", "--quiet", "-am", "our team")
	git(t, w.lead, "push", "--quiet", "origin", "master")
	w.fsys["skills/files/.claude/skills/{{.Lead}}/SKILL.md"] = &fstest.MapFile{Data: []byte("# {{.Lead}}\n\nversion two\n")}
	w.fsys["skills/files/TEAM.md"] = &fstest.MapFile{Data: []byte("{{range .Roles}}* {{.Name}}\n{{end}}")}
	out := w.apply()
	if !strings.Contains(out, "updates .claude/skills/chief/SKILL.md") || !strings.Contains(out, "leaves TEAM.md alone, the project changed it") || !strings.Contains(out, "+* chief") {
		t.Fatal(out)
	}
	if w.show(Branch, "TEAM.md") != "- chief\n- helper\n- our own line" || w.show(Branch, ".claude/skills/chief/SKILL.md") != "# chief\n\nversion two" {
		t.Fatal(w.show(Branch, "TEAM.md"))
	}
}

func TestApplyMakesTheFirstCommitOfAnEmptyRepository(t *testing.T) {
	w := newWorld(t, true)
	out := w.apply()
	if !strings.Contains(out, "the first commit on master: mingo kit: skills, sessions") || w.show("master", "TEAM.md") != "- chief\n- helper" || len(w.gh) != 0 {
		t.Fatal(out, w.gh)
	}
}

func TestApplyAndDoctorNeedTheLeadsClone(t *testing.T) {
	w := newWorld(t, false)
	os.RemoveAll(w.lead)
	k, _ := Load(w.fsys)
	p := project(t, "modules = [\"skills\"]\n[[roles]]\nname = \"chief\"\n")
	err := Apply(Deps{Kit: k, Project: p, Run: team.Capture, Out: w.out, TempRoot: t.TempDir(), Home: w.home})
	if err == nil || !strings.Contains(err.Error(), "run mingo setup demo first") {
		t.Fatal(err)
	}
}

func TestDoctorSaysWhatIsInPlaceAndWhatIsMissing(t *testing.T) {
	w := newWorld(t, false)
	w.fsys["guards/module.toml"] = &fstest.MapFile{Data: []byte("does = \"guards\"\nchecks = [\"guards\"]\n")}
	w.fsys["sessions/module.toml"] = &fstest.MapFile{Data: []byte("does = \"sessions\"\nalways = true\nchecks = [\"clones\", \"memory\"]\n")}
	k, _ := Load(w.fsys)
	p := project(t, "modules = [\"skills\", \"guards\"]\n[[roles]]\nname = \"chief\"\n[[roles]]\nname = \"helper\"\n")
	d := Deps{Kit: k, Project: p, Run: team.Capture, Out: w.out, TempRoot: t.TempDir(), Version: "v1", Home: w.home}
	missing, err := Doctor(d)
	if err != nil {
		t.Fatal(err)
	}
	out := w.out.String()
	for _, line := range []string{
		"    in place: the clone of chief, " + w.lead,
		"    missing: the clone of helper, " + filepath.Join(w.home, "dev", "demo", "helper") + " (mingo setup demo)",
		"    missing: chief's MEMORY.md pointing at handoff-chief.md (mingo setup demo)",
		"    missing: the write and kill guards in " + filepath.Join(w.home, ".claude", "settings.json") + " (mingo install)",
		"    missing: TEAM.md (mingo apply demo)",
	} {
		if !strings.Contains(out, line+"\n") {
			t.Fatal(line, "\n", out)
		}
	}
	if missing != 7 {
		t.Fatal(missing, out)
	}
}

func (w *world) pushToKit(path, text string) {
	w.t.Helper()
	dir := filepath.Join(w.t.TempDir(), "other")
	git(w.t, filepath.Dir(dir), "clone", "--quiet", "--branch", Branch, w.origin, dir)
	os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o755)
	os.WriteFile(filepath.Join(dir, path), []byte(text), 0o644)
	git(w.t, dir, "add", "-A")
	git(w.t, dir, "commit", "--quiet", "-m", "a fix during review")
	git(w.t, dir, "push", "--quiet", "origin", Branch)
}

func TestApplyBuildsOnTheOpenPullRequestsBranchAndKeepsWhatWasPushedToIt(t *testing.T) {
	w := newWorld(t, false)
	w.apply()
	w.pushToKit("TEAM.md", "- chief\n- helper\n- a reviewer's line\n")
	fix := git(t, w.origin, "rev-parse", Branch)
	w.openPR = "https://github.com/x/demo/pull/7"
	w.fsys["skills/files/.claude/skills/{{.Lead}}/SKILL.md"] = &fstest.MapFile{Data: []byte("# {{.Lead}}\n\nversion two\n")}
	out := w.apply()
	if !strings.Contains(out, "updates .claude/skills/chief/SKILL.md") || !strings.HasSuffix(out, "pushed to the open pull request https://github.com/x/demo/pull/7\n") {
		t.Fatal(out)
	}
	if git(t, w.origin, "rev-parse", Branch+"^") != fix || w.show(Branch, "TEAM.md") != "- chief\n- helper\n- a reviewer's line" {
		t.Fatal("the fix on the branch was lost")
	}
}

func TestApplyRefusesToPushOverABranchThatMovedMeanwhile(t *testing.T) {
	for _, open := range []string{"", "https://github.com/x/demo/pull/7"} {
		w := newWorld(t, false)
		w.apply()
		w.openPR = open
		w.fsys["skills/files/TEAM.md"] = &fstest.MapFile{Data: []byte("{{range .Roles}}* {{.Name}}\n{{end}}")}
		moved := false
		w.before = func(args []string) {
			if args[0] == "push" && !moved {
				moved = true
				w.pushToKit("other.md", "someone else\n")
			}
		}
		k, _ := Load(w.fsys)
		p := project(t, "repo = \"x/demo\"\nmodules = [\"skills\"]\n[[roles]]\nname = \"chief\"\n[[roles]]\nname = \"helper\"\n")
		w.deps.Kit, w.deps.Project = k, p
		w.out.Reset()
		err := Apply(w.deps)
		if err == nil || !strings.Contains(err.Error(), "mingo-kit moved while mingo applied, run mingo apply demo again") {
			t.Fatal(open, err)
		}
		if w.show(Branch, "other.md") != "someone else" {
			t.Fatal("the other push was lost")
		}
	}
}

func TestApplyRemovesAFileNoModuleRendersWhenItIsStillAsRenderedAndKeepsAnEditedOne(t *testing.T) {
	w := newWorld(t, false)
	w.fsys["skills/files/GONE.md"] = &fstest.MapFile{Data: []byte("gone\n")}
	w.apply()
	w.merge()
	git(t, w.lead, "pull", "--quiet", "origin", "master")
	os.WriteFile(filepath.Join(w.lead, "TEAM.md"), []byte("- chief\n- helper\n- ours\n"), 0o644)
	git(t, w.lead, "commit", "--quiet", "-am", "our team")
	git(t, w.lead, "push", "--quiet", "origin", "master")
	delete(w.fsys, "skills/files/GONE.md")
	delete(w.fsys, "skills/files/TEAM.md")
	out := w.apply()
	if !strings.Contains(out, "removes GONE.md, which no module renders now\n") || !strings.Contains(out, "keeps TEAM.md, which no module renders now and the project changed\n") {
		t.Fatal(out)
	}
	if r := exec.Command("git", "-C", w.origin, "cat-file", "-e", Branch+":GONE.md"); r.Run() == nil {
		t.Fatal("GONE.md is still there")
	}
	lock := w.show(Branch, LockPath)
	if strings.Contains(lock, "GONE.md") || strings.Contains(lock, "TEAM.md") || w.show(Branch, "TEAM.md") != "- chief\n- helper\n- ours" {
		t.Fatal(lock)
	}
}

func TestApplySaysWhenTheProjectHasItsOwnFileAtAPath(t *testing.T) {
	w := newWorld(t, false)
	os.WriteFile(filepath.Join(w.lead, "TEAM.md"), []byte("our own team\n"), 0o644)
	git(t, w.lead, "add", "TEAM.md")
	git(t, w.lead, "commit", "--quiet", "-m", "ours")
	git(t, w.lead, "push", "--quiet", "origin", "master")
	if out := w.apply(); !strings.Contains(out, "leaves TEAM.md alone, the project has its own, and mingo renders:\n") {
		t.Fatal(out)
	}
}

func TestApplySaysWhatGitSaidWhenAPushFailsForAnotherReasonThanABranchThatMoved(t *testing.T) {
	w := newWorld(t, false)
	hook := filepath.Join(w.origin, "hooks", "pre-receive")
	os.WriteFile(hook, []byte("#!/bin/sh\necho 'permission to x/demo denied' >&2\nexit 1\n"), 0o755)
	err := w.try()
	if err == nil || !strings.Contains(err.Error(), `could not push the commit "mingo kit: skills, sessions" to the branch mingo-kit of `+w.origin) ||
		!strings.Contains(err.Error(), "permission to x/demo denied") || strings.Contains(err.Error(), "moved") {
		t.Fatal(err)
	}
}

func TestApplySaysWhatIsWrongWithTheLeadsCloneAndSendsToSetupOnlyWhenThereIsNone(t *testing.T) {
	w := newWorld(t, false)
	git(t, w.lead, "remote", "remove", "origin")
	if err := w.try(); err == nil || !strings.Contains(err.Error(), "the lead's clone "+w.lead+" has no remote origin, which mingo works through, add it with git remote add origin and the URL of x/demo") {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(w.lead, ".git"))
	if err := w.try(); err == nil || !strings.Contains(err.Error(), "chief's folder "+w.lead+" is no git clone, move it aside and run mingo setup demo\ngit rev-parse failed:") {
		t.Fatal(err)
	}
	parent := filepath.Dir(w.lead)
	git(t, parent, "init", "--quiet")
	top, _ := filepath.EvalSymlinks(parent)
	if err := w.try(); err == nil || !strings.Contains(err.Error(), "chief's folder "+w.lead+" is inside the repository "+top+", not a clone of its own, move it aside and run mingo setup demo") {
		t.Fatal(err)
	}
	if len(w.gh) != 0 {
		t.Fatal(w.gh)
	}
}

func TestApplyNamesTheProjectsRepositoryInEveryPullRequestCallSinceGhPicksAForksParent(t *testing.T) {
	w := newWorld(t, false)
	w.apply()
	w.openPR = "https://github.com/x/demo/pull/7"
	w.fsys["skills/files/TEAM.md"] = &fstest.MapFile{Data: []byte("{{range .Roles}}* {{.Name}}\n{{end}}")}
	w.apply()
	seen := map[string]bool{}
	for _, call := range w.gh {
		if call[0] != "pr" {
			continue
		}
		seen[call[1]] = true
		if call[2] != "-R" || call[3] != "x/demo" {
			t.Fatal(call)
		}
	}
	if !seen["create"] || !seen["list"] {
		t.Fatal(w.gh)
	}
}

func TestApplyLeavesOutTheHeavyScriptsAndThePortsOfAnEmptyRepositoryAndDoctorReportsThem(t *testing.T) {
	w := newWorld(t, true)
	w.fsys["heavy/module.toml"] = &fstest.MapFile{Data: []byte("does = \"heavy\"\n")}
	w.fsys["ports/module.toml"] = &fstest.MapFile{Data: []byte("does = \"ports\"\nsetup = [\"ports\"]\nchecks = [\"ports\"]\n")}
	w.config = "repo = \"x/demo\"\nmodules = [\"skills\", \"heavy\", \"ports\"]\n[heavy]\nscripts = [\"check\", \"test\"]\n[ports]\nvariable = \"APP_PORT_OFFSET\"\n[ports.offsets]\nchief = 10\n[[roles]]\nname = \"chief\"\n"
	out := w.apply()
	for _, line := range []string{
		"ports: leaves out " + filepath.Join(w.lead, ".env") + ", the clone has no .env.example yet, and mingo doctor reports it until it has\n",
		"leaves out the heavy scripts check, test, the repository has no package.json yet, and mingo doctor reports it until it has\n",
		"the first commit on master: mingo kit: skills, sessions\n",
	} {
		if !strings.Contains(out, line) {
			t.Fatal(line, out)
		}
	}
	missing, out, err := w.doctor()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"    missing: chief's .env with APP_PORT_OFFSET=10 (add .env.example with an empty APP_PORT_OFFSET= line, then mingo apply demo)\n",
		"    missing: the heavy scripts check, test in package.json, which the repository does not have (add it, then mingo apply demo)\n",
	} {
		if !strings.Contains(out, line) {
			t.Fatal(line, out)
		}
	}
	if missing != 2 {
		t.Fatal(missing, out)
	}
}

func TestDoctorRunsACheckThatTwoModulesShareOnce(t *testing.T) {
	w := newWorld(t, false)
	w.fsys["workflow/module.toml"] = &fstest.MapFile{Data: []byte("does = \"workflow\"\nchecks = [\"labels\"]\n")}
	w.fsys["tracker/module.toml"] = &fstest.MapFile{Data: []byte("does = \"tracker\"\nchecks = [\"labels\"]\n")}
	w.config = "repo = \"x/demo\"\nmodules = [\"skills\", \"workflow\", \"tracker\"]\n[tracker]\nkind = \"github\"\n[[roles]]\nname = \"chief\"\n"
	w.apply()
	w.merge()
	missing, out, err := w.doctor()
	if err != nil || strings.Count(out, "the label ci-unavailable on x/demo") != 1 || strings.Count(out, "the label chief on x/demo") != 1 || missing != 2 {
		t.Fatal(missing, err, out)
	}
}

func TestDoctorTellsTheOwnerToMakeTheLinearTeamWithExactlyItsIdentifier(t *testing.T) {
	w := newWorld(t, false)
	w.fsys["tracker/module.toml"] = &fstest.MapFile{Data: []byte("does = \"tracker\"\nchecks = [\"linear\"]\n")}
	w.config = "repo = \"x/demo\"\nmodules = [\"skills\", \"tracker\"]\n[tracker]\nkind = \"linear\"\nteam = \"DEMO\"\n[[roles]]\nname = \"chief\"\n"
	w.apply()
	w.merge()
	missing, out, err := w.doctor()
	want := "    only you can: the Linear connector on the Claude account the sessions use, at claude.ai under Settings, Connectors\n    only you can: the Linear team DEMO, made in Linear with exactly that identifier, since the connector cannot make a team\n"
	if err != nil || !strings.Contains(out, want) || missing != 0 {
		t.Fatal(missing, err, out)
	}
}

func TestDoctorSaysWhatItCouldNotCheckAndCountsNoneOfItAsMissing(t *testing.T) {
	w := newWorld(t, false)
	w.fsys["workflow/module.toml"] = &fstest.MapFile{Data: []byte("does = \"workflow\"\nchecks = [\"labels\"]\n")}
	w.config = "repo = \"x/demo\"\nmodules = [\"skills\", \"workflow\"]\n[[roles]]\nname = \"chief\"\n"
	w.apply()
	w.merge()
	w.ghErr = "HTTP 401: Bad credentials (https://api.github.com/graphql)\nTry authenticating with:  gh auth login"
	missing, out, err := w.doctor()
	want := "    could not check the labels on x/demo: gh label list failed:\n      HTTP 401: Bad credentials (https://api.github.com/graphql)\n      Try authenticating with:  gh auth login\n"
	if !strings.Contains(out, want) || strings.Contains(out, "missing: the label") || missing != 0 {
		t.Fatal(missing, out)
	}
	if err == nil || err.Error() != "could not check 1 of what the modules need, see above" {
		t.Fatal(err)
	}
}
