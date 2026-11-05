package kit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/team"
)

func TestPlanScriptsMovesEachHeavyScriptBehindTheShimOnce(t *testing.T) {
	p := project(t, "modules = [\"heavy\"]\n[heavy]\nscripts = [\"check\", \"test\"]\n[[roles]]\nname = \"lead\"\n")
	mods := []Module{{Name: "heavy"}}
	pkg := "{\n  \"name\": \"demo\",\n  \"scripts\": {\n    \"check\": \"tsc && biome check\",\n    \"test\": \"vitest\",\n    \"other\": \"x\"\n  }\n}\n"
	steps, err := PlanScripts(mods, p, reader(map[string]string{"package.json": pkg}))
	if err != nil || len(steps) != 1 || steps[0].Action != Update || !steps[0].Owned {
		t.Fatal(steps, err)
	}
	want := "{\n  \"name\": \"demo\",\n  \"scripts\": {\n    \"check\": \"node tools/heavy.mjs check -- pnpm run check:run\",\n    \"check:run\": \"tsc && biome check\",\n    \"test\": \"node tools/heavy.mjs test -- pnpm run test:run\",\n    \"test:run\": \"vitest\",\n    \"other\": \"x\"\n  }\n}\n"
	if steps[0].File.Text != want {
		t.Fatal(steps[0].File.Text)
	}
	again, _ := PlanScripts(mods, p, reader(map[string]string{"package.json": want}))
	if again[0].Action != Same {
		t.Fatal(again[0].Action)
	}
	if none, err := PlanScripts(mods, p, reader(nil)); err != nil || len(none) != 1 || none[0].Action != Absent || none[0].What != "the heavy scripts check, test" {
		t.Fatal("no package.json, and", none, err)
	}
	typo := project(t, "modules = [\"heavy\"]\n[heavy]\nscripts = [\"chek\"]\n[[roles]]\nname = \"lead\"\n")
	if _, err := PlanScripts(mods, typo, reader(map[string]string{"package.json": pkg})); err == nil || !strings.Contains(err.Error(), "package.json has no script chek, which") {
		t.Fatal(err)
	}
}

func TestPlanScriptsKeepsTheIndentationOfPackageJson(t *testing.T) {
	p := project(t, "modules = [\"heavy\"]\n[ci]\nstack = \"npm\"\n[heavy]\nscripts = [\"test:unit\"]\n[[roles]]\nname = \"lead\"\n")
	pkg := "{\n    \"name\": \"demo\",\n    \"scripts\": {\n        \"test:unit\": \"vitest run\"\n    }\n}\n"
	steps, err := PlanScripts([]Module{{Name: "heavy"}}, p, reader(map[string]string{"package.json": pkg}))
	if err != nil || len(steps) != 1 || steps[0].Action != Update {
		t.Fatal(steps, err)
	}
	want := "{\n    \"name\": \"demo\",\n    \"scripts\": {\n        \"test:unit\": \"node tools/heavy.mjs test:unit -- npm run test:unit:run\",\n        \"test:unit:run\": \"vitest run\"\n    }\n}\n"
	if steps[0].File.Text != want {
		t.Fatal(steps[0].File.Text)
	}
}

func TestPlanScriptsRefusesToOverwriteARunScriptOfTheProjectsOwn(t *testing.T) {
	p := project(t, "modules = [\"heavy\"]\n[heavy]\nscripts = [\"test\"]\n[[roles]]\nname = \"lead\"\n")
	mods := []Module{{Name: "heavy"}}
	pkg := "{\n  \"scripts\": {\n    \"test\": \"vitest\",\n    \"test:run\": \"vitest run\"\n  }\n}\n"
	_, err := PlanScripts(mods, p, reader(map[string]string{"package.json": pkg}))
	if err == nil || err.Error() != `package.json has a script test:run of its own, "vitest run", so mingo cannot move test, "vitest", there, rename test:run, then run mingo apply demo again` {
		t.Fatal(err)
	}
	same := "{\n  \"scripts\": {\n    \"test\": \"vitest\",\n    \"test:run\": \"vitest\"\n  }\n}\n"
	steps, err := PlanScripts(mods, p, reader(map[string]string{"package.json": same}))
	if err != nil || !strings.Contains(steps[0].File.Text, "\"test\": \"node tools/heavy.mjs test -- pnpm run test:run\",\n    \"test:run\": \"vitest\"\n") {
		t.Fatal(steps, err)
	}
}

func TestPlanScriptsMovesTheShimToTheStacksPackageManagerAndLeavesTheRunScriptAlone(t *testing.T) {
	p := project(t, "modules = [\"heavy\"]\n[ci]\nstack = \"npm\"\n[heavy]\nscripts = [\"test\"]\n[[roles]]\nname = \"lead\"\n")
	mods := []Module{{Name: "heavy"}}
	pkg := "{\n  \"scripts\": {\n    \"test\": \"node tools/heavy.mjs test -- pnpm run test:run\",\n    \"test:run\": \"vitest\"\n  }\n}\n"
	steps, err := PlanScripts(mods, p, reader(map[string]string{"package.json": pkg}))
	want := "{\n  \"scripts\": {\n    \"test\": \"node tools/heavy.mjs test -- npm run test:run\",\n    \"test:run\": \"vitest\"\n  }\n}\n"
	if err != nil || steps[0].Action != Update || steps[0].File.Text != want {
		t.Fatal(steps, err)
	}
	again, err := PlanScripts(mods, p, reader(map[string]string{"package.json": want}))
	if err != nil || again[0].Action != Same {
		t.Fatal(again, err)
	}
}

