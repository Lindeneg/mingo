package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Lindeneg/mingo/internal/config"
)

func project(t *testing.T, text string) *config.Project {
	t.Helper()
	return projectNamed(t, "demo", text)
}

func projectNamed(t *testing.T, name, text string) *config.Project {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MINGO_CONFIG", root)
	os.MkdirAll(filepath.Join(root, "projects"), 0o755)
	if err := os.WriteFile(filepath.Join(root, "projects", name+".toml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := config.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var testKit = fstest.MapFS{
	"base/module.toml":   {Data: []byte("does = \"always\"\nalways = true\n")},
	"skills/module.toml": {Data: []byte("does = \"skills\"\n")},
	"skills/files/.claude/skills/{{.Lead}}/SKILL.md": {Data: []byte("# {{.Lead}}\n\n{{block \"steps\" .}}the generic steps\n{{end}}done\n")},
	"skills/files/only-with-tracker.md":              {Data: []byte("{{if ne .Tracker.Kind \"none\"}}tracked by {{.Tracker.Kind}}\n{{end}}")},
	"other/module.toml":                              {Data: []byte("does = \"other\"\n")},
}

const demo = "[[roles]]\nname = \"chief\"\n[[roles]]\nname = \"helper\"\n"

func TestRenderFillsPathsAndTextsFromTheProjectAndLeavesOutWhatRendersEmpty(t *testing.T) {
	k, err := Load(testKit)
	if err != nil {
		t.Fatal(err)
	}
	p := project(t, "modules = [\"skills\"]\n"+demo)
	mods, err := k.For(p)
	if err != nil || len(mods) != 2 || mods[0].Name != "base" || mods[1].Name != "skills" {
		t.Fatal(mods, err)
	}
	files, err := k.Render(p, mods)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != ".claude/skills/chief/SKILL.md" || files[0].Text != "# chief\n\nthe generic steps\ndone\n" || files[0].Module != "skills" {
		t.Fatal(files)
	}
}

func TestAProjectsBlockReplacesTheGenericTextAndMayUseItsValues(t *testing.T) {
	k, _ := Load(testKit)
	p := project(t, "modules = [\"skills\"]\n[blocks.\".claude/skills/chief/SKILL.md\"]\nsteps = '''\nour own steps for {{.Name}}\n'''\n"+demo)
	mods, _ := k.For(p)
	files, err := k.Render(p, mods)
	if err != nil || files[0].Text != "# chief\n\nour own steps for demo\ndone\n" {
		t.Fatal(files, err)
	}
	empty := project(t, "modules = [\"skills\"]\n[blocks.\".claude/skills/chief/SKILL.md\"]\nsteps = \"\"\n"+demo)
	files, err = k.Render(empty, mods)
	if err != nil || files[0].Text != "# chief\n\ndone\n" {
		t.Fatal(files, err)
	}
}

func TestRenderRefusesABlockTheFileDoesNotHaveAndBlocksForAFileNotRendered(t *testing.T) {
	k, _ := Load(testKit)
	for want, text := range map[string]string{
		"which has no such block":           "[blocks.\".claude/skills/chief/SKILL.md\"]\nstpes = \"x\"\n",
		"which none of its modules renders": "[blocks.\"README.md\"]\nsteps = \"x\"\n",
	} {
		p := project(t, "modules = [\"skills\"]\n"+text+demo)
		mods, _ := k.For(p)
		if _, err := k.Render(p, mods); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, err)
		}
	}
}

// a load checks a config against the embedded kit, so a config a test kit's own refusal is shown on
// loads without that check
func unchecked(t *testing.T) {
	t.Helper()
	before := config.RegisterKit(nil)
	t.Cleanup(func() { config.RegisterKit(before) })
}

func TestTheKitGivesALoadItsModulesAndTheFoldersOfItsSkills(t *testing.T) {
	names := config.RegisterKit(nil)
	config.RegisterKit(names)
	if names == nil {
		t.Fatal("the kit gave the config nothing")
	}
	k, err := names()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(k.Modules, " ") != "ci guards heavy ports review rules skills team tracker workflow" || strings.Join(k.Always, " ") != "sessions" {
		t.Fatal(k.Modules, k.Always)
	}
	if strings.Join(k.Skills, " ") != "clear-context edge-cases plan-milestone spend sub-agents task-flow" {
		t.Fatal(k.Skills)
	}
	root := t.TempDir()
	t.Setenv("MINGO_CONFIG", root)
	os.MkdirAll(filepath.Join(root, "projects"), 0o755)
	for want, text := range map[string]string{
		`names the module "workflw", which mingo does not have, it has ci guards heavy ports review rules skills team tracker workflow`: "modules = [\"workflw\"]\n" + demo,
		"has the lead task-flow, whose skill would take .claude/skills/task-flow":                                                       "[[roles]]\nname = \"task-flow\"\n[[roles]]\nname = \"dev\"\n",
	} {
		os.WriteFile(filepath.Join(root, "projects", "demo.toml"), []byte(text), 0o644)
		if _, err := config.Load("demo"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, "\n", err)
		}
	}
}

