package kit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/team"
)

const nodeManifest = `{"name": "demo", "scripts": {"check": "tsc", "test": "vitest", "build": "vite build", "dev": "vite"}}`

// a gh that knows the repositories of repos, each with its package.json or none for ""
func fakeGH(repos map[string]string) team.Runner {
	return func(o team.Options, name string, args ...string) (team.Result, error) {
		switch {
		case len(args) >= 3 && args[0] == "repo" && args[1] == "view":
			if _, ok := repos[args[2]]; ok {
				return team.Result{Stdout: `{"name":"demo"}`}, nil
			}
			return team.Result{Code: 1, Stderr: "GraphQL: Could not resolve to a Repository with the name '" + args[2] + "'. (repository)\n"}, nil
		case len(args) == 4 && args[0] == "api" && args[2] == "Accept: application/vnd.github.raw+json":
			repo := strings.TrimSuffix(strings.TrimPrefix(args[3], "repos/"), "/contents/package.json")
			if text := repos[repo]; text != "" {
				return team.Result{Stdout: text}, nil
			}
			return team.Result{Code: 1, Stdout: `{"message":"Not Found","status":"404"}`, Stderr: "gh: Not Found (HTTP 404)\n"}, nil
		}
		return team.Result{Code: 1, Stderr: "fake gh: " + strings.Join(args, " ")}, nil
	}
}

var someRepos = map[string]string{"x/demo": nodeManifest, "x/y": nodeManifest, "x/bare": ""}

// a terminal whose answers to each question come in turn, an empty one being enter
type script struct {
	t        *testing.T
	answers  map[string][]string
	asked    []string
	problems []string
	inputs   []string
}

func (s *script) next(q string) (string, bool) {
	list := s.answers[q]
	if len(list) == 0 {
		return "", false
	}
	s.answers[q] = list[1:]
	return list[0], true
}

func (s *script) Choose(q string, values, hints []string, def, problem string) (string, error) {
	s.asked = append(s.asked, q+" ["+def+"]")
	if problem != "" {
		s.problems = append(s.problems, problem)
	}
	if len(hints) != len(values) {
		s.t.Fatal("hints", values, hints)
	}
	if a, ok := s.next(q); ok && a != "" {
		if !contains(values, a) {
			s.t.Fatalf("%q is not a choice of %q", a, q)
		}
		return a, nil
	}
	if def == "" {
		return values[0], nil
	}
	return def, nil
}

func (s *script) Text(q, def, input, problem string, check func(string) error) (string, error) {
	s.asked = append(s.asked, q+" ["+def+"]")
	if input != "" {
		s.inputs = append(s.inputs, input)
	}
	if problem != "" {
		s.problems = append(s.problems, problem)
	}
	for {
		a, left := s.next(q)
		if a == "^C" {
			return "", errors.New("stopped")
		}
		v := a
		if v == "" {
			v = def
		}
		err := check(v)
		if err == nil {
			return v, nil
		}
		if isFinal(err) {
			return "", err
		}
		s.problems = append(s.problems, err.Error())
		if !left {
			s.t.Fatalf("no answer left for %q, which says %v", q, err)
		}
	}
}

