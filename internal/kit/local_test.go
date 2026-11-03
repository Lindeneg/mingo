package kit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func localWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, false)
	w.fsys["sessions/module.toml"] = &fstest.MapFile{Data: []byte("does = \"sessions\"\nalways = true\nsetup = [\"memory\"]\nlines = [\"No attribution.\"]\n")}
	w.fsys["skills/module.toml"] = &fstest.MapFile{Data: []byte("does = \"skills\"\nimports = [\"TEAM.md\"]\n")}
	w.fsys["skills/files/tools/run.sh"] = &fstest.MapFile{Data: []byte("echo {{.Lead}}\n")}
	w.config = "repo = \"x/demo\"\nclaude_files = \"local\"\nmodules = [\"skills\"]\n[[roles]]\nname = \"chief\"\n[[roles]]\nname = \"helper\"\n"
	git(t, w.home, "clone", "--quiet", w.origin, w.helper())
	return w
}

func (w *world) helper() string {
	return filepath.Join(w.home, "dev", "demo", "helper")
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func onBranch(w *world, path string) bool {
	return exec.Command("git", "-C", w.origin, "cat-file", "-e", Branch+":"+path).Run() == nil
}

func TestApplyWithLocalClaudeFilesPutsThemInEveryCloneOutOfGitAndTheRestInThePullRequest(t *testing.T) {
	w := localWorld(t)
	out := w.apply()
	for _, line := range []string{
		"adds tools/run.sh\n", "opened https://github.com/x/demo/pull/7\n",
		"chief: adds /CLAUDE.md /TEAM.md /.claude/ to .git/info/exclude\n", "chief: adds TEAM.md\n", "chief: adds CLAUDE.md\n",
		"chief: adds .claude/settings.json\n", "helper: adds .claude/skills/chief/SKILL.md\n",
	} {
		if !strings.Contains(out, line) {
			t.Fatal(line, out)
		}
	}
	if w.show(Branch, "tools/run.sh") != "echo chief" {
		t.Fatal(w.show(Branch, "tools/run.sh"))
	}
	for _, path := range []string{"TEAM.md", "CLAUDE.md", ".claude/settings.json", ".claude/skills/chief/SKILL.md"} {
		if onBranch(w, path) {
			t.Fatal(path, "is in the pull request")
		}
	}
	if lock := w.show(Branch, LockPath); strings.Contains(lock, "TEAM.md") || !strings.Contains(lock, `path = "tools/run.sh"`) {
		t.Fatal(lock)
	}
	if body := w.gh[len(w.gh)-1]; strings.Contains(strings.Join(body, " "), "TEAM.md") {
		t.Fatal(body)
	}
	for _, dir := range []string{w.lead, w.helper()} {
		if read(t, filepath.Join(dir, "CLAUDE.md")) != "No attribution.\n@TEAM.md\n" || read(t, filepath.Join(dir, "TEAM.md")) != "- chief\n- helper\n" {
			t.Fatal(dir)
		}
		if status := git(t, dir, "status", "--porcelain"); status != "" {
			t.Fatal(dir, status)
		}
		if lock := read(t, filepath.Join(dir, ".git", "mingo", "kit.lock")); !strings.Contains(lock, `path = "TEAM.md"`) || strings.Contains(lock, "tools/run.sh") {
			t.Fatal(lock)
		}
	}
	w.merge()
	if out := w.apply(); out != "nothing to apply\n" {
		t.Fatal(out)
	}
}

func TestApplyWithLocalClaudeFilesReplacesOrRemovesAClonesFileOnlyWhileItIsAsRendered(t *testing.T) {
	w := localWorld(t)
	w.apply()
	w.merge()
	os.WriteFile(filepath.Join(w.helper(), "TEAM.md"), []byte("- chief\n- helper\n- my own line\n"), 0o644)
	w.fsys["skills/files/TEAM.md"] = &fstest.MapFile{Data: []byte("{{range .Roles}}* {{.Name}}\n{{end}}")}
	delete(w.fsys, "skills/files/.claude/skills/{{.Lead}}/SKILL.md")
	out := w.apply()
	for _, line := range []string{
		"chief: updates TEAM.md\n", "helper: leaves TEAM.md alone, the project changed it, and mingo now renders:\n", "+* chief",
		"chief: removes .claude/skills/chief/SKILL.md, which no module renders now\n",
	} {
		if !strings.Contains(out, line) {
			t.Fatal(line, out)
		}
	}
	if read(t, filepath.Join(w.lead, "TEAM.md")) != "* chief\n* helper\n" || read(t, filepath.Join(w.helper(), "TEAM.md")) != "- chief\n- helper\n- my own line\n" {
		t.Fatal(read(t, filepath.Join(w.lead, "TEAM.md")))
	}
	if _, err := os.Stat(filepath.Join(w.lead, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatal("the emptied skills folder is still there")
	}
	if _, err := os.Stat(filepath.Join(w.lead, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
}

func TestApplyWithLocalClaudeFilesRefusesARepositoryThatHasOneAndChangesNothing(t *testing.T) {
	w := localWorld(t)
	os.MkdirAll(filepath.Join(w.lead, ".claude"), 0o755)
	os.WriteFile(filepath.Join(w.lead, ".claude", "settings.json"), []byte("{}\n"), 0o644)
	git(t, w.lead, "add", ".claude/settings.json")
	git(t, w.lead, "commit", "--quiet", "-m", "settings")
	git(t, w.lead, "push", "--quiet", "origin", "master")
	err := w.try()
	if err == nil || !strings.Contains(err.Error(), "Claude's files stay out of the repository, and x/demo has .claude/settings.json, take it out first") {
		t.Fatal(err)
	}
	if exec.Command("git", "-C", w.origin, "rev-parse", "--verify", "--quiet", Branch).Run() == nil {
		t.Fatal("the kit's branch was pushed")
	}
	if _, err := os.Stat(filepath.Join(w.helper(), "TEAM.md")); err == nil {
		t.Fatal("TEAM.md was written")
	}
}

func TestApplyWithLocalClaudeFilesNeedsEveryRoleToHaveItsOwnClone(t *testing.T) {
	w := localWorld(t)
	os.RemoveAll(w.helper())
	want := "helper has none at " + w.helper() + ", run mingo setup demo first"
	if err := w.try(); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatal(err)
	}
	git(t, filepath.Dir(w.helper()), "init", "--quiet")
	os.MkdirAll(w.helper(), 0o755)
	if err := w.try(); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(w.helper()), ".git", "mingo")); err == nil {
		t.Fatal("the folder above the clones got a lock")
	}
}

func TestDoctorWithLocalClaudeFilesReadsEveryClone(t *testing.T) {
	w := localWorld(t)
	w.apply()
	w.merge()
	os.WriteFile(filepath.Join(w.helper(), ".git", "info", "exclude"), []byte("# nothing\n"), 0o644)
	os.Remove(filepath.Join(w.helper(), "TEAM.md"))
	w.out.Reset()
	missing, err := Doctor(w.deps)
	if err != nil {
		t.Fatal(err)
	}
	out := w.out.String()
	for _, line := range []string{
		"    in place: tools/run.sh\n",
		"  Claude's files in chief's clone\n    in place: Claude's files out of git in .git/info/exclude\n",
		"    in place: TEAM.md\n", "    in place: mingo's lines in CLAUDE.md\n",
		"  Claude's files in helper's clone\n    missing: /CLAUDE.md /TEAM.md /.claude/ in .git/info/exclude (mingo apply demo)\n",
		"    missing: TEAM.md (mingo apply demo)\n",
	} {
		if !strings.Contains(out, line) {
			t.Fatal(line, out)
		}
	}
	if missing != 2 {
		t.Fatal(missing, out)
	}
}
