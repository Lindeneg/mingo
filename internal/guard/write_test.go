package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/config"
)

type writeCase struct {
	env     WriteEnv
	project string
}

func newWriteCase(t *testing.T) writeCase {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(home, "dev", "demo", "lead")
	return writeCase{
		env:     WriteEnv{ProjectDir: project, TempDir: filepath.Join(root, "tmp"), Home: home},
		project: project,
	}
}

func (c writeCase) write(t *testing.T, path, cwd string) string {
	t.Helper()
	if cwd == "" {
		cwd = c.project
	}
	in, _ := json.Marshal(map[string]any{"tool_name": "Write", "cwd": cwd, "tool_input": map[string]any{"file_path": path}})
	return Write(in, c.env)
}

func denied(t *testing.T, out string) bool {
	t.Helper()
	var d decision
	if json.Unmarshal([]byte(out), &d) != nil {
		return false
	}
	return d.HookSpecificOutput.PermissionDecision == "deny" && d.HookSpecificOutput.HookEventName == "PreToolUse"
}

func TestWriteAllowsInsideTheProject(t *testing.T) {
	c := newWriteCase(t)
	if out := c.write(t, c.project+"/src/a.ts", ""); out != "" {
		t.Fatal(out)
	}
}

func TestWriteAllowsTempMemoryAndPlans(t *testing.T) {
	c := newWriteCase(t)
	for _, path := range []string{
		c.env.TempDir + "/claude-1000/scratch/notes.md",
		c.env.Home + "/.claude/projects/" + config.Slug(c.project) + "/memory/handoff.md",
		c.env.Home + "/.claude/plans/plan.md",
	} {
		if out := c.write(t, path, ""); out != "" {
			t.Fatal(path, out)
		}
	}
}

func TestWriteDeniesOtherClonesMemoryAndHome(t *testing.T) {
	c := newWriteCase(t)
	out := c.write(t, c.env.Home+"/dev/other/lead/a.ts", "")
	if !denied(t, out) || !strings.Contains(out, "Write only inside your own project") {
		t.Fatal(out)
	}
	for _, path := range []string{c.env.Home + "/.claude/projects/-other/memory/x.md", c.env.Home + "/.bashrc"} {
		if !denied(t, c.write(t, path, "")) {
			t.Fatal(path)
		}
	}
}

func TestWriteDeniesASiblingStartingLikeTheProject(t *testing.T) {
	c := newWriteCase(t)
	if !denied(t, c.write(t, c.project+"-copy/a.ts", "")) {
		t.Fatal("sibling allowed")
	}
}

func TestWriteDeniesAPathClimbingOut(t *testing.T) {
	c := newWriteCase(t)
	if !denied(t, c.write(t, c.project+"/../../other/a.ts", "")) {
		t.Fatal("climb allowed")
	}
}

func TestWriteReadsRelativePathAgainstCwd(t *testing.T) {
	c := newWriteCase(t)
	if out := c.write(t, "src/a.ts", c.project+"/packages"); out != "" {
		t.Fatal(out)
	}
	if !denied(t, c.write(t, "../../other/a.ts", c.project)) {
		t.Fatal("relative climb allowed")
	}
}

func TestWriteChecksANotebookPath(t *testing.T) {
	c := newWriteCase(t)
	in, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"notebook_path": c.env.Home + "/elsewhere.ipynb"}})
	if !denied(t, Write(in, c.env)) {
		t.Fatal("notebook allowed")
	}
}

func TestWriteDeniesEverythingWithoutAProject(t *testing.T) {
	c := newWriteCase(t)
	c.env.ProjectDir = ""
	out := c.write(t, c.project+"/a.ts", "")
	if !denied(t, out) || !strings.Contains(out, "CLAUDE_PROJECT_DIR is not set") {
		t.Fatal(out)
	}
}

func TestWriteAllowsUnreadableInputAndNoPath(t *testing.T) {
	c := newWriteCase(t)
	if out := Write([]byte("not json\n"), c.env); out != "" {
		t.Fatal(out)
	}
	if out := Write([]byte(`{"tool_input": {}}`), c.env); out != "" {
		t.Fatal(out)
	}
}

func TestWriteDeniesALinkInsideTheProjectThatLeadsOut(t *testing.T) {
	c := newWriteCase(t)
	outside := filepath.Join(filepath.Dir(c.env.Home), "outside")
	for _, dir := range []string{c.project, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(c.project, "external")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"external/config", "external/new/deep.txt", filepath.Join(c.project, "external")} {
		if !denied(t, c.write(t, path, "")) {
			t.Fatal(path)
		}
	}
	if denied(t, c.write(t, "inside/new.txt", "")) {
		t.Fatal("a new file inside the project")
	}
}

func TestWriteTakesTheProjectTheTempAndTheMemoryByTheirRealPathsToo(t *testing.T) {
	c := newWriteCase(t)
	real := filepath.Join(filepath.Dir(c.env.Home), "real")
	for _, dir := range []string{real, filepath.Join(real, "tmp"), filepath.Dir(c.project), c.env.Home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(real, "tmp"), c.env.TempDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, c.project); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(real, "tmp", "scratch.txt"),
		filepath.Join(c.env.TempDir, "scratch.txt"),
		filepath.Join(real, "src", "a.go"),
		filepath.Join(c.env.Home, ".claude", "projects", config.Slug(real), "memory", "MEMORY.md"),
		filepath.Join(c.env.Home, ".claude", "projects", config.Slug(c.project), "memory", "MEMORY.md"),
	} {
		if denied(t, c.write(t, path, "")) {
			t.Fatal(path)
		}
	}
}