func newKit(t *testing.T) *Kit {
	t.Helper()
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// mingo new without a terminal
func newFrom(t *testing.T, flags map[string][]string) (Made, error) {
	t.Helper()
	return newKit(t).New("demo", flags, NewDeps{Run: fakeGH(someRepos)})
}

func newAt(t *testing.T, s *script, flags map[string][]string) (Made, error) {
	t.Helper()
	s.t = t
	return newKit(t).New("demo", flags, NewDeps{Ask: s, Run: fakeGH(someRepos)})
}

func TestNewWritesAConfigThatLoadsFromThePresetAndTheFlags(t *testing.T) {
	k := newKit(t)
	flags := map[string][]string{
		"--preset":          {"full"},
		"--repo":            {"x/demo"},
		"--roles":           {"lead, dev"},
		"--ci-stack":        {"go"},
		"--workflow-checks": {"vet, test, security"},
		"--ci-run":          {"vet=go vet ./...", "test=go test ./..."},
		"--tracker-kind":    {"linear"},
		"--tracker-team":    {"DEM"},
		"--ports-variable":  {"APP_PORT_OFFSET"},
		"--ports-offsets":   {"dev=25"},
	}
	made, err := k.New("demo", flags, NewDeps{Run: fakeGH(someRepos)})
	if err != nil {
		t.Fatal(err)
	}
	text := made.TOML
	for _, line := range []string{
		`repo = "x/demo"`, `lead = "lead"`, `checks = ["vet", "test", "security"]`, "[ci.run]\nvet = \"go vet ./...\"\ntest = \"go test ./...\"",
		`kind = "linear"`, `team = "DEM"`, `project = "demo"`, "[ports.offsets]\nlead = 10\ndev = 25", "[[roles]]\nname = \"dev\"", `base = "master"`,
	} {
		if !strings.Contains(text, line) {
			t.Fatal(line, "\n", text)
		}
	}
	if strings.Contains(text, "heavy") || contains(made.Modules, "heavy") {
		t.Fatal("heavy is for a Node stack\n", text)
	}
	p := project(t, text)
	if mods, err := k.For(p); err != nil || len(mods) != len(k.Modules)-1 {
		t.Fatal(mods, err)
	}
	if made.Repo != "x/demo" || strings.Join(made.Roles, " ") != "lead dev" || made.Local {
		t.Fatal(made)
	}
}

func TestNewNamesTheReviewBotAsTheReviewerOnlyWithTheReviewModule(t *testing.T) {
	solo, err := newFrom(t, map[string][]string{"--preset": {"solo"}, "--repo": {"x/demo"}, "--ci-stack": {"go"}, "--workflow-checks": {"test"}, "--ci-run": {"test=go test ./..."}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(solo.TOML, "\nreviewer = \"claude[bot]\"\n") || project(t, solo.TOML).Workflow.Reviewer != "claude[bot]" {
		t.Fatal(solo.TOML)
	}
	bare, err := newFrom(t, map[string][]string{"--preset": {"bare"}, "--repo": {"x/demo"}})
	if err != nil || strings.Contains(bare.TOML, "reviewer") {
		t.Fatal(bare.TOML, err)
	}
}

func TestNewAsksWhatTheFlagsLeaveOutAndSaysWhatItNeedsWithoutATerminal(t *testing.T) {
	s := &script{answers: map[string][]string{"the preset": {"solo"}, "the GitHub repository, as owner/name": {"x/demo"}}}
	made, err := newAt(t, s, nil)
	if err != nil || !strings.Contains(made.TOML, `repo = "x/demo"`) || !strings.Contains(made.TOML, `stack = "pnpm"`) {
		t.Fatal(made.TOML, err)
	}
	if s.asked[0] != "the preset []" || s.asked[1] != "the GitHub repository, as owner/name []" || s.asked[3] != "the roles, in the order they start [lead]" {
		t.Fatal(s.asked)
	}
	for want, flags := range map[string]map[string][]string{
		"mingo new needs --preset, one of bare solo team full":             nil,
		"mingo new needs --repo, the GitHub repository":                    {"--preset": {"bare"}},
		"--roles: the solo preset has one role, not 2":                     {"--preset": {"solo"}, "--repo": {"x/y"}, "--roles": {"a,b"}},
		"mingo new knows no --colour":                                      {"--preset": {"bare"}, "--colour": {"red"}},
		`--preset: "huge" is not one of bare, solo, team or full`:          {"--preset": {"huge"}},
		`--lead: "boss" is none of the roles lead`:                         {"--preset": {"bare"}, "--repo": {"x/y"}, "--lead": {"boss"}},
		"mingo new needs --ci-run test=..., the command of the check test": {"--preset": {"solo"}, "--repo": {"x/y"}, "--ci-stack": {"go"}, "--workflow-checks": {"test"}},
	} {
		if _, err := newFrom(t, flags); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, "\n", err)
		}
	}
}

func TestNewWritesStringsTOMLReadsBack(t *testing.T) {
	odd := "a \"quoted\" \\ title\twith\x01control"
	made, err := newFrom(t, map[string][]string{"--preset": {"bare"}, "--repo": {"x/y"}, "--title": {odd}})
	if err != nil {
		t.Fatal(err)
	}
	if p := project(t, made.TOML); p.Title != odd {
		t.Fatalf("%q\n%s", p.Title, made.TOML)
	}
}

func TestNewKeepsClaudesFilesLocalWhenAskedAndTheReviewThenReadsTheReadme(t *testing.T) {
	s := &script{answers: map[string][]string{"the preset": {"solo"}, "the GitHub repository, as owner/name": {"x/demo"}, "where Claude's files go": {"local"}}}
	made, err := newAt(t, s, nil)
	if err != nil || !strings.Contains(made.TOML, "\nclaude_files = \"local\"\n") || !strings.Contains(made.TOML, "\nread = \"README.md first\"\n") || !made.Local {
		t.Fatal(made.TOML, err)
	}
	if !strings.Contains(strings.Join(s.asked, "\n"), "what the AI review of each pull request reads before the diff [README.md first]") || !project(t, made.TOML).ClaudeLocal() {
		t.Fatal(s.asked)
	}
	kept, err := newFrom(t, map[string][]string{"--preset": {"solo"}, "--repo": {"x/demo"}, "--claude-files": {"repo"}})
	if err != nil || strings.Contains(kept.TOML, "claude_files") || !strings.Contains(kept.TOML, "\nread = \"CLAUDE.md first\"\n") {
		t.Fatal(kept.TOML, err)
	}
}

func TestNewAsksTheStackOtherForItsSetupEachChecksCommandAndItsAudit(t *testing.T) {
	s := &script{answers: map[string][]string{
		"the GitHub repository, as owner/name":                                         {"x/demo"},
		"how CI installs the project":                                                  {"other"},
		"the commands CI runs to set up the project before each check, empty for none": {"make deps, make tools"},
		"the CI jobs every pull request must pass, such as test or build":              {"test, security"},
		"the command of the check test":                                                {"make test"},
		"the dependency audit the security job runs, empty for none":                   {"make audit"},
	}}
	made, err := newAt(t, s, map[string][]string{"--preset": {"solo"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(made.TOML, "[ci]\nstack = \"other\"\nsetup = [\"make deps\", \"make tools\"]\naudit = \"make audit\"\n\n[ci.run]\ntest = \"make test\"\n") {
		t.Fatal(made.TOML)
	}
	if p := project(t, made.TOML); p.CI.Audit != "make audit" || len(p.CI.Setup) != 2 {
		t.Fatal(p.CI)
	}
	bare, err := newFrom(t, map[string][]string{"--preset": {"solo"}, "--repo": {"x/demo"}, "--ci-stack": {"other"}, "--workflow-checks": {"test"}, "--ci-run": {"test=make test"}})
	if err != nil || strings.Contains(bare.TOML, "setup") || strings.Contains(bare.TOML, "audit") {
		t.Fatal(bare.TOML, err)
	}
	s = &script{answers: map[string][]string{}}
	if _, err := newAt(t, s, map[string][]string{"--preset": {"solo"}, "--repo": {"x/demo"}}); err != nil {
		t.Fatal(err)
	}
	if all := strings.Join(s.asked, "\n"); strings.Contains(all, "set up the project") || strings.Contains(all, "dependency audit") || strings.Contains(all, "the command of the check") {
		t.Fatal(all)
	}
}

func TestEachFlagIsCheckedAsItsAnswerIsAndFailsAtOnceWithoutATerminal(t *testing.T) {
	for want, flags := range map[string]map[string][]string{
		`--repo: "foo" is not owner/name, such as you/shop`:                                             {"--repo": {"foo"}},
		`--repo: "-x/y" is not owner/name`:                                                              {"--repo": {"-x/y"}},
		`--repo: "x/.." is not owner/name`:                                                              {"--repo": {"x/.."}},
		`--repo: "x/demo.git" is not owner/name, such as you/shop, without https://github.com/ or .git`: {"--repo": {"x/demo.git"}},
		`--workflow-gate: "pnpm check && pnpm test" is a gate command, which mingo runs without a shell, so && does not work there, put the line in a package script or a script file`: {"--preset": {"solo"}, "--workflow-gate": {"pnpm lint, pnpm check && pnpm test"}},
		"--repo: there is no repository x/nope on GitHub, or gh's account cannot see it":                                                                                               {"--repo": {"x/nope"}},
		`--roles: "Dev" is not a role name, use lowercase letters, digits and hyphens`:                                                                                                 {"--roles": {"lead,Dev"}},
		"--roles: the role lead is named twice":                                                                                     {"--roles": {"lead, lead"}},
		`--roles: "lead,,dev" has an empty item, separate the items with single commas`:                                             {"--roles": {"lead,,dev"}},
		`--claude-files: "git" is not one of repo or local`:                                                                         {"--claude-files": {"git"}},
		`--tracker-kind: "linear2" is not one of linear, github or none`:                                                            {"--preset": {"team"}, "--tracker-kind": {"linear2"}},
		`--workflow-checks: "type check" is not a GitHub job id, start it with a letter or _ and use only letters, digits, - and _`: {"--preset": {"solo"}, "--workflow-checks": {"lint, type check"}},
		"--workflow-checks: the check test is named twice":                                                                          {"--preset": {"solo"}, "--workflow-checks": {"test,test"}},
		`--workflow-gate: "pnpm test," has an empty item`:                                                                           {"--preset": {"solo"}, "--workflow-gate": {"pnpm test,"}},
		`--ports-offsets dev: "ten" is not a whole number`:                                                                          {"--preset": {"full"}, "--roles": {"lead,dev"}, "--ports-variable": {"P"}, "--ports-offsets": {"dev=ten"}},
		`--ports-offsets lead: "-5" is not a whole number`:                                                                          {"--preset": {"full"}, "--ports-variable": {"P"}, "--ports-offsets": {"lead=-5"}},
		"--heavy-scripts: the package.json of x/demo has no scripts lint or e2e":                                                    {"--preset": {"team"}, "--heavy-scripts": {"check, lint, e2e"}},
	} {
		if flags["--preset"] == nil {
			flags["--preset"] = []string{"bare"}
		}
		if flags["--repo"] == nil {
			flags["--repo"] = []string{"x/demo"}
		}
		if _, err := newFrom(t, flags); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, "\n", err)
		}
	}
}

func TestAFlagThatDoesNotApplyOrIsGivenWrongIsAnErrorEvenAtATerminal(t *testing.T) {
	cases := []struct {
		want string
		// whether the flags alone show it, so a terminal is asked nothing first
		early bool
		flags map[string][]string
	}{
		{"--ci-run is only for --ci-stack go or other, not pnpm", false, map[string][]string{"--preset": {"solo"}, "--ci-run": {"test=x"}}},
		{"--ci-run is only for --ci-stack go or other, not npm", true, map[string][]string{"--preset": {"solo"}, "--ci-stack": {"npm"}, "--ci-run": {"test=x"}}},
		{"--tracker-team is only for --tracker-kind linear, not github", false, map[string][]string{"--preset": {"team"}, "--tracker-team": {"ENG"}}},
		{"--heavy-scripts is only for --ci-stack pnpm or npm, not go", true, map[string][]string{"--preset": {"team"}, "--ci-stack": {"go"}, "--ci-run": {"check=a", "test=b", "build=c"}, "--heavy-scripts": {"check"}}},
		{"--ci-run names tset, which is none of the checks vet and test", true, map[string][]string{"--preset": {"solo"}, "--ci-stack": {"go"}, "--workflow-checks": {"vet,test,security"}, "--ci-run": {"vet=go vet", "tset=go test"}}},
		{"--ports-offsets names bob, which is none of the roles lead", false, map[string][]string{"--preset": {"full"}, "--ports-variable": {"P"}, "--ports-offsets": {"bob=10"}}},
		{"--ports-offsets names bob, which is none of the roles lead and dev", true, map[string][]string{"--preset": {"full"}, "--roles": {"lead,dev"}, "--ports-variable": {"P"}, "--ports-offsets": {"bob=10"}}},
		{"--repo is given 2 times, give it once", true, map[string][]string{"--preset": {"bare"}, "--repo": {"x/y", "x/demo"}}},
		{"--preset is given 2 times, give it once", true, map[string][]string{"--preset": {"bare", "solo"}}},
		{"--repo has an empty value", true, map[string][]string{"--preset": {"bare"}, "--repo": {"  "}}},
		{"--roles has an empty value", true, map[string][]string{"--preset": {"bare"}, "--roles": {""}}},
		{`--ci-run takes <check>=<value>, not "test"`, true, map[string][]string{"--preset": {"solo"}, "--ci-stack": {"go"}, "--ci-run": {"test"}}},
		{`--ci-run takes <check>=<value>, not "test="`, true, map[string][]string{"--preset": {"solo"}, "--ci-stack": {"go"}, "--ci-run": {"test="}}},
		{"--ci-run names test twice", true, map[string][]string{"--preset": {"solo"}, "--ci-stack": {"go"}, "--ci-run": {"test=a", "test=b"}}},
		{"--tracker-team belongs to the tracker module, which the solo preset leaves out", true, map[string][]string{"--preset": {"solo"}, "--tracker-team": {"ENG"}}},
	}
	for _, c := range cases {
		if c.flags["--repo"] == nil {
			c.flags["--repo"] = []string{"x/demo"}
		}
		if _, err := newFrom(t, c.flags); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatal(c.want, "\n", err)
		}
		s := &script{answers: map[string][]string{"the GitHub repository, as owner/name": {"x/demo"}}}
		if _, err := newAt(t, s, c.flags); err == nil || !strings.Contains(err.Error(), c.want) || c.early != (len(s.asked) == 0) {
			t.Fatal("at a terminal ", c.want, "\n", err, s.asked)
		}
	}
	s := &script{answers: map[string][]string{"the GitHub repository, as owner/name": {"x/demo"}, "the tracker": {"none"}}}
	if _, err := newAt(t, s, map[string][]string{"--preset": {"team"}, "--tracker-team": {"ENG"}}); err == nil || err.Error() != "--tracker-team is only for --tracker-kind linear, not none" {
		t.Fatal(err)
	}
}

func TestAtATerminalABadAnswerSaysWhyAndTheQuestionIsAskedAgain(t *testing.T) {
	s := &script{answers: map[string][]string{
		"the GitHub repository, as owner/name": {"", "foo", "x/nope", "x/demo"},
		"the roles, in the order they start":   {"lead,Dev", "lead, dev"},
		"the role that merges":                 {"boss", ""},
		"the package.json scripts too heavy for every session to run at once, which then take turns": {"check, dev, lint", ""},
		"the CI jobs every pull request must pass, such as test or build":                            {"check,", "check, test, build, security"},
	}}
	made, err := newAt(t, s, map[string][]string{"--preset": {"team"}, "--tracker-kind": {"linear2"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"this needs an answer",
		`"foo" is not owner/name, such as you/shop`,
		"there is no repository x/nope on GitHub, or gh's account cannot see it",
		`"Dev" is not a role name, use lowercase letters, digits and hyphens`,
		`"boss" is none of the roles lead and dev`,
		`"check," has an empty item, separate the items with single commas`,
		"the package.json of x/demo has no script lint",
		`"linear2" is not one of linear, github or none, pick one below`,
	}
	if strings.Join(s.problems, "\n") != strings.Join(want, "\n") {
		t.Fatal(strings.Join(s.problems, "\n"))
	}
	for _, line := range []string{`scripts = ["check", "test", "build"]`, `kind = "github"`, `lead = "lead"`, "[[roles]]\nname = \"dev\""} {
		if !strings.Contains(made.TOML, line) {
			t.Fatal(line, "\n", made.TOML)
		}
	}
	if !contains(made.Modules, "heavy") || !contains(made.Modules, "skills") {
		t.Fatal(made.Modules)
	}
	s = &script{answers: map[string][]string{"the GitHub repository, as owner/name": {"x/demo"}}}
	if made, err := newAt(t, s, map[string][]string{"--preset": {"bare"}, "--repo": {"you/x y"}}); err != nil || made.Repo != "x/demo" || strings.Join(s.inputs, " ") != "you/x y" || s.problems[0] != `"you/x y" is not owner/name, such as you/shop` {
		t.Fatal(err, s.inputs, s.problems)
	}
}

func TestWithGhMissingOrLoggedOutMingoNewSaysSoAndStops(t *testing.T) {
	for want, r := range map[string]team.Result{
		"gh, the GitHub CLI, is not installed, and mingo new checks the repository with it, see https://cli.github.com": {NotFound: true},
		"gh is not logged in, run gh auth login": {Code: 4, Stderr: "To get started with GitHub CLI, please run:  gh auth login\n"},
	} {
		run := func(team.Options, string, ...string) (team.Result, error) { return r, nil }
		flags := map[string][]string{"--preset": {"bare"}, "--repo": {"x/demo"}}
		if _, err := newKit(t).New("demo", flags, NewDeps{Run: run}); err == nil || err.Error() != want {
			t.Fatal(want, "\n", err)
		}
		s := &script{t: t, answers: map[string][]string{"the GitHub repository, as owner/name": {"x/demo"}}}
		if _, err := newKit(t).New("demo", map[string][]string{"--preset": {"bare"}}, NewDeps{Ask: s, Run: run}); err == nil || err.Error() != want || len(s.problems) > 0 {
			t.Fatal(want, "\n", err, s.problems)
		}
	}
}

func TestHeavyIsAskedOnlyForNodeAndLeftOutWithoutAPackageJSON(t *testing.T) {
	s := &script{answers: map[string][]string{"how CI installs the project": {"go"}, "the command of the check check": {"a"}, "the command of the check test": {"b"}, "the command of the check build": {"c"}}}
	made, err := newAt(t, s, map[string][]string{"--preset": {"team"}, "--repo": {"x/demo"}})
	if err != nil || contains(made.Modules, "heavy") || strings.Contains(made.TOML, "heavy") || strings.Contains(strings.Join(s.asked, "\n"), "package.json") {
		t.Fatal(err, made.TOML, s.asked)
	}
	var out bytes.Buffer
	made, err = newKit(t).New("demo", map[string][]string{"--preset": {"team"}, "--repo": {"x/bare"}}, NewDeps{Run: fakeGH(someRepos), Out: &out})
	if err != nil || contains(made.Modules, "heavy") || strings.Contains(made.TOML, "heavy") || out.String() != "x/bare has no package.json on its default branch, so heavy is left out\n" {
		t.Fatal(err, out.String(), made.TOML)
	}
	if _, err := newFrom(t, map[string][]string{"--preset": {"team"}, "--repo": {"x/bare"}, "--heavy-scripts": {"check"}}); err == nil || err.Error() != "--heavy-scripts names scripts, but x/bare has no package.json on its default branch" {
		t.Fatal(err)
	}
	made, err = newFrom(t, map[string][]string{"--preset": {"team"}, "--repo": {"x/demo"}, "--ci-stack": {"npm"}})
	if err != nil || !contains(made.Modules, "heavy") || !strings.Contains(made.TOML, `scripts = ["check", "test", "build"]`) {
		t.Fatal(err, made.TOML)
	}
	lean := map[string]string{"x/demo": `{"scripts": {"check": "tsc", "test": "vitest"}}`}
	if _, err := newKit(t).New("demo", map[string][]string{"--preset": {"team"}, "--repo": {"x/demo"}}, NewDeps{Run: fakeGH(lean)}); err == nil || err.Error() != "the package.json of x/demo has no script build, so give --heavy-scripts" {
		t.Fatal(err)
	}
}

func TestNoTrackerLeavesTheTrackerModuleOut(t *testing.T) {
	made, err := newFrom(t, map[string][]string{"--preset": {"team"}, "--repo": {"x/demo"}, "--tracker-kind": {"none"}})
	if err != nil || contains(made.Modules, "tracker") || strings.Contains(made.TOML, "[tracker]") {
		t.Fatal(err, made.TOML)
	}
	if p := project(t, made.TOML); p.Tracker.Kind != "none" {
		t.Fatal(p.Tracker)
	}
}

func TestARoleNamedLikeOneOfTheKitsSkillsCannotLead(t *testing.T) {
	for want, flags := range map[string]map[string][]string{
		"task-flow cannot lead, the lead's skill would take .claude/skills/task-flow, the folder of the kit's own task-flow skill, pick another role, so give --lead":  {"--roles": {"task-flow, dev"}},
		"--lead: spend cannot lead, the lead's skill would take .claude/skills/spend, the folder of the kit's own spend skill, pick another role":                      {"--roles": {"dev, spend"}, "--lead": {"spend"}},
		"--roles: the role task-flow cannot lead, the lead's skill would take .claude/skills/task-flow, the folder of the kit's own task-flow skill, rename the role":  {"--roles": {"task-flow"}},
		"--roles: none of the roles spend and task-flow can lead, the lead's skill would take the folder of one of the kit's own skills in .claude/skills, rename one": {"--roles": {"spend, task-flow"}},
	} {
		flags["--preset"], flags["--repo"] = []string{"team"}, []string{"x/demo"}
		if _, err := newFrom(t, flags); err == nil || err.Error() != want {
			t.Fatal(want, "\n", err)
		}
	}
	made, err := newFrom(t, map[string][]string{"--preset": {"team"}, "--repo": {"x/demo"}, "--roles": {"task-flow, dev"}, "--lead": {"dev"}})
	if err != nil || project(t, made.TOML).Lead != "dev" {
		t.Fatal(err, made.TOML)
	}
	s := &script{answers: map[string][]string{"the GitHub repository, as owner/name": {"x/demo"}, "the roles, in the order they start": {"task-flow, dev"}, "the role that merges": {"", "dev"}}}
	if made, err := newAt(t, s, map[string][]string{"--preset": {"bare"}}); err != nil || !strings.Contains(made.TOML, `lead = "dev"`) || len(s.problems) != 1 || !strings.HasPrefix(s.problems[0], "task-flow cannot lead") {
		t.Fatal(err, s.problems, made.TOML)
	}
}

func TestAFlagMingoNewCannotReadIsAMistakeOfTheCommandLineAndABadAnswerIsNot(t *testing.T) {
	for _, flags := range []map[string][]string{
		{"--preset": {"bare"}, "--colour": {"red"}},
		{"--preset": {"bare", "solo"}},
		{"--preset": {"bare"}, "--repo": {" "}},
		{"--preset": {"solo"}, "--ci-run": {"test"}},
	} {
		var flagErr FlagError
		if _, err := newFrom(t, flags); !errors.As(err, &flagErr) {
			t.Fatal(flags, err)
		}
	}
	var flagErr FlagError
	if _, err := newFrom(t, map[string][]string{"--preset": {"bare"}, "--repo": {"foo"}}); err == nil || errors.As(err, &flagErr) {
		t.Fatal(err)
	}
}

func TestARoleWhoseSessionAnotherProjectHasIsRefused(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MINGO_CONFIG", root)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "demo-web.toml"), []byte("[[roles]]\nname = \"lead\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := "--roles: the role web-lead makes the tmux session demo-web-lead, which the project demo-web has for its role lead, rename the role"
	if _, err := newFrom(t, map[string][]string{"--preset": {"bare"}, "--repo": {"x/demo"}, "--roles": {"lead, web-lead"}}); err == nil || err.Error() != want {
		t.Fatal(err)
	}
	if _, err := newFrom(t, map[string][]string{"--preset": {"bare"}, "--repo": {"x/demo"}, "--roles": {"lead, web"}}); err != nil {
		t.Fatal(err)
	}
}

func TestTheTeamPresetHasTheSkillsTEAMmdPointsAt(t *testing.T) {
	mods, err := newKit(t).Preset("team")
	if err != nil || !contains(mods, "skills") || !contains(mods, "team") {
		t.Fatal(mods, err)
	}
	if len(PresetHints) != len(Presets) {
		t.Fatal(PresetHints)
	}
}

func TestEveryChoiceHasAHintAndItsDefaultIsOneOfThem(t *testing.T) {
	k := newKit(t)
	seen := 0
	for _, m := range k.Modules {
		for _, a := range m.Asks {
			if len(a.Choices) == 0 {
				continue
			}
			seen++
			for _, c := range a.Choices {
				if c.Value == "" || c.Hint == "" {
					t.Fatal(a.Key, c)
				}
			}
			if a.Default == nil || !contains(a.Values(), *a.Default) {
				t.Fatal(a.Key, a.Default)
			}
		}
	}
	if seen != 3 {
		t.Fatal(seen)
	}
}

func TestMadeSaysWhatComesNextBeforeItAsksToGoOn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "dev", "demo", "lead"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := Made{Name: "demo", Repo: "x/demo", Roles: []string{"lead", "dev", "qa"}, Local: true}
	want := "\nmingo new will\n" +
		"  write the config /c/demo.toml\n" +
		"  clone x/demo into ~/dev/demo/dev and ~/dev/demo/qa\n" +
		"  use the clone already in ~/dev/demo/lead\n" +
		"  open a pull request on x/demo with the kit's files\n" +
		"  write Claude's files into each clone, out of git\n"
	if got := m.Next("/c/demo.toml"); got != want {
		t.Fatal(got)
	}
}