func TestForRefusesAModuleMingoDoesNotHaveOrOneAlwaysOn(t *testing.T) {
	unchecked(t)
	k, _ := Load(testKit)
	for want, modules := range map[string]string{
		"names the module nope, mingo has base other skills": `["nope"]`,
		"which is always on": `["base"]`,
	} {
		if _, err := k.For(project(t, "modules = "+modules+"\n"+demo)); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatal(want, err)
		}
	}
}

func TestTheEmbeddedKitLoadsWithSessionsAlwaysOn(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := k.Module("sessions")
	if !ok || !m.Always {
		t.Fatal(k.Names())
	}
	if _, ok := k.Module("guards"); !ok {
		t.Fatal(k.Names())
	}
}

func TestTheStackOtherRunsItsOwnSetupAndAuditAndHasNoSetupActionWithoutSetup(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	render := func(ci string) map[string]string {
		t.Helper()
		p := project(t, "repo = \"x/demo\"\nmodules = [\"ci\"]\n[workflow]\nchecks = [\"test\", \"security\"]\n[ci]\nstack = \"other\"\n"+ci+"[ci.run]\ntest = \"make test\"\n[[roles]]\nname = \"lead\"\n")
		mods, err := k.For(p)
		if err != nil {
			t.Fatal(err)
		}
		files, err := k.Render(p, mods)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, f := range files {
			out[f.Path] = f.Text
		}
		return out
	}
	own := render("setup = [\"make deps\", \"make tools\"]\naudit = \"make audit\"\n")
	if action := own[".github/actions/setup/action.yml"]; !strings.Contains(action, "    - run: make deps\n      shell: bash\n    - run: make tools\n      shell: bash\n") {
		t.Fatal(action)
	}
	ci := own[".github/workflows/ci.yml"]
	if strings.Count(ci, "- uses: ./.github/actions/setup\n") != 2 || !strings.Contains(ci, "      - name: Dependency audit\n        run: make audit\n") || !strings.Contains(ci, "      - run: make test\n") {
		t.Fatal(ci)
	}
	none := render("")
	if _, ok := none[".github/actions/setup/action.yml"]; ok {
		t.Fatal("a setup action without setup commands")
	}
	if ci := none[".github/workflows/ci.yml"]; strings.Contains(ci, "actions/setup") || strings.Contains(ci, "Dependency audit") || !strings.Contains(ci, "Secret scan") {
		t.Fatal(ci)
	}
}

func TestAYAMLValueStaysPlainWhereThatReadsTheSameAndIsJSONQuotedElsewhere(t *testing.T) {
	for in, want := range map[string]string{
		"pnpm check":              "pnpm check",
		"go test ./...":           "go test ./...",
		"ci":                      "ci",
		"ci-${{ github.ref }}":    "ci-${{ github.ref }}",
		"go test -run 'Foo: bar'": `"go test -run 'Foo: bar'"`,
		"golangci-lint run # all": `"golangci-lint run # all"`,
		"CI: main":                `"CI: main"`,
		"*.sh":                    `"*.sh"`,
		"- run":                   `"- run"`,
		"'quoted'":                `"'quoted'"`,
		"make\ntest":              `"make\ntest"`,
		"true":                    `"true"`,
		"No":                      `"No"`,
		"1.5":                     `"1.5"`,
		"":                        `""`,
		" lead":                   `" lead"`,
		"ends:":                   `"ends:"`,
		`say "hi" > out & <done>`: `say "hi" > out & <done>`,
		"{a}":                     `"{a}"`,
		"tab\there":               `"tab\there"`,
	} {
		if got := yamlScalar(in); got != want {
			t.Errorf("%q gives %s, want %s", in, got, want)
		}
	}
	if got := indent(4, "one\ntwo\r\nthree"); got != "one\n    two\n    three" {
		t.Fatal(got)
	}
}

