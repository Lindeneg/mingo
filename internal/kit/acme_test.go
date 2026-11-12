package kit

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func showDiff(t *testing.T, want, got string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "expected"), []byte(want), 0o644)
	os.WriteFile(filepath.Join(dir, "rendered"), []byte(got), 0o644)
	out, _ := exec.Command("git", "diff", "--no-index", "--no-color", filepath.Join(dir, "expected"), filepath.Join(dir, "rendered")).Output()
	return string(out)
}

// KIT_UPDATE=1 writes the expected files again, after a template changed on purpose
func TestAProjectsConfigRendersItsFilesByteForByte(t *testing.T) {
	text, err := os.ReadFile(filepath.Join("testdata", "acme", "acme.toml"))
	if err != nil {
		t.Fatal(err)
	}
	p := projectNamed(t, "acme", string(text))
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	mods, err := k.For(p)
	if err != nil {
		t.Fatal(err)
	}
	files, err := k.Render(p, mods)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join("testdata", "acme", "repo")
	if os.Getenv("KIT_UPDATE") == "1" {
		os.RemoveAll(repo)
		for _, f := range files {
			path := filepath.Join(repo, filepath.FromSlash(f.Path))
			os.MkdirAll(filepath.Dir(path), 0o755)
			if err := os.WriteFile(path, []byte(f.Text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	rendered := map[string]bool{}
	for _, f := range files {
		rendered[f.Path] = true
		want, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("%s is rendered and not expected", f.Path)
			continue
		}
		if f.Text != string(want) {
			t.Errorf("%s differs from what is expected:\n%s", f.Path, showDiff(t, string(want), f.Text))
		}
	}
	var expected []string
	filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(repo, path)
			expected = append(expected, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(expected)
	for _, path := range expected {
		if !rendered[path] {
			t.Errorf("%s is expected and not rendered", path)
		}
	}
	if len(expected) < 12 {
		t.Fatalf("only %d expected files", len(expected))
	}
}
