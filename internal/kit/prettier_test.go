package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheIgnoreRulesFollowGit(t *testing.T) {
	for _, c := range []struct {
		rules, file string
		want        bool
	}{
		{".github", ".github/workflows/ci.yml", true},
		{"/tools/heavy.mjs", "tools/heavy.mjs", true},
		{"heavy.mjs", "tools/heavy.mjs", true},
		{"tools/", "tools/heavy.mjs", true},
		{"*.md", ".claude/skills/plan/SKILL.md", true},
		{".claude/", ".claude/settings.json", true},
		{".claude/", ".claude", false},
		{"tools/*.mjs", "tools/heavy.mjs", true},
		{"tools/*.mjs", "app/tools/heavy.mjs", false},
		{"**/workflows/*.yml", ".github/workflows/ci.yml", true},
		{".github/**", ".github/workflows/ci.yml", true},
		{".github/*\n!.github/workflows/", ".github/workflows/ci.yml", false},
		{".github/*\n!.github/workflows/", ".github/actions/setup/action.yml", true},
		{"*.yml\n!ci.yml", ".github/workflows/ci.yml", false},
		{".github\n!.github/workflows/ci.yml", ".github/workflows/ci.yml", true},
		{"# tools/heavy.mjs\n\n", "tools/heavy.mjs", false},
		{`\#notes.md`, "#notes.md", true},
		{"dist\nvar\r\n", "TEAM.md", false},
	} {
		if got := ignored(parseIgnore(c.rules), c.file); got != c.want {
			t.Errorf("%q ignoring %s is %v, want %v", c.rules, c.file, got, c.want)
		}
	}
}

func TestPrettierOpenListsTheFilesNeitherIgnoreFileKeepsOutOnlyWhereTheProjectHasPrettier(t *testing.T) {
	files := []File{{Path: ".github/workflows/ci.yml"}, {Path: "tools/heavy.mjs"}, {Path: "TEAM.md"}}
	repo := map[string]string{
		"package.json":    `{"devDependencies": {"prettier": "3.9.9"}}`,
		".gitignore":      "*.md\n",
		".prettierignore": ".github\n",
	}
	open, used := prettierOpen(reader(repo), files)
	if !used || strings.Join(open, " ") != "tools/heavy.mjs" {
		t.Fatal(open, used)
	}
	repo[".prettierignore"] = ".github\n!TEAM.md\n"
	if open, _ := prettierOpen(reader(repo), files); strings.Join(open, " ") != "tools/heavy.mjs" {
		t.Fatal("a ! line in .prettierignore took back what .gitignore ignores,", open)
	}
	repo["package.json"] = `{"dependencies": {"prettier": "3.9.9"}}`
	if open, used := prettierOpen(reader(repo), files); !used || len(open) != 1 {
		t.Fatal(open, used)
	}
	for _, pkg := range []string{`{"devDependencies": {"biome": "2"}}`, "not json"} {
		repo["package.json"] = pkg
		if open, used := prettierOpen(reader(repo), files); used || open != nil {
			t.Fatal(pkg, open, used)
		}
	}
	if _, used := prettierOpen(reader(nil), files); used {
		t.Fatal("no package.json, and Prettier is used")
	}
}

func (w *world) commit(files map[string]string) {
	w.t.Helper()
	git(w.t, w.lead, "pull", "--quiet", "origin", "master")
	for path, text := range files {
		os.WriteFile(filepath.Join(w.lead, path), []byte(text), 0o644)
		git(w.t, w.lead, "add", path)
	}
	git(w.t, w.lead, "commit", "--quiet", "-m", "the project's own")
	git(w.t, w.lead, "push", "--quiet", "origin", "master")
}

func TestDoctorSaysWhichOfMingosFilesThePrettierOfTheProjectWouldRewrite(t *testing.T) {
	w := newWorld(t, false)
	w.apply()
	w.merge()
	w.commit(map[string]string{"package.json": "{\"devDependencies\": {\"prettier\": \"3.9.9\"}}\n", ".prettierignore": "TEAM.md\n"})
	missing, out, err := w.doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "    in place: the attribution setting in .claude/settings.json\n    missing: .claude/skills/chief/SKILL.md in .prettierignore, since Prettier would rewrite it (add it by hand)\n") || strings.Contains(out, "missing: TEAM.md") || missing != 1 {
		t.Fatal(missing, out)
	}
	w.commit(map[string]string{".prettierignore": "TEAM.md\n.claude\n"})
	missing, out, err = w.doctor()
	if err != nil || !strings.Contains(out, "    in place: mingo's files out of Prettier in .prettierignore\n") || missing != 0 {
		t.Fatal(missing, err, out)
	}
}

func TestDoctorLeavesPrettierOutWhereTheProjectHasNone(t *testing.T) {
	w := newWorld(t, false)
	w.apply()
	w.merge()
	w.commit(map[string]string{"package.json": "{\"devDependencies\": {\"vitest\": \"4\"}}\n"})
	missing, out, err := w.doctor()
	if err != nil || strings.Contains(out, "Prettier") || missing != 0 {
		t.Fatal(missing, err, out)
	}
}

func TestDoctorWithLocalClaudeFilesChecksThemAgainstPrettierToo(t *testing.T) {
	w := localWorld(t)
	w.apply()
	w.merge()
	w.commit(map[string]string{"package.json": "{\"devDependencies\": {\"prettier\": \"3.9.9\"}}\n", ".prettierignore": "tools\n/.claude/\n"})
	missing, out, err := w.doctor()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"TEAM.md", "CLAUDE.md"} {
		if !strings.Contains(out, "    missing: "+path+" in .prettierignore, since Prettier would rewrite it (add it by hand)\n") {
			t.Fatal(path, out)
		}
	}
	if strings.Contains(out, "missing: tools/run.sh in") || strings.Contains(out, "missing: .claude/") || missing != 2 {
		t.Fatal(missing, out)
	}
}