func TestConfigValuesReachTheWorkflowsAsTheStringsTheyAre(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	p := project(t, `repo = "x/demo"
title = "Demo\nShop"
modules = ["ci", "review"]
[workflow]
ci = "CI: main"
checks = ["test", "lint"]
[ci]
stack = "other"
setup = ["*/setup.sh"]
[ci.run]
test = "go test -run 'Foo: bar'"
lint = "golangci-lint run # all"
[review]
read = "CLAUDE.md first\nthen docs/"
[[roles]]
name = "lead"
`)
	mods, _ := k.For(p)
	files, err := k.Render(p, mods)
	if err != nil {
		t.Fatal(err)
	}
	text := map[string]string{}
	for _, f := range files {
		text[f.Path] = f.Text
	}
	for path, lines := range map[string][]string{
		".github/workflows/ci.yml":         {`name: "CI: main"`, `  group: "CI: main-${{ github.event.pull_request.number || github.ref }}"`, `      - run: "go test -run 'Foo: bar'"`, `      - run: "golangci-lint run # all"`},
		".github/actions/setup/action.yml": {`    - run: "*/setup.sh"`},
		".github/workflows/review.yml":     {"            You are the pull request review of Demo\n            Shop. Read CLAUDE.md first\n            then docs/. The pull request"},
	} {
		for _, line := range lines {
			if !strings.Contains(text[path], line) {
				t.Errorf("%s has no %q:\n%s", path, line, text[path])
			}
		}
	}
}

func TestAnEmptyBlockInAYAMLFileRendersNothing(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	p := project(t, "repo = \"x/demo\"\nmodules = [\"ci\"]\n[workflow]\nchecks = [\"test\"]\n[ci]\nstack = \"pnpm\"\n[blocks.\".github/workflows/ci.yml\"]\nheader = \"\"\n[[roles]]\nname = \"lead\"\n")
	mods, _ := k.For(p)
	files, err := k.Render(p, mods)
	if err != nil {
		t.Fatal(err)
	}
	if ci := renderedText(t, files, ".github/workflows/ci.yml"); !strings.HasPrefix(ci, "name: ci\n") {
		t.Fatal(ci)
	}
}

func renderedText(t *testing.T, files []File, path string) string {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return f.Text
		}
	}
	t.Fatalf("%s is not rendered", path)
	return ""
}

func TestTwoFilesThatRenderToOnePathAreRefusedNamingBoth(t *testing.T) {
	unchecked(t)
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	p := project(t, "modules = [\"workflow\"]\nlead = \"task-flow\"\n[[roles]]\nname = \"task-flow\"\n")
	mods, _ := k.For(p)
	_, err = k.Render(p, mods)
	if err == nil || !strings.HasSuffix(err.Error(), ": workflow/.claude/skills/task-flow/SKILL.md and workflow/.claude/skills/{{.Lead}}/SKILL.md both render .claude/skills/task-flow/SKILL.md, so a role is named like one of the kit's files, rename it") {
		t.Fatal(err)
	}
}

func TestARolesDoesStaysInItsCellOfTheTeamTable(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	p := project(t, "modules = [\"team\"]\n[[roles]]\nname = \"lead\"\ndoes = \"merges | reviews\\nand plans\"\n[[roles]]\nname = \"dev\"\n")
	mods, _ := k.For(p)
	files, err := k.Render(p, mods)
	if err != nil {
		t.Fatal(err)
	}
	if team := renderedText(t, files, "TEAM.md"); !strings.Contains(team, "| lead | `~/dev/demo/lead` | merges \\| reviews and plans |\n| dev |") {
		t.Fatal(team)
	}
}

// the team and full presets of an earlier mingo new wrote the tracker module with the tracker none
func TestTheTrackerModuleWithNoTrackerRendersNothingOfItsOwnAndNeedsNothing(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	p := project(t, "repo = \"x/demo\"\nmodules = [\"workflow\", \"skills\", \"team\", \"tracker\"]\n[tracker]\nkind = \"none\"\n[[roles]]\nname = \"lead\"\n[[roles]]\nname = \"dev\"\n")
	mods, err := k.For(p)
	if err != nil {
		t.Fatal(err)
	}
	files, err := k.Render(p, mods)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Module == "tracker" {
			t.Fatal(f.Path)
		}
	}
	tracker, _ := k.Module("tracker")
	d := Deps{Project: p, Run: (&fakeGitHub{labels: `[{"name":"ci-unavailable"}]`}).run, TempRoot: t.TempDir()}
	for _, c := range tracker.Checks {
		r := &report{}
		checks[c](d, r)
		for _, f := range r.findings {
			if !f.ok {
				t.Fatal(c, f)
			}
		}
	}
	if labels := wantedLabels(d); len(labels) != 1 || labels[0].name != CIUnavailable {
		t.Fatal(labels)
	}
}
