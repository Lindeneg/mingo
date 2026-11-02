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

func TestForRefusesAModuleMingoDoesNotHaveOrOneAlwaysOn(t *testing.T) {
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