func TestPortsSetsTheOffsetInAnEnvThatIsThereAndKeepsEveryOtherLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := project(t, "[ports]\nvariable = \"APP_PORT_OFFSET\"\n[ports.offsets]\nlead = 10\n[[roles]]\nname = \"lead\"\n")
	dir := p.CloneDir("lead")
	os.MkdirAll(dir, 0o755)
	env := filepath.Join(dir, ".env")
	for have, want := range map[string]string{
		"A=1\nAPP_PORT_OFFSET=3\nB=secret\n": "A=1\nAPP_PORT_OFFSET=10\nB=secret\n",
		"A=1\nAPP_PORT_OFFSET=\n":            "A=1\nAPP_PORT_OFFSET=10\n",
		"A=1":                                "A=1\nAPP_PORT_OFFSET=10\n",
	} {
		os.WriteFile(env, []byte(have), 0o640)
		os.Chmod(env, 0o640)
		out := &bytes.Buffer{}
		if err := Ports(p, "lead", out); err != nil {
			t.Fatal(err)
		}
		if got := read(t, env); got != want || out.String() != "ports: "+dir+"/.env, APP_PORT_OFFSET=10\n" {
			t.Fatal(got, out.String())
		}
		if info, _ := os.Stat(env); info.Mode().Perm() != 0o640 {
			t.Fatal("the .env lost its mode", info.Mode())
		}
		r := &report{}
		checks["ports"](Deps{Project: p}, r)
		if r.missing != 0 {
			t.Fatal(r.findings)
		}
	}
	out := &bytes.Buffer{}
	if err := Ports(p, "lead", out); err != nil || out.Len() != 0 {
		t.Fatal(err, out.String())
	}
}

func TestSeedRolePutsTheHandoffPointerFirstInAMemoryFileWithoutItAndKeepsTheRest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := project(t, "[[roles]]\nname = \"lead\"\n")
	file := filepath.Join(p.MemoryDir("lead"), "MEMORY.md")
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte("- [Notes](notes.md): our notes\n"), 0o644)
	if _, made, err := SeedRole(p, "lead"); err != nil || !made {
		t.Fatal(made, err)
	}
	want := handoffLine("lead") + "- [Notes](notes.md): our notes\n"
	if read(t, file) != want {
		t.Fatal(read(t, file))
	}
	if _, made, err := SeedRole(p, "lead"); err != nil || made || read(t, file) != want {
		t.Fatal(made, err, read(t, file))
	}
}

func TestPortsWritesTheEnvFromTheExampleOnceAndSaysWhenItCannot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := project(t, "[ports]\nvariable = \"APP_PORT_OFFSET\"\n[ports.offsets]\nlead = 10\n[[roles]]\nname = \"lead\"\n[[roles]]\nname = \"other\"\n")
	dir := p.CloneDir("lead")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("A=1\nAPP_PORT_OFFSET=\nB=2\n"), 0o644)
	out := &bytes.Buffer{}
	for _, role := range []string{"lead", "other", "lead"} {
		if err := Ports(p, role, out); err != nil {
			t.Fatal(err)
		}
	}
	env, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if string(env) != "A=1\nAPP_PORT_OFFSET=10\nB=2\n" || out.String() != "ports: "+dir+"/.env, APP_PORT_OFFSET=10\n" {
		t.Fatal(string(env), out.String())
	}
	if info, err := os.Stat(filepath.Join(dir, ".env")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("the .env is not 0600", err)
	}
	os.Remove(filepath.Join(dir, ".env"))
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("A=1\n"), 0o644)
	if err := Ports(p, "lead", out); err == nil || !strings.Contains(err.Error(), "no empty APP_PORT_OFFSET line") {
		t.Fatal(err)
	}
	r := &report{}
	checks["ports"](Deps{Project: p}, r)
	if len(r.findings) != 1 || r.findings[0].ok {
		t.Fatal(r.findings)
	}
}

