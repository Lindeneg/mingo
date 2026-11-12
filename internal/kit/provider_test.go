package kit

import (
	"strings"
	"testing"

	"github.com/Lindeneg/mingo/internal/provider"
	// the projects of these tests run Claude Code unless they name another provider
	_ "github.com/Lindeneg/mingo/internal/provider/claude"
)

// a provider defined here alone, so a project that names it shows what the kit takes from a provider
type testCLI struct{}

func init() {
	provider.Register(testCLI{})
}

func (testCLI) Name() string                        { return "testcli" }
func (testCLI) Title() string                       { return "Test CLI" }
func (testCLI) Site() string                        { return "https://example.com/test-cli" }
func (testCLI) Command() string                     { return "test-cli" }
func (testCLI) CommandEnv() string                  { return "MINGO_TEST_CLI" }
func (testCLI) DefaultArgs() []string               { return nil }
func (testCLI) Args(s provider.Session) []string    { return []string{"--prompt", s.Opener} }
func (testCLI) ExitKeys() []string                  { return []string{"C-d"} }
func (testCLI) ExitShown() string                   { return "Ctrl+D" }
func (testCLI) MemoryDir(home, clone string) string { return home + "/.test-cli/memory" + clone }
func (testCLI) MemoryIndex() string                 { return "" }
func (testCLI) Kit() provider.Kit                   { return testCLIKit }

var testCLIKit = provider.Kit{
	Instructions: "AGENTS.md",
	Import:       "Read %s whole before anything.",
	Dir:          ".agents",
	Skills:       ".agents/skills",
	Connectors:   "in the test CLI's own config",
	Words: `{{define "agent.cli"}}Test CLI{{end}}
{{define "agent.review"}}test review{{end}}
{{define "agent.reach"}}through ` + "`mingo say {{.Name}}/<role>`: `mingo agents {{.Name}}`" + ` shows a row{{end}}
{{define "agent.send"}}` + "`mingo say`" + `{{end}}
{{define "agent.to-row"}}by ` + "`mingo say {{.Name}}/{{.Lead}}`" + `{{end}}
{{define "agent.tell-lead"}}by ` + "`mingo say {{.Name}}/{{.Lead}}`" + `{{end}}
{{define "agent.list"}}` + "`mingo agents {{.Name}}`" + `. Every role has a row{{end}}
{{define "agent.exit"}}Ctrl+D{{end}}
{{define "agent.stop-tasks"}}{{end}}
{{define "agent.busy-exit"}}{{end}}
{{define "agent.held"}}the CLI can hold it with a question nobody answers{{end}}
{{define "agent.draft"}}{{end}}
{{define "agent.subagent-tool"}}{{end}}
`,
}

func TestAProjectOnAnotherProviderGetsThatProvidersFilesWordsAndFoldersAndNothingOfClaudeCode(t *testing.T) {
	p := project(t, "provider = \"testcli\"\nmodules = [\"workflow\", \"skills\", \"team\", \"tracker\"]\n[tracker]\nkind = \"github\"\n[[roles]]\nname = \"lead\"\n[[roles]]\nname = \"dev\"\n")
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
	paths := map[string]string{}
	for _, f := range files {
		paths[f.Path] = f.Text
		if strings.Contains(f.Path, ".claude") || strings.Contains(f.Text, "Claude") || strings.Contains(f.Text, "CLAUDE.md") ||
			strings.Contains(f.Text, "ListAgents") || strings.Contains(f.Text, "SendMessage") || strings.Contains(f.Text, "/exit") {
			t.Fatalf("%s names Claude Code\n%s", f.Path, f.Text)
		}
	}
	for _, path := range []string{".agents/skills/task-flow/SKILL.md", ".agents/skills/lead/SKILL.md", ".agents/skills/clear-context/SKILL.md", ".agents/skills/sub-agents/SKILL.md", "TEAM.md"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("no %s among %v", path, paths)
		}
	}
	if team := paths["TEAM.md"]; !strings.Contains(team, "2 Test CLI sessions work on this repository at once") || !strings.Contains(team, "`mingo agents demo` shows a row per role") {
		t.Fatal(team)
	}
	clear := paths[".agents/skills/clear-context/SKILL.md"]
	if !strings.Contains(clear, "types Ctrl+D, and starts the next session") || !strings.Contains(clear, "Reads `AGENTS.md` whole") {
		t.Fatal(clear)
	}
	steps := PlanImports(p.Provider().Kit(), mods, reader(map[string]string{}))
	if len(steps) != 1 || steps[0].File.Path != "AGENTS.md" || !strings.Contains(steps[0].File.Text, "Read TEAM.md whole before anything.\n") {
		t.Fatal(steps)
	}
	if steps, err := PlanSettings(p.Provider(), reader(map[string]string{})); err != nil || len(steps) != 0 {
		t.Fatal(steps, err)
	}
}

func TestTheReviewModuleIsRefusedOnAProviderWithoutAReview(t *testing.T) {
	p := project(t, "provider = \"testcli\"\nmodules = [\"review\"]\n[[roles]]\nname = \"lead\"\n")
	k, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.For(p); err == nil || !strings.HasSuffix(err.Error(), "names the module review, which Test CLI has no files for") {
		t.Fatal(err)
	}
}
