//go:build unix

package loop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/config"
	"github.com/Lindeneg/mingo/internal/provider"
)

// a provider defined here alone, so a loop on a project that names it shows what the loop takes from a provider
type testCLI struct{}

func init() {
	provider.Register(testCLI{})
}

func (testCLI) Name() string          { return "testcli" }
func (testCLI) Title() string         { return "Test CLI" }
func (testCLI) Site() string          { return "https://example.com/test-cli" }
func (testCLI) Command() string       { return "test-cli" }
func (testCLI) CommandEnv() string    { return "MINGO_TEST_CLI" }
func (testCLI) DefaultArgs() []string { return []string{"--yes"} }
func (testCLI) ExitKeys() []string    { return []string{"C-d"} }
func (testCLI) ExitShown() string     { return "Ctrl+D" }
func (testCLI) MemoryDir(home, clone string) string {
	return filepath.Join(home, ".test-cli", filepath.Base(clone))
}
func (testCLI) MemoryIndex() string { return "" }
func (testCLI) Kit() provider.Kit   { return provider.Kit{Instructions: "AGENTS.md"} }

func (testCLI) Args(s provider.Session) []string {
	return append([]string{"--model=" + s.Model, "--prompt", s.Opener}, s.Args...)
}

func TestALoopRunsTheCLIOfTheProjectsProviderWithThatProvidersArgs(t *testing.T) {
	b := newBoundary(t)
	os.WriteFile(b.file, []byte("clone = \""+filepath.Join(filepath.Dir(b.l.exe), "clone")+"\"\nprovider = \"testcli\"\nopener = \"Go on, {role}.\"\n[[roles]]\nname = \"lead\"\nmodel = \"m1\"\n"), 0o644)
	p, err := config.Find("demo")
	if err != nil {
		t.Fatal(err)
	}
	b.l.p = p
	if got := strings.Join(b.l.args(), "|"); got != "--model=m1|--prompt|Go on, lead.|--yes" {
		t.Fatal(got)
	}
	argv := filepath.Join(t.TempDir(), "argv")
	cli := filepath.Join(t.TempDir(), "test-cli")
	os.WriteFile(cli, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >"+argv+"\nexit 3\n"), 0o755)
	if code, _, _ := b.l.session(cli); code != 3 {
		t.Fatal(code)
	}
	if got, _ := os.ReadFile(argv); string(got) != "--model=m1\n--prompt\nGo on, lead.\n--yes\n" {
		t.Fatalf("%q", got)
	}
	t.Setenv("PATH", t.TempDir())
	if err := commandProblem(p.Provider(), "test-cli", false); err == nil || err.Error() != "test-cli is not on PATH, install Test CLI" {
		t.Fatal(err)
	}
	t.Setenv("MINGO_TEST_CLI", cli)
	if command, set := provider.CommandOf(p.Provider()); command != cli || !set {
		t.Fatal(command, set)
	}
}
