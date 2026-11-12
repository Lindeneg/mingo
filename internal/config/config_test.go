package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("MINGO_CONFIG", filepath.Join(root, "config"))
	t.Setenv("MINGO_STATE", filepath.Join(root, "state"))
	for _, v := range []string{"MINGO_PROJECT", "MINGO_ROLE", "CLAUDECODE"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	if err := os.MkdirAll(filepath.Join(root, "config", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

func addProject(t *testing.T, name string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(File(name), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func addDemo(t *testing.T, home string) {
	t.Helper()
	addProject(t, "demo",
		`repo = "Lindeneg/demo"`,
		`opener = "You are the {role} of {project}."`,
		`[[roles]]`, `name = "lead"`, `aliases = ["l"]`, `model = "claude-opus-5-5"`, `effort = "high"`,
		`[[roles]]`, `name = "helper"`, `aliases = ["h"]`)
	for _, r := range []string{"lead", "helper"} {
		if err := os.MkdirAll(filepath.Join(home, "dev", "demo", r), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func mustLoad(t *testing.T, name string) *Project {
	t.Helper()
	p, err := Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsForEverythingButRoles(t *testing.T) {
	home := setupHome(t)
	addProject(t, "solo", `[[roles]]`, `name = "main"`)
	p := mustLoad(t, "solo")
	if p.Lead != "main" || len(p.Autostart) != 0 {
		t.Fatalf("lead %q autostart %v", p.Lead, p.Autostart)
	}
	if got := p.CloneDir("main"); got != home+"/dev/solo/main" {
		t.Fatal(got)
	}
	if got := p.OpenerFor("main"); got != "Read your handoff memory, then continue." {
		t.Fatal(got)
	}
	if got := strings.Join(p.Args(), " "); got != "--test default" {
		t.Fatal(got)
	}
	if p.Model("main") != "" || p.Effort("main") != "" {
		t.Fatal("model or effort set")
	}
}

func TestProjectNamesClonesOpenerModelsEfforts(t *testing.T) {
	home := setupHome(t)
	addProject(t, "acme",
		`clone = "~/dev/acme/{role}"`,
		`opener = "You are the {role}. Read your handoff memory, then continue."`,
		`[[roles]]`, `name = "tech-lead"`, `model = "claude-opus-5-5"`, `effort = "xhigh"`,
		`[[roles]]`, `name = "senior"`)
	p := mustLoad(t, "acme")
	if got := p.CloneDir("tech-lead"); got != home+"/dev/acme/tech-lead" {
		t.Fatal(got)
	}
	if got := p.OpenerFor("tech-lead"); got != "You are the tech lead. Read your handoff memory, then continue." {
		t.Fatal(got)
	}
	if p.Model("tech-lead") != "claude-opus-5-5" || p.Effort("tech-lead") != "xhigh" {
		t.Fatal(p.Model("tech-lead"), p.Effort("tech-lead"))
	}
	if got := p.SessionName("senior"); got != "acme-senior" {
		t.Fatal(got)
	}
	want := home + "/memory/tech-lead"
	if got := p.MemoryDir("tech-lead"); got != want {
		t.Fatalf("%s, want %s", got, want)
	}
}

func TestTheRepoHasTheProjectsNameFilledIn(t *testing.T) {
	setupHome(t)
	addProject(t, "solo", `repo = "someone/{project}"`, `[[roles]]`, `name = "main"`)
	if p := mustLoad(t, "solo"); p.Repo != "someone/solo" {
		t.Fatal(p.Repo)
	}
}

func TestSingleFolderProjectKeepsOneClone(t *testing.T) {
	home := setupHome(t)
	addProject(t, "johndoe", `clone = "~/dev/johndoe"`, `[[roles]]`, `name = "lead"`)
	if got := mustLoad(t, "johndoe").CloneDir("lead"); got != home+"/dev/johndoe" {
		t.Fatal(got)
	}
}

func TestEnvironmentOverridesModelAndEffort(t *testing.T) {
	setupHome(t)
	addProject(t, "acme", `[[roles]]`, `name = "tech-lead"`, `model = "claude-opus-5-5"`)
	p := mustLoad(t, "acme")
	t.Setenv("MINGO_MODEL_ACME_TECH_LEAD", "claude-sonnet-5-5")
	t.Setenv("MINGO_EFFORT_ACME_TECH_LEAD", "low")
	if p.Model("tech-lead") != "claude-sonnet-5-5" || p.Effort("tech-lead") != "low" {
		t.Fatal(p.Model("tech-lead"), p.Effort("tech-lead"))
	}
	t.Setenv("MINGO_MODEL_ACME_TECH_LEAD", "")
	if p.Model("tech-lead") != "claude-opus-5-5" {
		t.Fatal(p.Model("tech-lead"))
	}
}

func TestOneProjectsSettingsDoNotLeak(t *testing.T) {
	setupHome(t)
	addProject(t, "one", `claude_args = ["--x"]`, `[[roles]]`, `name = "a"`, `aliases = ["x"]`, `model = "m1"`)
	addProject(t, "two", `[[roles]]`, `name = "b"`)
	if one := mustLoad(t, "one"); strings.Join(one.Args(), " ") != "--x" {
		t.Fatal(one.Args())
	}
	two := mustLoad(t, "two")
	if _, err := two.Resolve("x"); err == nil {
		t.Fatal("alias x leaked")
	}
	if two.Model("a") != "" || strings.Join(two.Args(), " ") != "--test default" {
		t.Fatal("model or claude_args leaked")
	}
}

func TestRolesResolveByNameAndAlias(t *testing.T) {
	home := setupHome(t)
	addDemo(t, home)
	p := mustLoad(t, "demo")
	if r, _ := p.Resolve("lead"); r != "lead" {
		t.Fatal(r)
	}
	if r, _ := p.Resolve("h"); r != "helper" {
		t.Fatal(r)
	}
	_, err := p.Resolve("nobody")
	if err == nil || !strings.Contains(err.Error(), "demo has no role or alias nobody") {
		t.Fatal(err)
	}
}

func TestTargetForms(t *testing.T) {
	home := setupHome(t)
	addDemo(t, home)
	addProject(t, "other", `[[roles]]`, `name = "solo"`)
	p, role, err := ParseTarget("demo/h")
	if err != nil || p.Name != "demo" || role != "helper" {
		t.Fatal(p, role, err)
	}
	p, role, err = ParseTarget("other")
	if err != nil || p.Name != "other" || role != "" {
		t.Fatal(p, role, err)
	}
	if _, role, _ = ParseRoleTarget("other"); role != "solo" {
		t.Fatal(role)
	}
	t.Setenv("MINGO_PROJECT", "demo")
	p, role, err = ParseTarget("l")
	if err != nil || p.Name != "demo" || role != "lead" {
		t.Fatal(p, role, err)
	}
}

func TestDefaultProjectOrder(t *testing.T) {
	home := setupHome(t)
	addDemo(t, home)
	if got, _ := DefaultProject(); got != "demo" {
		t.Fatal(got)
	}
	addProject(t, "other", `[[roles]]`, `name = "solo"`)
	if _, err := DefaultProject(); err == nil {
		t.Fatal("two projects and no default should fail")
	}
	if err := os.WriteFile(DefaultFile(), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := DefaultProject(); got != "other" {
		t.Fatal(got)
	}
	t.Chdir(filepath.Join(home, "dev", "demo", "helper"))
	if got, _ := DefaultProject(); got != "demo" {
		t.Fatal(got)
	}
	t.Setenv("MINGO_PROJECT", "other")
	if got, _ := DefaultProject(); got != "other" {
		t.Fatal(got)
	}
}

func TestNamesMustBeLowercaseWords(t *testing.T) {
	setupHome(t)
	addProject(t, "bad", `[[roles]]`, `name = "Bad_Role"`)
	if _, err := Load("bad"); err == nil {
		t.Fatal("bad role loaded")
	}
	if _, err := Load("no/such"); err == nil {
		t.Fatal("bad project name loaded")
	}
}

func TestProjectWithoutRolesIsRefused(t *testing.T) {
	setupHome(t)
	addProject(t, "empty", `repo = "you/empty"`)
	_, err := Load("empty")
	if err == nil || !strings.Contains(err.Error(), "names no roles") {
		t.Fatal(err)
	}
}

func TestProjectIDWorksWhereANameDoes(t *testing.T) {
	home := setupHome(t)
	addDemo(t, home)
	addProject(t, "rather-long-project", `id = "rl"`, `[[roles]]`, `name = "lead"`, `[[roles]]`, `name = "dev1"`)
	p, role, err := ParseTarget("rl/dev1")
	if err != nil || p.Name != "rather-long-project" || role != "dev1" {
		t.Fatal(p, role, err)
	}
	p, role, err = ParseTarget("rl")
	if err != nil || p.Name != "rather-long-project" || role != "" {
		t.Fatal(p, role, err)
	}
	if p, err := Find("demo"); err != nil || p.Name != "demo" {
		t.Fatal(p, err)
	}
}

func TestProjectIDMustBeUniqueAndNameShaped(t *testing.T) {
	setupHome(t)
	addProject(t, "one", `id = "x"`, `[[roles]]`, `name = "a"`)
	addProject(t, "two", `id = "x"`, `[[roles]]`, `name = "b"`)
	if _, err := Load("one"); err == nil || !strings.Contains(err.Error(), "which project two has too") {
		t.Fatal(err)
	}
	addProject(t, "two", `id = "one"`, `[[roles]]`, `name = "b"`)
	if _, err := Load("two"); err == nil || !strings.Contains(err.Error(), "the name of another project") {
		t.Fatal(err)
	}
	addProject(t, "two", `id = "No_Good"`, `[[roles]]`, `name = "b"`)
	if _, err := Load("two"); err == nil || !strings.Contains(err.Error(), "use lowercase letters") {
		t.Fatal(err)
	}
}

func TestWorkflowDefaultsForAProjectThatSetsNone(t *testing.T) {
	setupHome(t)
	addProject(t, "bare", `[[roles]]`, `name = "lead"`)
	w := mustLoad(t, "bare").Workflow
	if w.Base != "master" || w.CI != "ci" || len(w.Checks) != 0 || len(w.Gate) != 0 || len(w.Tags) != 0 {
		t.Fatal(w)
	}
	if w.ReviewLine != "review:" || w.NoFindings != "no findings" || w.ReviewHeading != "## Review" {
		t.Fatal(w)
	}
	if strings.Join(w.LocalRuns, ",") != "mingo gate" || strings.Join(w.Trailers, ",") != "Co-Authored-By" {
		t.Fatal(w.LocalRuns, w.Trailers)
	}
	if !w.IsFooter("Made with the test CLI") || w.IsFooter("made with the test CLI, not a footer") {
		t.Fatal("footers")
	}
	if p := mustLoad(t, "bare"); p.Tracker.Kind != "none" {
		t.Fatal(p.Tracker)
	}
}

func TestWorkflowLocalRunsDefaultToTheGateAndMingoGate(t *testing.T) {
	setupHome(t)
	addProject(t, "go", `[workflow]`, `gate = ["go vet ./...", "go test ./..."]`, `[[roles]]`, `name = "lead"`)
	if got := strings.Join(mustLoad(t, "go").Workflow.LocalRuns, ","); got != "go vet ./...,go test ./...,mingo gate" {
		t.Fatal(got)
	}
}

func TestWorkflowSharedFilesMatchTheirPatterns(t *testing.T) {
	setupHome(t)
	addProject(t, "lists", `[[workflow.shared]]`, `name = "the list"`, `pattern = '^docs/list\.md$'`, `merge = "lines"`,
		`[[workflow.shared]]`, `name = "a migration"`, `pattern = '^db/migrations/'`, `[[roles]]`, `name = "lead"`)
	w := mustLoad(t, "lists").Workflow
	if s, ok := w.SharedFile("docs/list.md"); !ok || s.Name != "the list" || s.Merge != "lines" {
		t.Fatal(s, ok)
	}
	if s, ok := w.SharedFile("db/migrations/1.sql"); !ok || s.Merge != "none" {
		t.Fatal(s, ok)
	}
	if _, ok := w.SharedFile("x/docs/list.md"); ok {
		t.Fatal("matched")
	}
}

func TestWorkflowAndTrackerMistakesAreRefused(t *testing.T) {
	setupHome(t)
	for want, lines := range map[string][]string{
		"merged by both, use lines, json-keys or none": {`[[workflow.shared]]`, `name = "x"`, `pattern = 'x'`, `merge = "both"`},
		"whose pattern is no regexp":                   {`[[workflow.shared]]`, `name = "x"`, `pattern = '('`},
		"a footer pattern that is no regexp":           {`[workflow]`, `footers = ['(']`},
		"a generated pattern without a group":          {`[workflow]`, `generated = 'wrote'`},
		"the tracker jira, use linear, github or none": {`[tracker]`, `kind = "jira"`},
		"a linear tracker without its team key":        {`[tracker]`, `kind = "linear"`},
		"an empty command in [workflow] gate":          {`[workflow]`, `gate = ["go test ./...", " "]`},
		"name both or neither":                         {`[workflow]`, `generate_list = ["x --list"]`},
		"names generate without generated":             {`[workflow]`, `generate_list = ["x --list"]`, `generate = ["x"]`},
	} {
		addProject(t, "wrong", append(lines, `[[roles]]`, `name = "lead"`)...)
		if _, err := Load("wrong"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, err)
		}
	}
}

func TestClaudeFilesAreInTheRepoUnlessLocalAndALocalProjectsReviewReadsTheReadme(t *testing.T) {
	setupHome(t)
	addProject(t, "kept", `[[roles]]`, `name = "lead"`)
	if p := mustLoad(t, "kept"); p.ClaudeFiles != "repo" || p.ClaudeLocal() || p.Review.Read != "AGENTS.md first" {
		t.Fatal(p.ClaudeFiles, p.Review.Read)
	}
	addProject(t, "own", `claude_files = "local"`, `[[roles]]`, `name = "lead"`)
	if p := mustLoad(t, "own"); !p.ClaudeLocal() || p.Review.Read != "README.md first" {
		t.Fatal(p.ClaudeFiles, p.Review.Read)
	}
	addProject(t, "set", `claude_files = "local"`, `[review]`, `read = "docs/ first"`, `[[roles]]`, `name = "lead"`)
	if p := mustLoad(t, "set"); p.Review.Read != "docs/ first" {
		t.Fatal(p.Review.Read)
	}
	addProject(t, "wrong", `claude_files = "home"`, `[[roles]]`, `name = "lead"`)
	if _, err := Load("wrong"); err == nil || !strings.Contains(err.Error(), "has claude_files home, use repo or local") {
		t.Fatal(err)
	}
}

func TestTheStackOtherNeedsEachChecksCommandAndOnlyItTakesSetupAndAudit(t *testing.T) {
	setupHome(t)
	addProject(t, "own", `[workflow]`, `checks = ["test", "security"]`, `[ci]`, `stack = "other"`, `setup = ["make deps"]`, `audit = "make audit"`, `[ci.run]`, `test = "make test"`, `[[roles]]`, `name = "lead"`)
	if p := mustLoad(t, "own"); p.CI.Command("test") != "make test" || strings.Join(p.CI.Setup, ",") != "make deps" || p.CI.Audit != "make audit" {
		t.Fatal(p.CI)
	}
	for want, lines := range map[string][]string{
		"has the check test with no command, name it in [ci.run]": {`[workflow]`, `checks = ["test"]`, `[ci]`, `stack = "other"`},
		"has the stack rust, use pnpm, npm, go or other":          {`[ci]`, `stack = "rust"`},
		"has [ci] setup, which is for the stack other":            {`[ci]`, `stack = "npm"`, `setup = ["make deps"]`},
		"has [ci] audit, which is for the stack other":            {`[ci]`, `stack = "pnpm"`, `audit = "make audit"`},
	} {
		addProject(t, "wrong", append(lines, `[[roles]]`, `name = "lead"`)...)
		if _, err := Load("wrong"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, err)
		}
	}
}

func TestTrackerGivesTheClosesLineAndTheTitlesIssueID(t *testing.T) {
	setupHome(t)
	addProject(t, "lin", `[tracker]`, `kind = "linear"`, `team = "ACM"`, `[[roles]]`, `name = "lead"`)
	closes, title := mustLoad(t, "lin").IssuePatterns()
	if !closes.MatchString("Closes ACM-16") || closes.MatchString("Closes ACM-16 and ACM-17") || !title.MatchString("ACM-7: ci") {
		t.Fatal(closes, title)
	}
	addProject(t, "gh", `[tracker]`, `kind = "github"`, `[[roles]]`, `name = "lead"`)
	closes, title = mustLoad(t, "gh").IssuePatterns()
	if !closes.MatchString("Closes #12") || title != nil || mustLoad(t, "gh").ClosesExample() != "Closes #n" {
		t.Fatal(closes, title)
	}
}

func TestRoleReferencesAndClonesAreCheckedAtLoad(t *testing.T) {
	setupHome(t)
	for want, lines := range map[string][]string{
		"has the role dev twice":                              {`[[roles]]`, `name = "dev"`, `[[roles]]`, `name = "dev"`},
		"has the alias d, which is a role's name":             {`[[roles]]`, `name = "dev"`, `[[roles]]`, `name = "d"`, `aliases = ["d"]`},
		"has the alias x twice":                               {`[[roles]]`, `name = "a"`, `aliases = ["x"]`, `[[roles]]`, `name = "b"`, `aliases = ["x"]`},
		"has the alias \"X Y\", use lowercase":                {`[[roles]]`, `name = "a"`, `aliases = ["X Y"]`},
		"has the lead leed, which is none of its roles":       {`lead = "leed"`, `[[roles]]`, `name = "lead"`},
		"autostarts \"nobody\", which is none of its roles":   {`autostart = ["nobody"]`, `[[roles]]`, `name = "lead"`},
		"has 2 roles and one clone for them all":              {`clone = "~/dev/{project}"`, `[[roles]]`, `name = "a"`, `[[roles]]`, `name = "b"`},
		"has the clone \"dev/{role}\", start it with ~/ or /": {`clone = "dev/{role}"`, `[[roles]]`, `name = "lead"`},
	} {
		addProject(t, "wrong", lines...)
		if _, err := Load("wrong"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, err)
		}
	}
	addProject(t, "one", `clone = "~/dev/one"`, `[[roles]]`, `name = "main"`)
	mustLoad(t, "one")
}

func TestACloneEndingInASlashIsTheSameFolder(t *testing.T) {
	home := setupHome(t)
	addProject(t, "demo", `clone = "~/dev/{project}/{role}/"`, `[[roles]]`, `name = "lead"`)
	p := mustLoad(t, "demo")
	lead := filepath.Join(home, "dev", "demo", "lead")
	if p.CloneDir("lead") != lead {
		t.Fatal(p.CloneDir("lead"))
	}
	if role, ok := p.RoleOfDir(lead); !ok || role != "lead" {
		t.Fatal(role, ok)
	}
	if role, ok := p.RoleOfDir(filepath.Join(lead, "src")); !ok || role != "lead" {
		t.Fatal(role, ok)
	}
}

func TestHoldsDirSeesAClonePastALink(t *testing.T) {
	home := setupHome(t)
	real := filepath.Join(filepath.Dir(home), "disk", "demo", "lead")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(home), "disk", "demo"), filepath.Join(home, "dev", "demo")); err != nil {
		t.Fatal(err)
	}
	addProject(t, "demo", `[[roles]]`, `name = "lead"`)
	p := mustLoad(t, "demo")
	if !p.HoldsDir(real) || !p.HoldsDir(filepath.Join(home, "dev", "demo", "lead")) || p.HoldsDir(filepath.Join(filepath.Dir(home), "disk")) {
		t.Fatal("holds")
	}
}