func TestTheRulesetIsMadeOnlyWhenTheRepositoryHasNone(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, have := range []string{"[]", `[{"name":"default"}]`} {
		var calls []string
		var posted string
		run := func(o team.Options, name string, args ...string) (team.Result, error) {
			calls = append(calls, strings.Join(args, " "))
			if args[1] == "-X" {
				posted = o.Stdin
				return team.Result{}, nil
			}
			return team.Result{Stdout: have}, nil
		}
		out := &bytes.Buffer{}
		d := Deps{Kit: k, Project: project(t, "repo = \"x/demo\"\n[[roles]]\nname = \"lead\"\n"), Run: run, Out: out, TempRoot: t.TempDir()}
		if err := makeRuleset(d); err != nil {
			t.Fatal(err)
		}
		if have == "[]" && (len(calls) != 2 || calls[1] != "api -X POST repos/x/demo/rulesets --input -" || !strings.Contains(posted, `"required_review_thread_resolution": true`) || out.String() != "ruleset: default on x/demo\n") {
			t.Fatal(calls, out.String())
		}
		if have != "[]" && len(calls) != 1 {
			t.Fatal(calls)
		}
	}
}

func TestTheRulesetTargetsTheProjectsBaseBranch(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	d := Deps{Kit: k, Project: project(t, "repo = \"x/demo\"\n[workflow]\nbase = \"main\"\n[[roles]]\nname = \"lead\"\n")}
	name, data, err := d.ruleset()
	if err != nil || name != "default" || !strings.Contains(string(data), "\"include\": [\n        \"refs/heads/main\"\n      ]") || strings.Contains(string(data), "{base}") {
		t.Fatal(name, string(data), err)
	}
}

func TestTheRulesetIsLeftOutWithANoteWhereTheRepositorysPlanHasNone(t *testing.T) {
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	refused := team.Result{Code: 1, Stderr: "gh: Upgrade to GitHub Pro or make this repository public to enable this feature. (HTTP 403)\n"}
	for _, list := range []bool{true, false} {
		run := func(o team.Options, name string, args ...string) (team.Result, error) {
			if args[1] == "-X" || list {
				return refused, nil
			}
			return team.Result{Stdout: "[]"}, nil
		}
		out := &bytes.Buffer{}
		d := Deps{Kit: k, Project: project(t, "repo = \"x/demo\"\n[[roles]]\nname = \"lead\"\n"), Run: run, Out: out, TempRoot: t.TempDir()}
		if err := makeRuleset(d); err != nil || out.String() != "ruleset: not made on x/demo, GitHub has rulesets on a private repository only with GitHub Pro, Team or Enterprise Cloud\n" {
			t.Fatal(list, err, out.String())
		}
		if !list {
			continue
		}
		r := &report{}
		checks["ruleset"](d, r)
		if r.missing != 0 || r.unknown != 0 || len(r.findings) != 1 || r.findings[0].text != "not available: the ruleset default on x/demo, GitHub has rulesets on a private repository only with GitHub Pro, Team or Enterprise Cloud" {
			t.Fatal(r.findings)
		}
	}
}

func TestPlanSettingsSetsOnlyTheAttributionKey(t *testing.T) {
	off := "{\n  \"attribution\": {\n    \"commit\": \"\",\n    \"pr\": \"\",\n    \"sessionUrl\": false\n  }\n}\n"
	for name, c := range map[string]struct {
		have   string
		action Action
		want   string
	}{
		"none":    {"", Add, off},
		"others":  {"{\n  \"hooks\": {}\n}\n", Update, "{\n  \"hooks\": {},\n  \"attribution\": {\n    \"commit\": \"\",\n    \"pr\": \"\",\n    \"sessionUrl\": false\n  }\n}\n"},
		"text":    {"{\"attribution\": {\"commit\": \"Co-Authored-By: x\"}}", Update, off},
		"off":     {off, Same, off},
		"false":   {"{\"attribution\": false}", Update, off},
		"no link": {"{\"attribution\": {\"commit\": \"\", \"pr\": \"\"}}", Update, off},
		"link on": {"{\"attribution\": {\"commit\": \"\", \"pr\": \"\", \"sessionUrl\": true}}", Update, off},
	} {
		files := map[string]string{}
		if c.have != "" {
			files[Settings] = c.have
		}
		steps, err := PlanSettings(reader(files))
		if err != nil || len(steps) != 1 || steps[0].Action != c.action || steps[0].File.Text != c.want || !steps[0].Owned {
			t.Fatal(name, steps, err)
		}
	}
	if _, err := PlanSettings(reader(map[string]string{Settings: "[]"})); err == nil {
		t.Fatal("a settings file that is no object")
	}
}

func TestNextLockDropsTheFilesNoModuleRenders(t *testing.T) {
	lock := &Lock{Files: []Locked{{Path: "a", Sum: Sum([]byte("a"))}, {Path: "b", Sum: Sum([]byte("b"))}, {Path: "c", Sum: Sum([]byte("c"))}}}
	steps := Plan([]File{{Path: "c", Text: "c"}}, reader(map[string]string{"a": "a", "b": "b, ours", "c": "c"}), lock)
	got := map[string]Action{}
	for _, s := range steps {
		got[s.File.Path] = s.Action
	}
	if got["a"] != Remove || got["b"] != Leave || got["c"] != Same {
		t.Fatal(got)
	}
	next, changed := NextLock(steps, lock, "v")
	if !changed || len(next.Files) != 1 || next.Files[0].Path != "c" {
		t.Fatal(next.Files)
	}
}
