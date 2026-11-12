package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
		env:     WriteEnv{ProjectDir: project, TempDir: filepath.Join(root, "tmp"), Home: home, StateDir: filepath.Join(root, "tmp", "mingo")},
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

func TestWriteDeniesMingosStateFolderInsideTheTempFolder(t *testing.T) {
	c := newWriteCase(t)
	for _, path := range []string{c.env.StateDir + "/other/lead.stop", c.env.StateDir + "/heavy/slot-1", c.env.StateDir} {
		out := c.write(t, path, "")
		if !denied(t, out) || !strings.Contains(out, "is in mingo's state folder, which only mingo writes") {
			t.Fatal(path, out)
		}
	}
	if out := c.write(t, c.env.TempDir+"/mingo-notes.md", ""); out != "" {
		t.Fatal(out)
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

func TestWriteJudgesALinkWhoseTargetIsMissingByThatTarget(t *testing.T) {
	c := newWriteCase(t)
	outside := filepath.Join(filepath.Dir(c.env.Home), "outside")
	for _, dir := range []string{c.project, outside, c.env.TempDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		"out":     filepath.Join(outside, "missing.txt"),
		"outdir":  filepath.Join(outside, "missing"),
		"rel":     "../../../../outside/missing.txt",
		"chain":   "out",
		"build":   "dist/next.txt",
		"scratch": filepath.Join(c.env.TempDir, "missing.txt"),
	} {
		if err := os.Symlink(target, filepath.Join(c.project, link)); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"out", "outdir/new.txt", "rel", "chain", filepath.Join(c.project, "out")} {
		if !denied(t, c.write(t, path, "")) {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"build", "scratch"} {
		if out := c.write(t, path, ""); out != "" {
			t.Fatal(path, out)
		}
	}
}

func TestWriteAllowsTheSystemTempFolderOnlyWhenItIsGiven(t *testing.T) {
	c := newWriteCase(t)
	root := filepath.Dir(c.env.Home)
	private := filepath.Join(root, "private", "tmp")
	if err := os.MkdirAll(private, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "systmp")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(link, "claude-501", "scratchpad", "notes.md"),
		filepath.Join(private, "claude-501", "scratchpad", "notes.md"),
	}
	for _, path := range paths {
		if !denied(t, c.write(t, path, "")) {
			t.Fatal("allowed without a system temp folder", path)
		}
	}
	c.env.SystemTempDir = link
	for _, path := range paths {
		if out := c.write(t, path, ""); out != "" {
			t.Fatal(path, out)
		}
	}
	c.env.StateDir = filepath.Join(link, "mingo")
	out := c.write(t, filepath.Join(private, "mingo", "heavy", "slot-1"), "")
	if !denied(t, out) || !strings.Contains(out, "is in mingo's state folder") {
		t.Fatal(out)
	}
}

func TestWriteEnvAddsSlashTmpBesideTMPDIROnlyOnMacOS(t *testing.T) {
	t.Setenv("TMPDIR", "/var/folders/xy/T")
	env := WriteEnvFromOS()
	want := ""
	if runtime.GOOS == "darwin" {
		want = "/tmp"
	}
	if env.TempDir != "/var/folders/xy/T" || env.SystemTempDir != want {
		t.Fatalf("%+v", env)
	}
	t.Setenv("TMPDIR", "")
	if env := WriteEnvFromOS(); env.TempDir != "/tmp" {
		t.Fatalf("%+v", env)
	}
}

func TestWriteAllowsSlashTmpAndPrivateTmpOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	c := newWriteCase(t)
	c.env.SystemTempDir = WriteEnvFromOS().SystemTempDir
	for _, path := range []string{"/tmp/claude-501/x/scratchpad/notes.md", "/private/tmp/claude-501/x/scratchpad/notes.md"} {
		if out := c.write(t, path, ""); out != "" {
			t.Fatal(path, out)
		}
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
